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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BpfApplicationProgram defines the desired state of BpfApplication
// +union
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'XDP' ?  has(self.xdp) : !has(self.xdp)",message="xdp configuration is required when type is XDP, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'TC' ?  has(self.tc) : !has(self.tc)",message="tc configuration is required when type is TC, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'TCX' ?  has(self.tcx) : !has(self.tcx)",message="tcx configuration is required when type is TCX, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Fentry' ?  has(self.fentry) : !has(self.fentry)",message="fentry configuration is required when type is Fentry, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Fexit' ?  has(self.fexit) : !has(self.fexit)",message="fexit configuration is required when type is Fexit, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Kprobe' ?  has(self.kprobe) : !has(self.kprobe)",message="kprobe configuration is required when type is Kprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Kretprobe' ?  has(self.kretprobe) : !has(self.kretprobe)",message="kretprobe configuration is required when type is Kretprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Uprobe' ?  has(self.uprobe) : !has(self.uprobe)",message="uprobe configuration is required when type is Uprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Uretprobe' ?  has(self.uretprobe) : !has(self.uretprobe)",message="uretprobe configuration is required when type is Uretprobe, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Tracepoint' ?  has(self.tracepoint) : !has(self.tracepoint)",message="tracepoint configuration is required when type is Tracepoint, and forbidden otherwise"
type BpfApplicationProgramNode struct {
	// Type specifies the bpf program type
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:="XDP";"TC";"TCX";"Fentry";"Fexit";"Kprobe";"Kretprobe";"Uprobe";"Uretprobe";"Tracepoint"
	Type EBPFProgType `json:"type,omitempty"`

	// xdp defines the desired state of the application's XdpPrograms.
	// +unionMember
	// +optional
	XDP *XdpProgramInfoNode `json:"xdp,omitempty"`

	// tc defines the desired state of the application's TcPrograms.
	// +unionMember
	// +optional
	TC *TcProgramInfoNode `json:"tc,omitempty"`

	// // tcx defines the desired state of the application's TcxPrograms.
	// // +unionMember
	// // +optional
	// TCX *TcxProgramInfoNode `json:"tcx,omitempty"`

	// fentry defines the desired state of the application's FentryPrograms.
	// +unionMember
	// +optional
	Fentry *FentryProgramInfoNode `json:"fentry,omitempty"`

	// // fexit defines the desired state of the application's FexitPrograms.
	// // +unionMember
	// // +optional
	// Fexit *FexitProgramInfoNode `json:"fexit,omitempty"`

	// // kprobe defines the desired state of the application's KprobePrograms.
	// // +unionMember
	// // +optional
	// Kprobe *KprobeProgramInfoNode `json:"kprobe,omitempty"`

	// // kretprobe defines the desired state of the application's KretprobePrograms.
	// // +unionMember
	// // +optional
	// Kretprobe *KprobeProgramInfoNode `json:"kretprobe,omitempty"`

	// // uprobe defines the desired state of the application's UprobePrograms.
	// // +unionMember
	// // +optional
	// Uprobe *UprobeProgramInfoNode `json:"uprobe,omitempty"`

	// // uretprobe defines the desired state of the application's UretprobePrograms.
	// // +unionMember
	// // +optional
	// Uretprobe *UprobeProgramInfoNode `json:"uretprobe,omitempty"`

	// // tracepoint defines the desired state of the application's TracepointPrograms.
	// // +unionMember
	// // +optional
	// Tracepoint *TracepointProgramInfoNode `json:"tracepoint,omitempty"`
}

// BpfApplicationSpec defines the desired state of BpfApplication
type BpfApplicationNodeSpec struct {
	// Programs is a list of bpf programs contained in the parent application.
	// It is a map from the bpf program name to BpfApplicationProgramNode
	// elements.
	Programs map[string]BpfApplicationProgramNode `json:"programs,omitempty"`
}

// BpfApplicationStatus defines the observed state of BpfApplication
type BpfApplicationNodeStatus struct {
	BpfAppStatus `json:",inline"`
}

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// BpfApplicationNode is the Schema for the bpfapplications API
// +kubebuilder:printcolumn:name="NodeSelector",type=string,JSONPath=`.spec.nodeselector`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type BpfApplicationNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BpfApplicationNodeSpec `json:"spec,omitempty"`
	Status BpfAppStatus           `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// BpfApplicationList contains a list of BpfApplications
type BpfApplicationNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfApplicationNode `json:"items"`
}
