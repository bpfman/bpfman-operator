//go:build integration_tests

package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
)

const (
	xnsAppName          = "xns-pod-scope"
	xnsAppNs            = "xns-pod-scope-app"
	xnsForeignNs        = "xns-pod-scope-foreign"
	xnsTargetPodApp     = "xns-target-app"
	xnsTargetPodForeign = "xns-target-foreign"
	xnsTargetLabel      = "xns-pod-scope-target"
	xnsTargetImage      = "quay.io/quay/busybox:latest"
	xnsBytecode         = "quay.io/bpfman-bytecode/go-app-counter:latest"
)

// TestNamespacedAppDoesNotSelectPodsInOtherNamespaces proves that a
// namespaced BpfApplication's pod selectors are confined to the
// application's own namespace.
//
// Two namespaces each hold an identically-labelled target pod on the
// same node. A BpfApplication in one namespace carries a TC program
// whose network-namespace selector matches that label. Correctly
// scoped, the application attaches to exactly one pod: the target in
// its own namespace. If the agent resolves the selector against an
// empty namespace it attaches to both pods, reaching into a foreign
// namespace's pod on the same node -- the isolation break this test
// guards against. The number of TC links in the application's
// BpfApplicationState is the observable: one when scoped, two when
// leaking.
func TestNamespacedAppDoesNotSelectPodsInOtherNamespaces(t *testing.T) {
	nodes, err := env.Cluster().Client().CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, nodes.Items)
	// Pin both target pods to one node so they share a bpfman-agent,
	// making the link count deterministic.
	targetNode := nodes.Items[0].Name

	// The two target pods carry the same labels but distinct names.
	// Distinct names matter: TC link expansion runs the matched pods
	// through GetOneContainerPerPod, which deduplicates by pod name, so
	// identically-named pods would collapse to a single link and mask a
	// cross-namespace match.
	targets := []struct{ namespace, pod string }{
		{xnsAppNs, xnsTargetPodApp},
		{xnsForeignNs, xnsTargetPodForeign},
	}

	targetPod := func(name, namespace string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{"app": xnsTargetLabel},
			},
			Spec: corev1.PodSpec{
				NodeName:    targetNode,
				Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
				Containers: []corev1.Container{{
					Name:    "target",
					Image:   xnsTargetImage,
					Command: []string{"sleep", "3600"},
				}},
			},
		}
	}

	app := &v1alpha1.BpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      xnsAppName,
			Namespace: xnsAppNs,
		},
		Spec: v1alpha1.BpfApplicationSpec{
			BpfAppCommon: v1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: v1alpha1.ByteCodeSelector{
					Image: &v1alpha1.ByteCodeImage{Url: xnsBytecode},
				},
			},
			Programs: []v1alpha1.BpfApplicationProgram{
				{
					Name: "stats",
					Type: v1alpha1.ProgTypeTC,
					TC: &v1alpha1.TcProgramInfo{
						Links: []v1alpha1.TcAttachInfo{
							{
								InterfaceSelector: v1alpha1.InterfaceSelector{
									Interfaces: []string{"eth0"},
								},
								NetworkNamespaces: v1alpha1.NetworkNamespaceSelector{
									Pods: metav1.LabelSelector{
										MatchLabels: map[string]string{"app": xnsTargetLabel},
									},
								},
								Direction: v1alpha1.TCIngress,
								Priority:  ptr.To(int32(55)),
							},
						},
					},
				},
			},
		},
	}

	for _, ns := range []string{xnsAppNs, xnsForeignNs} {
		t.Logf("creating namespace %s", ns)
		_, err := env.Cluster().Client().CoreV1().Namespaces().Create(ctx,
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		cleanupLog("deleting BpfApplication %s/%s", xnsAppNs, xnsAppName)
		bpfmanClient.BpfmanV1alpha1().BpfApplications(xnsAppNs).Delete(ctx, xnsAppName, metav1.DeleteOptions{})
		// Wait for the agent to detach and remove its state objects
		// before deleting the namespace so it doesn't hang on the
		// state-object finalizer.
		require.Eventually(t, func() bool {
			states, err := bpfmanClient.BpfmanV1alpha1().BpfApplicationStates(xnsAppNs).List(ctx, metav1.ListOptions{})
			return err == nil && len(states.Items) == 0
		}, 2*time.Minute, 2*time.Second)
		for _, ns := range []string{xnsAppNs, xnsForeignNs} {
			cleanupLog("deleting namespace %s", ns)
			env.Cluster().Client().CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
		}
	})

	// Both target pods must be running before the application appears,
	// so the first reconcile already sees both candidates and the leak,
	// if present, is immediate rather than racing pod startup.
	for _, tgt := range targets {
		t.Logf("creating target pod %s/%s on node %s", tgt.namespace, tgt.pod, targetNode)
		_, err := env.Cluster().Client().CoreV1().Pods(tgt.namespace).Create(ctx, targetPod(tgt.pod, tgt.namespace), metav1.CreateOptions{})
		require.NoError(t, err)
	}
	for _, tgt := range targets {
		t.Logf("waiting for target pod %s/%s to be running", tgt.namespace, tgt.pod)
		require.Eventually(t, func() bool {
			p, err := env.Cluster().Client().CoreV1().Pods(tgt.namespace).Get(ctx, tgt.pod, metav1.GetOptions{})
			return err == nil && p.Status.Phase == corev1.PodRunning
		}, 2*time.Minute, 2*time.Second)
	}

	t.Logf("creating BpfApplication %s/%s with a TC network-namespace selector", xnsAppNs, xnsAppName)
	_, err = bpfmanClient.BpfmanV1alpha1().BpfApplications(xnsAppNs).Create(ctx, app, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait for the agent to expand the selector into attach links. The
	// link entries appear in the state whether or not the attach itself
	// succeeds, so this does not gate on the application's overall
	// condition -- an attach into the foreign pod might fail and drive
	// the application to Error, which must not hide the extra link.
	t.Logf("waiting for BpfApplication %s/%s to attach within its own namespace", xnsAppNs, xnsAppName)
	require.Eventually(t, func() bool {
		return countNsTcLinks(t, xnsAppNs) >= 1
	}, 2*time.Minute, 5*time.Second)

	// The decisive check: the application must never expand to more than
	// the single target pod in its own namespace. A second link means it
	// selected the identically-labelled pod in the foreign namespace.
	t.Logf("verifying BpfApplication %s/%s never selects the foreign namespace's pod", xnsAppNs, xnsAppName)
	require.Never(t, func() bool {
		return countNsTcLinks(t, xnsAppNs) > 1
	}, 30*time.Second, 5*time.Second)
}

// countNsTcLinks sums the TC attach links across every
// BpfApplicationState in the namespace. In a namespace holding a single
// application this is that application's total number of TC attach
// points on all nodes, regardless of whether each attach succeeded.
func countNsTcLinks(t *testing.T, namespace string) int {
	states, err := bpfmanClient.BpfmanV1alpha1().BpfApplicationStates(namespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	total := 0
	for i := range states.Items {
		// Ignore states whose application is being torn down.
		if !states.Items[i].DeletionTimestamp.IsZero() {
			continue
		}
		for _, prog := range states.Items[i].Status.Programs {
			if prog.TC != nil {
				total += len(prog.TC.Links)
			}
		}
	}
	return total
}
