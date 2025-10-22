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
	"github.com/bpfman/bpfman-operator/internal"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientGoFake "k8s.io/client-go/kubernetes/fake"
)

const (
	// global config
	testAppProgramName = "fakeAppProgram"
	testNamespace      = "bpfman"
	testBytecodePath   = "/tmp/hello.o"

	testDirectionIngress = bpfmaniov1alpha1.TCIngress
	testAttachName       = "AttachNameTest"
	testPriority         = 50
	fakeInt0             = "eth0"

	fakePodName       = "my-pod"
	fakeContainerName = "my-container-1"

	testFentryBpfFunctionName     = "FentryTest"
	testFexitBpfFunctionName      = "FexitTest"
	testUprobeBpfFunctionName     = "UprobeTest"
	testUretprobeBpfFunctionName  = "UretprobeTest"
	testKprobeBpfFunctionName     = "KprobeTest"
	testKretprobeBpfFunctionName  = "KretprobeTest"
	testTracepointBpfFunctionName = "TracepointTest"
	testTcBpfFunctionName         = "TcTest"
	testTcxBpfFunctionName        = "TcxTest"
	testXdpBpfFunctionName        = "XdpTest"
)

type FakeContainerGetter struct {
	containerList *[]ContainerInfo
}

func (f *FakeContainerGetter) GetContainers(
	ctx context.Context,
	selectorNamespace string,
	selectorPods metav1.LabelSelector,
	selectorContainerNames *[]string,
	logger logr.Logger,
) (*[]ContainerInfo, error) {
	return f.containerList, nil
}

func TestGetPods(t *testing.T) {
	ctx := context.TODO()

	// Create a fake clientset
	clientset := clientGoFake.NewSimpleClientset()

	// Create a ContainerSelector
	containerSelector := &bpfmaniov1alpha1.ClContainerSelector{
		Pods: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "test",
			},
		},
		Namespace: "default",
	}

	nodeName := "test-node"

	// Create a Pod that matches the label selector and is on the correct node
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
		},
	}

	containerGetter := RealContainerGetter{
		nodeName:  nodeName,
		clientSet: clientset,
	}

	// Add the Pod to the fake clientset
	_, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, err)

	// Call getPods and check the returned PodList
	podList, err := containerGetter.getPodsForNode(
		ctx,
		containerSelector.Namespace,
		containerSelector.Pods,
	)
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	require.Equal(t, "test-pod", podList.Items[0].Name)

	// Try another selector
	// Create a ContainerSelector
	containerSelector = &bpfmaniov1alpha1.ClContainerSelector{
		Pods: metav1.LabelSelector{
			MatchLabels: map[string]string{},
		},
	}

	podList, err = containerGetter.getPodsForNode(
		ctx,
		containerSelector.Namespace,
		containerSelector.Pods,
	)
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	require.Equal(t, "test-pod", podList.Items[0].Name)
}

// verifyBpfApplicationState validates the common properties of a BPF application state object.
// It checks that the state has the expected condition, correct node and owner labels, and proper
// finalizer. This function works with both ClusterBpfApplicationState and BpfApplicationState types,
// but panics if another type is provided.
func verifyBpfApplicationState(t *testing.T, bpfAppState any, fakeNode *v1.Node, appProgramName string,
	expectedCondition bpfmaniov1alpha1.BpfApplicationStateConditionType) {
	var conditions []metav1.Condition
	var labels map[string]string
	var finalizers []string
	var expectedFinalizer string
	switch b := bpfAppState.(type) {
	case *bpfmaniov1alpha1.ClusterBpfApplicationState:
		conditions = b.Status.Conditions
		labels = b.Labels
		finalizers = b.Finalizers
		expectedFinalizer = internal.ClBpfApplicationControllerFinalizer
	case *bpfmaniov1alpha1.BpfApplicationState:
		conditions = b.Status.Conditions
		labels = b.Labels
		finalizers = b.Finalizers
		expectedFinalizer = internal.NsBpfApplicationControllerFinalizer
	default:
		panic("invalid type for bpfAppState")
	}

	require.NotEqual(t, nil, bpfAppState)
	require.Equal(t, 1, len(conditions))
	require.Equal(t, string(expectedCondition), conditions[0].Type)
	require.Equal(t, fakeNode.Name, labels[internal.K8sHostLabel])
	require.Equal(t, appProgramName, labels[internal.BpfAppStateOwner])
	require.Equal(t, expectedFinalizer, finalizers[0])
}

// runReconciler executes a reconciliation cycle and verifies that it completes
// successfully without errors and without requiring a requeue.
func runReconciler(t *testing.T, ctx context.Context, r reconcile.Reconciler, req reconcile.Request,
	logger logr.Logger) {
	res, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	logger.Info("Reconcile result", "res:", res, "err:", err)
	// Require no requeue.
	require.False(t, res.Requeue)
}

// registerBpfApplicationScheme is a helper to register bpf application schemes for the mock reconciler.
func registerBpfApplicationScheme(s *runtime.Scheme, isClusterScoped bool, bpfApp runtime.Object) {
	if isClusterScoped {
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplication{})
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationList{})
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationState{})
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationStateList{})
	} else {
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplication{})
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationList{})
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationState{})
		s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationStateList{})
	}
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, bpfApp)
}

type MockNetNsCache map[string]*uint64

func (mnnc MockNetNsCache) GetNetNsId(path string) *uint64 {
	return mnnc[path]
}

func (mnnc MockNetNsCache) Reset() {}
