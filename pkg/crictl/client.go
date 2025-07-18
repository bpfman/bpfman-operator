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
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// Client provides a high-level interface to the container runtime.
type Client struct {
	conn          *grpc.ClientConn
	runtimeClient runtime.RuntimeServiceClient
}

// NewClient creates a new CRI client by discovering the runtime
// socket. The provided context is used for testing the connection to
// the CRI runtime.
func NewClient(ctx context.Context) (*Client, error) {
	socket, err := findCRISocket()
	if err != nil {
		return nil, fmt.Errorf("finding CRI socket: %w", err)
	}

	conn, err := grpc.NewClient(socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to CRI runtime at %s: %w", socket, err)
	}

	client := runtime.NewRuntimeServiceClient(conn)
	_, err = client.Version(ctx, &runtime.VersionRequest{})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("testing connection to CRI runtime at %s: %w", socket, err)
	}

	return &Client{
		conn:          conn,
		runtimeClient: client,
	}, nil
}

// Close closes the gRPC connection to the CRI runtime.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// listPods lists pods, optionally filtering by name.
func (c *Client) listPods(ctx context.Context, nameFilter string) (*PodsResponse, error) {
	req := &runtime.ListPodSandboxRequest{
		Filter: &runtime.PodSandboxFilter{},
	}

	if nameFilter != "" {
		req.Filter.LabelSelector = map[string]string{}
	}

	resp, err := c.runtimeClient.ListPodSandbox(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("listing pod sandboxes: %w", err)
	}

	pods := make([]PodInfo, 0)
	for _, pod := range resp.Items {
		if nameFilter != "" && pod.Metadata.Name != nameFilter {
			continue
		}

		podInfo := PodInfo{
			ID: pod.Id,
			Metadata: PodMetadata{
				Name:      pod.Metadata.Name,
				UID:       pod.Metadata.Uid,
				Namespace: pod.Metadata.Namespace,
				Attempt:   pod.Metadata.Attempt,
			},
			State:     pod.State.String(),
			CreatedAt: pod.CreatedAt,
			Labels:    pod.Labels,
		}
		pods = append(pods, podInfo)
	}

	return &PodsResponse{Items: pods}, nil
}

// listContainers lists containers, optionally filtering by pod ID.
func (c *Client) listContainers(ctx context.Context, podID string) (*ContainersResponse, error) {
	req := &runtime.ListContainersRequest{
		Filter: &runtime.ContainerFilter{},
	}

	if podID != "" {
		req.Filter.PodSandboxId = podID
	}

	resp, err := c.runtimeClient.ListContainers(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	containers := make([]ContainerInfo, 0)
	for _, container := range resp.Containers {
		containerInfo := ContainerInfo{
			ID:    container.Id,
			PodID: container.PodSandboxId,
			Metadata: ContainerMetadata{
				Name:    container.Metadata.Name,
				Attempt: container.Metadata.Attempt,
			},
			Image: ContainerImageRef{
				Image: container.Image.Image,
			},
			ImageRef:  container.ImageRef,
			State:     container.State.String(),
			CreatedAt: container.CreatedAt,
			Labels:    container.Labels,
		}
		containers = append(containers, containerInfo)
	}

	return &ContainersResponse{Containers: containers}, nil
}

// inspectContainer returns detailed information about a specific
// container.
func (c *Client) inspectContainer(ctx context.Context, containerID string) (*InspectResponse, error) {
	req := &runtime.ContainerStatusRequest{
		ContainerId: containerID,
		Verbose:     true,
	}

	resp, err := c.runtimeClient.ContainerStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("getting container status for %s: %w", containerID, err)
	}

	status := resp.Status
	info := resp.Info

	mounts := make([]ContainerMount, len(status.Mounts))
	for i, mount := range status.Mounts {
		mounts[i] = ContainerMount{
			ContainerPath: mount.ContainerPath,
			HostPath:      mount.HostPath,
			Readonly:      mount.Readonly,
		}
	}

	containerStatus := ContainerStatus{
		ID: status.Id,
		Metadata: ContainerMetadata{
			Name:    status.Metadata.Name,
			Attempt: status.Metadata.Attempt,
		},
		State:      status.State.String(),
		CreatedAt:  status.CreatedAt,
		StartedAt:  status.StartedAt,
		FinishedAt: status.FinishedAt,
		ExitCode:   status.ExitCode,
		Image: ContainerImageRef{
			Image: status.Image.Image,
		},
		ImageRef:    status.ImageRef,
		Reason:      status.Reason,
		Message:     status.Message,
		Labels:      status.Labels,
		Annotations: status.Annotations,
		Mounts:      mounts,
		LogPath:     status.LogPath,
	}

	infoMap := make(map[string]interface{})
	for key, value := range info {
		infoMap[key] = value
	}

	return &InspectResponse{
		Status: containerStatus,
		Info:   processInfoField(infoMap),
	}, nil
}

// GetContainerInfoFromPod retrieves container information for the
// specified pod.
func (c *Client) GetContainerInfoFromPod(ctx context.Context, podName string, containerNames []string) ([]ContainerPIDInfo, error) {
	podsResp, err := c.listPods(ctx, podName)
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	containersResp, err := c.listContainers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	inspectResults := make(map[string]map[string]interface{})
	for _, container := range containersResp.Containers {
		inspectResp, err := c.inspectContainer(ctx, container.ID)
		if err != nil {
			return nil, fmt.Errorf("inspecting container %s: %w", container.ID, err)
		}
		inspectResults[container.ID] = inspectResp.Info
	}

	return processContainerInfoFromPod(
		podName,
		containerNames,
		podsResp.Items,
		containersResp.Containers,
		inspectResults,
	)
}

// Public methods for CLI usage.

// ListPods lists pods, optionally filtering by name.
func (c *Client) ListPods(ctx context.Context, nameFilter string) (*PodsResponse, error) {
	return c.listPods(ctx, nameFilter)
}

// ListContainers lists containers, optionally filtering by pod ID.
func (c *Client) ListContainers(ctx context.Context, podID string) (*ContainersResponse, error) {
	return c.listContainers(ctx, podID)
}

// InspectContainer returns detailed information about a specific
// container.
func (c *Client) InspectContainer(ctx context.Context, containerID string) (*InspectResponse, error) {
	return c.inspectContainer(ctx, containerID)
}
