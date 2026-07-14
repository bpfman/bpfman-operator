/*
Copyright 2025 The bpfman Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bpfmanagent

import (
	"context"
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientGoFake "k8s.io/client-go/kubernetes/fake"
)

// nsApp builds a namespaced BpfApplication in the given namespace.
func nsApp(namespace string) *bpfmaniov1alpha1.BpfApplication {
	return &bpfmaniov1alpha1.BpfApplication{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: namespace},
	}
}

// TestNsProgramReconcilerNamespaceFromApp proves the namespace a namespaced
// program scopes its selectors to is derived from the owning application, not
// a separately-set field that could be left empty.
func TestNsProgramReconcilerNamespaceFromApp(t *testing.T) {
	common := NsProgramReconcilerCommon{currentApp: nsApp("tenant-a")}
	require.Equal(t, "tenant-a", common.getNamespace())
}

// TestNsUprobeReconcilerScopesLookupToAppNamespace proves the derived
// namespace actually reaches the container lookup: the value handed to
// GetContainers -- which becomes Pods(namespace).List(...) -- is the owning
// application's namespace, not the empty string that would list pods across
// every namespace. The uprobe path is used because it calls GetContainers
// without first resolving interfaces, so no node fixture is needed; all four
// namespaced reconcilers share the same promoted getNamespace().
func TestNsUprobeReconcilerScopesLookupToAppNamespace(t *testing.T) {
	fake := &FakeContainerGetter{}
	r := &NsUprobeProgramReconciler{
		ReconcilerCommon: ReconcilerCommon{
			Containers: fake,
			Logger:     logr.Discard(),
		},
		NsProgramReconcilerCommon: NsProgramReconcilerCommon{
			currentApp: nsApp("tenant-a"),
		},
	}

	_, err := r.getExpectedLinks(context.TODO(), bpfmaniov1alpha1.UprobeAttachInfo{
		Containers: bpfmaniov1alpha1.ContainerSelector{
			Pods: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "tenant-a", fake.gotNamespace,
		"container lookup must be scoped to the owning application's namespace")
}

// labelledPod builds a pod with identical labels in the given namespace on
// the given node.
func labelledPod(name, namespace, nodeName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "test"},
		},
		Spec: v1.PodSpec{NodeName: nodeName},
	}
}

// TestGetPodsForNodeEmptyNamespaceIsAllNamespaces proves the client-go
// semantics that made the missing namespace dangerous: listing pods with an
// empty namespace returns pods from every namespace on the node, filtered
// only by the label and node-name selectors. Scoping the lookup to the
// application's namespace (above) is what confines selection to one tenant.
func TestGetPodsForNodeEmptyNamespaceIsAllNamespaces(t *testing.T) {
	ctx := context.TODO()
	nodeName := "test-node"

	clientset := clientGoFake.NewSimpleClientset(
		labelledPod("pod-a", "ns-a", nodeName),
		labelledPod("pod-b", "ns-b", nodeName),
	)

	containerGetter := RealContainerGetter{
		nodeName:  nodeName,
		clientSet: clientset,
	}

	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "test"},
	}

	// Empty namespace: expect pods from BOTH namespaces.
	podList, err := containerGetter.getPodsForNode(ctx, "", selector)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, p := range podList.Items {
		names[p.Namespace+"/"+p.Name] = true
	}
	require.Len(t, podList.Items, 2,
		"empty namespace must list pods across all namespaces; got %v", names)
	require.True(t, names["ns-a/pod-a"])
	require.True(t, names["ns-b/pod-b"])

	// Contrast: a specific namespace scopes the result to that namespace.
	podList, err = containerGetter.getPodsForNode(ctx, "ns-a", selector)
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	require.Equal(t, "ns-a", podList.Items[0].Namespace)
	require.Equal(t, "pod-a", podList.Items[0].Name)
}
