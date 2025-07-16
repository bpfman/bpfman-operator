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
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// findCRISocket discovers the CRI runtime socket path.
func findCRISocket() (string, error) {
	if endpoint := os.Getenv("CONTAINER_RUNTIME_ENDPOINT"); endpoint != "" {
		return endpoint, nil
	}

	// Use the same default endpoints as crictl, plus legacy paths
	// for compatibility crictl's defaultRuntimeEndpoints.
	endpoints := []string{
		"unix:///run/containerd/containerd.sock", // containerd (crictl default)
		"unix:///run/crio/crio.sock",             // CRI-O (crictl default)
		"unix:///var/run/cri-dockerd.sock",       // cri-dockerd (crictl default)

		// Legacy paths for broader compatibility
		"unix:///var/run/containerd/containerd.sock", // containerd (legacy)
		"unix:///var/run/crio/crio.sock",             // CRI-O (legacy)
		"unix:///var/run/dockershim.sock",            // dockershim (legacy)
	}

	timeout := 2 * time.Second

	for _, endpoint := range endpoints {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		if testConnection(ctx, endpoint) {
			cancel()
			return endpoint, nil
		}
		cancel()
	}

	return "", fmt.Errorf("no CRI socket found at any of the expected locations: %v", endpoints)
}

// testConnection checks whether the given endpoint is a valid CRI
// runtime socket. Returns true if the socket exists and responds to a
// Version request.
func testConnection(ctx context.Context, endpoint string) bool {
	var socketPath string
	if strings.HasPrefix(endpoint, "unix://") {
		socketPath = endpoint[7:]
	} else {
		socketPath = endpoint
	}

	if info, err := os.Stat(socketPath); err != nil {
		return false
	} else if info.Mode()&os.ModeSocket == 0 {
		return false
	}

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false
	}
	defer conn.Close()

	client := runtime.NewRuntimeServiceClient(conn)
	_, err = client.Version(ctx, &runtime.VersionRequest{})
	return err == nil
}
