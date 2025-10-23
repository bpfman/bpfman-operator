//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	tcxGoCounterKustomize       = "https://github.com/bpfman/bpfman/examples/config/default/go-tcx-counter/?timeout=120&ref=main"
	tcxGoCounterUserspaceNs     = "go-tcx-counter"
	tcxGoCounterUserspaceDsName = "go-tcx-counter-ds"
	tcxGoCounterBytecodeName    = "go-tcx-counter-example"
	tcxByteCodeLabelSelector    = "app.kubernetes.io/name=tcxprogram"
)

func TestTcxGoCounter(t *testing.T) {
	t.Log("deploying tcx counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), tcxGoCounterKustomize))
	t.Cleanup(func() {
		cleanupLog("cleaning up tcx counter program")
		clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), tcxGoCounterKustomize)
	})

	t.Log("waiting for go tcx counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(tcxGoCounterUserspaceNs).Get(ctx, tcxGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
		// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
		5*time.Minute, 10*time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(tcxGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-tcx-counter"})
	require.NoError(t, err)
	require.Len(t, pods.Items, 1)
	gotcxCounterPod := pods.Items[0]

	req := env.Cluster().Client().CoreV1().Pods(tcxGoCounterUserspaceNs).GetLogs(gotcxCounterPod.Name, &corev1.PodLogOptions{})

	require.Eventually(t, func() bool {
		logs, err := req.Stream(ctx)
		require.NoError(t, err)
		defer logs.Close()
		output := new(bytes.Buffer)
		_, err = io.Copy(output, logs)
		require.NoError(t, err)
		t.Logf("counter pod log %s", output.String())

		return doTcxCheck(t, output)
	}, 30*time.Second, time.Second)
}

func TestTcxGoCounterLinkPriority(t *testing.T) {
	priorities := []*int32{
		nil,
		ptr.To(int32(0)),
		ptr.To(int32(500)),
		ptr.To(int32(1000)),
	}

	t.Log("deploying tcx counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), tcxGoCounterKustomize))
	t.Cleanup(func() {
		cleanupLog("cleaning up tcx counter program")
		clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), tcxGoCounterKustomize)

		cleanupLog("cleaning up tcx counter bytecode")
		bpfmanClient.BpfmanV1alpha1().ClusterBpfApplications().DeleteCollection(ctx, metav1.DeleteOptions{},
			metav1.ListOptions{
				LabelSelector: tcxByteCodeLabelSelector,
			})
	})

	t.Log("creating copies of bytecode using the same link")
	cba, err := bpfmanClient.BpfmanV1alpha1().ClusterBpfApplications().Get(ctx, tcxGoCounterBytecodeName, metav1.GetOptions{})
	require.NoError(t, err)
	name := cba.Name
	cba.ObjectMeta = metav1.ObjectMeta{
		Labels: cba.Labels,
	}
	for i, priority := range priorities {
		cba.Name = fmt.Sprintf("%s-%d", name, i)
		cba.Spec.Programs[0].TCX.Links[0].Priority = priority
		_, err := bpfmanClient.BpfmanV1alpha1().ClusterBpfApplications().Create(ctx, cba, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	// Add priority 55 from the kustomize deployment as well.
	priorities = append(priorities, ptr.To(int32(55)))

	t.Log("waiting for bytecode to be attached successfully")
	require.Eventually(t, clusterBpfApplicationStateSuccess(t, tcxByteCodeLabelSelector, len(priorities)), 2*time.Minute, 10*time.Second)
	require.Eventually(t, verifyClusterBpfApplicationPriority(t, tcxByteCodeLabelSelector), 1*time.Minute, 10*time.Second)

	t.Log("waiting for go tcx counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(tcxGoCounterUserspaceNs).Get(ctx, tcxGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
		// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
		5*time.Minute, 10*time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(tcxGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-tcx-counter"})
	require.NoError(t, err)
	require.Len(t, pods.Items, 1)
	goTcxCounterPod := pods.Items[0]

	req := env.Cluster().Client().CoreV1().Pods(tcxGoCounterUserspaceNs).GetLogs(goTcxCounterPod.Name, &corev1.PodLogOptions{})

	require.Eventually(t, func() bool {
		logs, err := req.Stream(ctx)
		require.NoError(t, err)
		defer logs.Close()
		output := new(bytes.Buffer)
		_, err = io.Copy(output, logs)
		require.NoError(t, err)
		t.Logf("counter pod log %s", output.String())

		return doTcxCheck(t, output)
	}, 30*time.Second, time.Second)
}
