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

import "context"

// GetContainerPIDsFromPod retrieves container process information for
// the specified pod. This is the recommended way to get container
// PIDs for most use cases.
func GetContainerPIDsFromPod(ctx context.Context, podName string, containerNames []string) ([]ContainerPIDInfo, error) {
	client, err := NewClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.GetContainerInfoFromPod(ctx, podName, containerNames)
}
