/*
Copyright 2023 The bpfman Authors.

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
package v1alpha1

type ClUprobeProgramInfo struct {
	// links is optional and is the list of hook points to which the UProbe
	// or URetProbe program should be attached. The eBPF program is loaded in
	// kernel memory when the BPF Application CRD is created and the selected
	// Kubernetes nodes are active. The eBPF program will not be triggered until
	// the program has also been attached to a hook point described in this list.
	// Items may be added or removed from the list at any point, causing the eBPF
	// program to be attached or detached.
	//
	// The hook point for a UProbe and URetProbe program is a user-space binary,
	// or function. By default, the eBPF program is triggered at the entry of the
	// hook point, but the hook point can be adjusted using an optional function
	// name and/or offset. Optionally, the eBPF program can be installed in a set
	// of containers or limited to a specified PID.
	// +optional
	// +kubebuilder:default:={}
	Links []ClUprobeAttachInfo `json:"links"`
}

type ClUprobeAttachInfo struct {
	// function is optional and specifies the name of user-space function to attach
	// the UProbe or URetProbe program. If not provided, the eBPF program will be
	// triggered on the entry of the target.
	// +optional
	// +kubebuilder:validation:Pattern="^[a-zA-Z][a-zA-Z0-9_]+."
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Function string `json:"function"`

	// offset is optional and the value is added to the address of the hook point
	// function.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// target is required and is the user-space library name or the absolute path
	// to a binary or library.
	Target string `json:"target"`

	// pid is optional and if provided, limits the execution of the  UProbe or
	// URetProbe to the provided process identification number (PID). If pid is not
	// provided, the UProbe or URetProbe executes for all PIDs.
	// +optional
	Pid *int32 `json:"pid"`

	// containers is an optional field that identifies the set of containers in
	// which to attach the UProbe or URetProbe program. If containers is not
	// specified, the eBPF program will be attached in the bpfman-agent container.
	// +optional
	Containers *ClContainerSelector `json:"containers"`
}

type ClUprobeProgramInfoState struct {
	// links is the list of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attach point required by the
	// reconciler
	// +optional
	// +kubebuilder:default:={}
	Links []ClUprobeAttachInfoState `json:"links"`
}

type ClUprobeAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// function to attach the uprobe to.
	// +optional
	Function string `json:"function"`

	// offset added to the address of the function for uprobe.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// target is the library name or the absolute path to a binary or library.
	Target string `json:"target"`

	// pid only execute uprobe for given process identification number (PID). If PID
	// is not provided, uprobe executes for all PIDs.
	// +optional
	Pid *int32 `json:"pid"`

	// Optional container pid to attach the uprobe program in.
	// +optional
	ContainerPid *int32 `json:"containerPid"`
}
