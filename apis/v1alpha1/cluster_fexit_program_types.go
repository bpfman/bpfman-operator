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

type ClFexitProgramInfo struct {
	ClFexitLoadInfo `json:",inline"`
	// The hook point for a FExit program is a Linux kernel function. Unlike a
	// KRetProbe program, the FExit function name is required at load time instead
	// of attach time. Therefore, the FExit function is not part of links.
	//
	// links is optional, but instead of being a list of hook points like other
	// eBPF program types, links for FExit is a list of one, which indicates
	// whether the FExit program should be attached or not. The FExit program is
	// loaded in kernel memory when the BPF Application CRD is created and the
	// selected Kubernetes nodes are active. The FExit program will not be
	// triggered until the program has also been attached. To attach, add an entry
	// to links with mode set to Attach. To detach, remove the link entry.
	// +optional
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:default:={}
	Links []ClFexitAttachInfo `json:"links"`
}

type ClFexitLoadInfo struct {
	// function is required and specifies the name of the Linux kernel function to
	// attach the FExit program.
	// +kubebuilder:validation:Pattern="^[a-zA-Z][a-zA-Z0-9_]+."
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Function string `json:"function"`
}

type ClFexitAttachInfo struct {
	// mode is required and its presence indicates that the FExit program should
	// be attached. mode should be set to Attach. To detach the FExit program,
	// remove the link entry.
	// +kubebuilder:validation:Enum=Attach;Detach;
	Mode AttachTypeAttach `json:"mode"`
}

type ClFexitProgramInfoState struct {
	ClFexitLoadInfo `json:",inline"`
	// +optional
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:default:={}
	Links []ClFexitAttachInfoState `json:"links"`
}

type ClFexitAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`
}
