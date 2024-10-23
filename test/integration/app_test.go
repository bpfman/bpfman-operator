//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	appGoCounterKustomize       = "https://github.com/bpfman/bpfman/examples/config/default/go-app-counter/?timeout=120&ref=main"
	appGoCounterUserspaceNs     = "go-app-counter"
	appGoCounterUserspaceDsName = "go-app-counter-ds"
)

func TestApplicationGoCounter(t *testing.T) {
	t.Log("deploying target required for uprobe counter program if its not already deployed")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), targetKustomize))

	t.Log("waiting for go target userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(targetUserspaceNs).Get(ctx, targetUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
		// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
		5*time.Minute, 10*time.Second)

	t.Log("deploying application counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), appGoCounterKustomize))
	addCleanup(func(context.Context) error {
		cleanupLog("cleaning up application counter program")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), appGoCounterKustomize)
	})

	t.Log("waiting for go application counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(appGoCounterUserspaceNs).Get(ctx, appGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
		// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
		5*time.Minute, 10*time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(appGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-app-counter"})
	require.NoError(t, err)
	goAppCounterPod := pods.Items[0]

	checkFunctions := []func(t *testing.T, output *bytes.Buffer) bool{
		doAppKprobeCheck,
		doAppTcCheck,
		doAppTcxCheck,
		doAppTracepointCheck,
		doAppUprobeCheck,
		doAppXdpCheck,
	}

	for idx, f := range checkFunctions {
		req := env.Cluster().Client().CoreV1().Pods(appGoCounterUserspaceNs).GetLogs(goAppCounterPod.Name, &corev1.PodLogOptions{})
		require.Eventually(t, func() bool {
			logs, err := req.Stream(ctx)
			require.NoError(t, err)
			defer logs.Close()
			output := new(bytes.Buffer)
			_, err = io.Copy(output, logs)
			require.NoError(t, err)

			if f(t, output) {
				return true
			}
			t.Logf("check %d failed retrying, output %s", idx, output.String())
			return false
		}, 30*time.Second, time.Second)
	}
}
