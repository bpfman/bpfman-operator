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
	kprobeGoCounterKustomize       = "https://github.com/bpfman/bpfman/examples/config/default/go-kprobe-counter/?timeout=120&ref=main"
	kprobeGoCounterUserspaceNs     = "go-kprobe-counter"
	kprobeGoCounterUserspaceDsName = "go-kprobe-counter-ds"
)

func TestKprobeGoCounter(t *testing.T) {
	t.Log("deploying kprobe counter program")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), kprobeGoCounterKustomize))
	addCleanup(func(context.Context) error {
		cleanupLog("cleaning up kprobe counter program")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), kprobeGoCounterKustomize)
	})

	t.Log("waiting for go kprobe counter userspace daemon to be available")
	require.Eventually(t, func() bool {
		daemon, err := env.Cluster().Client().AppsV1().DaemonSets(kprobeGoCounterUserspaceNs).Get(ctx, kprobeGoCounterUserspaceDsName, metav1.GetOptions{})
		require.NoError(t, err)
		return daemon.Status.DesiredNumberScheduled == daemon.Status.NumberAvailable
	},
		// Wait 5 minutes since cosign is slow, https://github.com/bpfman/bpfman/issues/1043
		5*time.Minute, 10*time.Second)

	pods, err := env.Cluster().Client().CoreV1().Pods(kprobeGoCounterUserspaceNs).List(ctx, metav1.ListOptions{LabelSelector: "name=go-kprobe-counter"})
	require.NoError(t, err)
	goKprobeCounterPod := pods.Items[0]

	req := env.Cluster().Client().CoreV1().Pods(kprobeGoCounterUserspaceNs).GetLogs(goKprobeCounterPod.Name, &corev1.PodLogOptions{})
	require.Eventually(t, func() bool {
		logs, err := req.Stream(ctx)
		require.NoError(t, err)
		defer logs.Close()
		output := new(bytes.Buffer)
		_, err = io.Copy(output, logs)
		require.NoError(t, err)
		t.Logf("counter pod log %s", output.String())

		return doKprobeCheck(t, output)
	}, 30*time.Second, time.Second)
}
