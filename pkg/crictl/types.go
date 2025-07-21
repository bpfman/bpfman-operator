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
	"encoding/json"
	"strings"
)

// ContainerPIDInfo represents container information with process ID.
type ContainerPIDInfo struct {
	PodName       string `json:"podName"`
	ContainerName string `json:"containerName"`
	ContainerID   string `json:"containerID"`
	PID           int32  `json:"pid"`
	Namespace     string `json:"namespace"`
}

// PodInfo represents a pod in the CRI runtime.
type PodInfo struct {
	ID        string            `json:"id"`
	Metadata  PodMetadata       `json:"metadata"`
	State     string            `json:"state"`
	CreatedAt int64             `json:"createdAt"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// PodMetadata contains pod metadata.
type PodMetadata struct {
	Name      string `json:"name"`
	UID       string `json:"uid"`
	Namespace string `json:"namespace"`
	Attempt   uint32 `json:"attempt"`
}

// PodsResponse represents the response from crictl pods command.
type PodsResponse struct {
	Items []PodInfo `json:"items"`
}

// ContainerInfo represents a container in the CRI runtime.
type ContainerInfo struct {
	ID        string            `json:"id"`
	PodID     string            `json:"podSandboxId"`
	Metadata  ContainerMetadata `json:"metadata"`
	Image     ContainerImageRef `json:"image"`
	ImageRef  string            `json:"imageRef"`
	State     string            `json:"state"`
	CreatedAt int64             `json:"createdAt"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// ContainerMetadata contains container metadata.
type ContainerMetadata struct {
	Name    string `json:"name"`
	Attempt uint32 `json:"attempt"`
}

// ContainerImageRef contains container image reference.
type ContainerImageRef struct {
	Image string `json:"image"`
}

// ContainersResponse represents the response from crictl ps command.
type ContainersResponse struct {
	Containers []ContainerInfo `json:"containers"`
}

// ContainerStatus contains container status details.
type ContainerStatus struct {
	ID          string            `json:"id"`
	Metadata    ContainerMetadata `json:"metadata"`
	State       string            `json:"state"`
	CreatedAt   int64             `json:"createdAt"`
	StartedAt   int64             `json:"startedAt"`
	FinishedAt  int64             `json:"finishedAt"`
	ExitCode    int32             `json:"exitCode"`
	Image       ContainerImageRef `json:"image"`
	ImageRef    string            `json:"imageRef"`
	Reason      string            `json:"reason"`
	Message     string            `json:"message"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Mounts      []ContainerMount  `json:"mounts,omitempty"`
	LogPath     string            `json:"logPath"`
}

// ContainerMount represents a container mount point.
type ContainerMount struct {
	ContainerPath string `json:"containerPath"`
	HostPath      string `json:"hostPath"`
	Readonly      bool   `json:"readonly"`
}

// InspectResponse represents the response from crictl inspect
// command.
type InspectResponse struct {
	Status ContainerStatus        `json:"status"`
	Info   map[string]interface{} `json:"info"`
}

// processInfoField processes the info field to parse nested JSON
// strings like crictl does. This matches the behavior in
// cri-tools/cmd/crictl/util.go. It returns the processed info map
// that should replace the original info field.
func processInfoField(info map[string]interface{}) map[string]interface{} {
	if info == nil {
		return nil
	}

	if infoValue, exists := info["info"]; exists {
		if strValue, ok := infoValue.(string); ok && strings.HasPrefix(strValue, "{") {
			var parsedInfo map[string]interface{}
			if err := json.Unmarshal([]byte(strValue), &parsedInfo); err == nil {
				return parsedInfo
			}
		}
	}

	processedInfo := make(map[string]interface{})
	for key, value := range info {
		if strValue, ok := value.(string); ok {
			if strings.HasPrefix(strValue, "{") {
				var genericVal map[string]interface{}
				if err := json.Unmarshal([]byte(strValue), &genericVal); err == nil {
					processedInfo[key] = genericVal
				} else {
					processedInfo[key] = strings.Trim(strValue, `"`)
				}
			} else {
				processedInfo[key] = strings.Trim(strValue, `"`)
			}
		} else {
			processedInfo[key] = value
		}
	}

	return processedInfo
}
