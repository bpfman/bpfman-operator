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
