/*
Copyright 2024.

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

// All fields are required unless explicitly marked optional
// +kubebuilder:validation:Required
package v1alpha1

// UprobeNsProgramInfo contains the information for the uprobe program
type UprobeNsProgramInfo struct {
	// The list of points to which the program should be attached.  The list is
	// optional and may be udated after the bpf program has been loaded
	// +optional
	AttachPoints []UprobeNsAttachInfo `json:"attach_points"`
}

type UprobeNsAttachInfo struct {
	// Function to attach the uprobe to.
	// +optional
	FunctionName string `json:"func_name"`

	// Offset added to the address of the function for uprobe.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// Library name or the absolute path to a binary or library.
	Target string `json:"target"`

	// Whether the program is a retprobe.  Default is false
	// +optional
	// +kubebuilder:default:=false
	Retprobe bool `json:"retprobe"`

	// Only execute uprobe for given process identification number (PID). If PID
	// is not provided, uprobe executes for all PIDs.
	// +optional
	Pid *int32 `json:"pid"`

	// Containers identifies the set of containers in which to attach the
	// uprobe.
	Containers ContainerNsSelector `json:"containers"`
}

type UprobeNsProgramInfoState struct {
	// List of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attach point required by the
	// reconciler
	// +optional
	AttachPoints []UprobeNsAttachInfoState `json:"attach_points"`
}

type UprobeNsAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// Function to attach the uprobe to.
	// +optional
	FunctionName string `json:"func_name"`

	// Offset added to the address of the function for uprobe.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// Library name or the absolute path to a binary or library.
	Target string `json:"target"`

	// Whether the program is a retprobe.  Default is false
	// +optional
	// +kubebuilder:default:=false
	Retprobe bool `json:"retprobe"`

	// Only execute uprobe for given process identification number (PID). If PID
	// is not provided, uprobe executes for all PIDs.
	// +optional
	Pid *int32 `json:"pid"`

	// Container pid to attach the uprobe program in.
	// +optional
	ContainerPid int32 `json:"containerpid"`
}
