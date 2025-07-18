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

package crictl

import (
	"fmt"
)

// findPodByNameWithNamespace returns the pod object for the named
// pod, or nil if not found.
func findPodByNameWithNamespace(pods []PodInfo, podName string) *PodInfo {
	for _, pod := range pods {
		if pod.Metadata.Name == podName {
			return &pod
		}
	}
	return nil
}

// filterContainersByNames returns containers matching the specified
// names. Empty or nil containerNames returns all containers for
// compatibility.
func filterContainersByNames(containers []ContainerInfo, containerNames []string) []ContainerInfo {
	if len(containerNames) == 0 {
		return containers
	}

	var filtered []ContainerInfo
	for _, container := range containers {
		found := false
		for _, name := range containerNames {
			if name == container.Metadata.Name {
				found = true
				break
			}
		}
		if found {
			filtered = append(filtered, container)
		}
	}
	return filtered
}

// extractPIDFromInspectInfo extracts the process ID from container
// inspect data.
func extractPIDFromInspectInfo(info map[string]interface{}) (int32, error) {
	if pidValue, exists := info["pid"]; exists {
		return convertPIDValue(pidValue)
	}

	return 0, fmt.Errorf("PID not found in inspect info")
}

// convertPIDValue converts PID values from various types to int32.
func convertPIDValue(pidValue interface{}) (int32, error) {
	switch v := pidValue.(type) {
	case int64:
		return int32(v), nil
	case float64:
		return int32(v), nil
	case int:
		return int32(v), nil
	case int32:
		return v, nil
	default:
		return 0, fmt.Errorf("unexpected PID type %T: %v", v, v)
	}
}

// buildContainerPIDInfo constructs a ContainerPIDInfo from container
// metadata and PID.
func buildContainerPIDInfo(podName string, namespace string, container ContainerInfo, pid int32) ContainerPIDInfo {
	return ContainerPIDInfo{
		PodName:       podName,
		ContainerName: container.Metadata.Name,
		ContainerID:   container.ID,
		PID:           pid,
		Namespace:     namespace,
	}
}

// processContainerInfoFromPod extracts container PID information from
// pod data.
func processContainerInfoFromPod(
	podName string,
	containerNames []string,
	pods []PodInfo,
	containers []ContainerInfo,
	inspectResults map[string]map[string]interface{},
) ([]ContainerPIDInfo, error) {
	pod := findPodByNameWithNamespace(pods, podName)
	if pod == nil {
		return nil, fmt.Errorf("pod %s not found", podName)
	}

	var podContainers []ContainerInfo
	for _, container := range containers {
		if container.PodID == pod.ID {
			podContainers = append(podContainers, container)
		}
	}

	filteredContainers := filterContainersByNames(podContainers, containerNames)

	var result []ContainerPIDInfo
	for _, container := range filteredContainers {
		inspectInfo, exists := inspectResults[container.ID]
		if !exists {
			return nil, fmt.Errorf("inspect info not found for container %s", container.ID)
		}

		pid, err := extractPIDFromInspectInfo(inspectInfo)
		if err != nil {
			return nil, fmt.Errorf("container %s: %w", container.ID, err)
		}

		result = append(result, buildContainerPIDInfo(podName, pod.Metadata.Namespace, container, pid))
	}

	return result, nil
}
