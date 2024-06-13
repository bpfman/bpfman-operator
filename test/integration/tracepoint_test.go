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
	tracepointGoCounterKustomize       = "https://github.com/bpfman/bpfman/examples/config/default/go-tracepoint-counter/?timeout=120&ref=main"
	tracepointGoCounterUserspaceNs     = "go-tracepoint-counter"
	tracepointGoCounterUserspaceDsName = "go-tracepoint-counter-ds"
)

func TestTracepointGoCounter(t *testing.T) {
	t.Log("deploying tracepoint counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), tracepointGoCounterKustomize))
	addCleanup(func(context.Context) error {
		cleanupLog("cleaning up tracepoint counter program")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), tracepointGoCounterKustomize)
	})

	t.Log("waiting for go tracepoint counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(tracepointGoCounterUserspaceNs).Get(ctx, tracepointGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
		// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
		5*time.Minute, 10*time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(tracepointGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-tracepoint-counter"})
	require.NoError(t, err)
	goTracepointCounterPod := pods.Items[0]

	req := env.Cluster().Client().CoreV1().Pods(tracepointGoCounterUserspaceNs).GetLogs(goTracepointCounterPod.Name, &corev1.PodLogOptions{})
	require.Eventually(t, func() bool {
		logs, err := req.Stream(ctx)
		require.NoError(t, err)
		defer logs.Close()
		output := new(bytes.Buffer)
		_, err = io.Copy(output, logs)
		require.NoError(t, err)
		t.Logf("counter pod log %s", output.String())
		return doTracepointCheck(t, output)
	}, 30*time.Second, time.Second)
}
