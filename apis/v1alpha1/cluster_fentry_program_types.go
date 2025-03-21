/*
Copyright 2024 The bpfman Authors.

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

type ClFentryProgramInfo struct {
	ClFentryLoadInfo `json:",inline"`

	// The hook point for a FEntry program is a Linux kernel function. Unlike other
	// eBPF program types, an FEntry program must be provided with the target
	// function at load time. The links field is optional, but unlike other program
	// types where it represents a list of hook points, for FEntry programs it
	// contains at most one entry that determines whether the program should be
	// attached to the specified function. To attach the program, add an entry to
	// links with mode set to Attach. To detach it, remove the entry from links.
	// Although only a single entry is used, links is maintained as a list to
	// preserve consistent syntax and semantics across program types.
	// +optional
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:default:={}
	Links []ClFentryAttachInfo `json:"links"`
}

type ClFentryLoadInfo struct {
	// function is required and specifies the name of the Linux kernel function to
	// attach the FEntry program.
	// +kubebuilder:validation:Pattern="^[a-zA-Z][a-zA-Z0-9_]+."
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Function string `json:"function"`
}

type AttachTypeAttach string

const (
	Attach AttachTypeAttach = "Attach"
)

type ClFentryAttachInfo struct {
	// mode is required and its presence indicates that the FEntry program should
	// be attached. mode should be set to Attach. To detach the FEntry program,
	// remove the link entry.
	// +kubebuilder:validation:Enum=Attach;
	Mode AttachTypeAttach `json:"mode"`
}

type ClFentryProgramInfoState struct {
	ClFentryLoadInfo `json:",inline"`
	// +optional
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:default:={}
	Links []ClFentryAttachInfoState `json:"links"`
}

type ClFentryAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`
}
