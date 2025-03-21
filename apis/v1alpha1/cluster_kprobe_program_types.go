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

// ClKprobeProgramInfo contains the information for the kprobe program
type ClKprobeProgramInfo struct {
	// links is optional and is the list of hook points to which the KProbe program
	// should be attached. The eBPF program is loaded in kernel memory when the BPF
	// Application CRD is created and the selected Kubernetes nodes are active. The
	// eBPF program will not be triggered until the program has also been attached
	// to a hook point described in this list. Items may be added or removed from
	// the list at any point, causing the eBPF program to be attached or detached.
	//
	// The hook point for a KProbe program is a Linux kernel function. By default,
	// the eBPF program is triggered at the entry of the hook point, but the hook
	// point can be adjusted using an optional offset.
	// +optional
	// +kubebuilder:default:={}
	Links []ClKprobeAttachInfo `json:"links"`
}

type ClKprobeAttachInfo struct {
	// function is required and specifies the name of the Linux kernel function to
	// attach the KProbe program.
	// +kubebuilder:validation:Pattern="^[a-zA-Z][a-zA-Z0-9_]+."
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Function string `json:"function"`

	// offset is optional and the value is added to the address of the hook point
	// function.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`
}

type ClKprobeProgramInfoState struct {
	// List of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attach point required by the
	// reconciler
	// +optional
	// +kubebuilder:default:={}
	Links []ClKprobeAttachInfoState `json:"links"`
}

type ClKprobeAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// Function to attach the kprobe to.
	Function string `json:"function"`

	// Offset added to the address of the function for kprobe.
	Offset uint64 `json:"offset"`
}
