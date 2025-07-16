/*
Copyright 2025 The bpfman Authors.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this file except in compliance with the License. You may obtain a copy of the
License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
*/

package bpfmanagent

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/bpfman/bpfman-operator/pkg/crictl"
	"github.com/go-logr/logr"
)

const (
	// containerDiscoveryTimeout is the maximum time to wait for
	// container PID discovery from the container runtime
	// interface (CRI).
	containerDiscoveryTimeout = 10 * time.Second
)

type ContainerInfo struct {
	podName       string
	containerName string
	pid           int32
}

// Create an interface for getting the list of containers in which the program
// should be attached so we can mock it in unit tests.
type ContainerGetter interface {
	// Get the list of containers on this node that match the containerSelector.
	GetContainers(ctx context.Context,
		selectorNamespace string,
		selectorPods metav1.LabelSelector,
		selectorContainerNames *[]string,
		logger logr.Logger) (*[]ContainerInfo, error)
}

type RealContainerGetter struct {
	nodeName  string
	clientSet kubernetes.Interface
}

func NewRealContainerGetter(nodeName string) (*RealContainerGetter, error) {
	clientSet, err := getClientset()
	if err != nil {
		return nil, fmt.Errorf("failed to get clientset: %v", err)
	}

	containerGetter := RealContainerGetter{
		nodeName:  nodeName,
		clientSet: clientSet,
	}

	return &containerGetter, nil
}

func (c *RealContainerGetter) GetContainers(
	ctx context.Context,
	selectorNamespace string,
	selectorPods metav1.LabelSelector,
	selectorContainerNames *[]string,
	logger logr.Logger) (*[]ContainerInfo, error) {

	// Get the list of pods that match the selector.
	podList, err := c.getPodsForNode(ctx, selectorNamespace, selectorPods)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod list: %v", err)
	}

	// Get the list of containers in the list of pods that match the selector.
	containerList, err := getContainerInfo(ctx, podList, selectorContainerNames, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get container info: %v", err)
	}

	for i, container := range *containerList {
		logger.V(1).Info("Container", "index", i, "PodName", container.podName,
			"ContainerName", container.containerName, "PID", container.pid)
	}

	return containerList, nil
}

// getPodsForNode returns a list of pods on the given node that match the given
// container selector.
func (c *RealContainerGetter) getPodsForNode(
	ctx context.Context,
	selectorNamespace string,
	selectorPods metav1.LabelSelector,
) (*v1.PodList, error) {

	selectorString := metav1.FormatLabelSelector(&selectorPods)

	if selectorString == "<error>" {
		return nil, fmt.Errorf("error parsing selector: %v", selectorString)
	}

	listOptions := metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + c.nodeName,
	}

	if selectorString != "<none>" {
		listOptions.LabelSelector = selectorString
	}

	podList, err := c.clientSet.CoreV1().Pods(selectorNamespace).List(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("error getting pod list: %v", err)
	}

	return podList, nil
}

// getContainerInfo returns container metadata for pods in the given
// podList, applying optional container filtering.
//
// The containerNames parameter is a pointer to a slice of container names,
// and its value determines filtering semantics:
//
//   - nil:         No container filtering — return info for *all* containers in all matching pods.
//   - empty slice: Currently behaves the same as nil — returns *all* containers.
//   - non-empty:   Return only containers whose names match elements in the slice.
//
// This tri-state contract is intentional. Do not simplify
// containerNames to a plain []string without first understanding the
// caller’s expectations.
//
// For example, some programs (e.g., XDP or TC probes) want *all*
// containers in a pod, while others (e.g., uprobes) only want
// specifically annotated ones.
//
// Returns a slice of ContainerInfo for the matched containers or an
// error if discovery fails.
//
// Note: We claim 3 states because there's exactly 3 states the
// variable could be in (nil, empty slice, non-empty slice), but
// functionally there are only 2. Empty slice behaves the same as nil
// — both return all containers. This behaviour is left unchanged to
// preserve existing semantics. The underlying
// crictl.filterContainersByNames function maintains this same
// historic choice.
func getContainerInfo(ctx context.Context, podList *v1.PodList, containerNames *[]string, logger logr.Logger) (*[]ContainerInfo, error) {
	containers := []ContainerInfo{}

	for i, pod := range podList.Items {
		logger.V(1).Info("Pod", "index", i, "Name", pod.Name, "Namespace", pod.Namespace, "NodeName", pod.Spec.NodeName)

		containerInfos, err := getContainerInfoFromPod(ctx, pod.Name, containerNames, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to get container info for pod %s: %w", pod.Name, err)
		}

		containers = append(containers, containerInfos...)
	}

	return &containers, nil
}

// getContainerInfoFromPod returns container metadata for a single
// pod, filtered by the provided containerNames, following the same
// tri-state filtering contract as getContainerInfo.
//
// If containerNames is nil, all containers in the pod are returned.
// If it points to an empty slice, no containers will be selected. If
// it contains names, only matching containers are included.
func getContainerInfoFromPod(ctx context.Context, podName string, containerNames *[]string, logger logr.Logger) ([]ContainerInfo, error) {
	// Convert containerNames to slice if provided
	var nameSlice []string
	if containerNames != nil {
		nameSlice = *containerNames
	}

	crictlCtx, cancel := context.WithTimeout(ctx, containerDiscoveryTimeout)
	defer cancel()

	pidInfos, err := crictl.GetContainerPIDsFromPod(crictlCtx, podName, nameSlice)
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, len(pidInfos))
	for i, info := range pidInfos {
		result[i] = ContainerInfo{
			podName:       info.PodName,
			containerName: info.ContainerName,
			pid:           info.PID,
		}
		logger.V(0).Info("Container PID discovered",
			"namespace", info.Namespace,
			"pod", info.PodName,
			"container", info.ContainerName,
			"pid", info.PID)
	}

	return result, nil
}

func GetOneContainerPerPod(containers *[]ContainerInfo) *[]ContainerInfo {
	uniquePods := make(map[string]bool)
	uniqueContainers := []ContainerInfo{}
	for _, container := range *containers {
		if _, ok := uniquePods[container.podName]; !ok {
			uniquePods[container.podName] = true
			uniqueContainers = append(uniqueContainers, container)
		}
	}

	return &uniqueContainers
}
