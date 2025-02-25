/*
Copyright 2023.

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

// KprobeProgramInfo contains the information for the kprobe program
type KprobeProgramInfo struct {
	// The list of points to which the program should be attached.  The list is
	// optional and may be udated after the bpf program has been loaded
	// +optional
	AttachPoints []KprobeAttachInfo `json:"attach_points"`
}

// +kubebuilder:validation:XValidation:message="offset cannot be set for kretprobes",rule="self.retprobe == false || self.offset == 0"
type KprobeAttachInfo struct {
	// Function to attach the kprobe to.
	FunctionName string `json:"func_name"`

	// Offset added to the address of the function for kprobe.
	// Not allowed for kretprobes.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// Whether the program is a retprobe.  Default is false
	// +optional
	// +kubebuilder:default:=false
	Retprobe bool `json:"retprobe"`
}

type KprobeProgramInfoState struct {
	// List of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attach point required by the
	// reconciler
	// +optional
	AttachPoints []KprobeAttachInfoState `json:"attach_points"`
}

type KprobeAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// Function to attach the kprobe to.
	FunctionName string `json:"func_name"`

	// Offset added to the address of the function for kprobe.
	// Not allowed for kretprobes.
	Offset uint64 `json:"offset"`

	// Whether the program is a retprobe.  Default is false
	// +optional
	// +kubebuilder:default:=false
	Retprobe bool `json:"retprobe"`
}
