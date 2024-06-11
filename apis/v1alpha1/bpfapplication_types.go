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

// EBPFProgType defines the supported eBPF program types
type EBPFProgType string

const (
	// ProgTypeXDP refers to the eBPF XDP programs type.
	ProgTypeXDP EBPFProgType = "XDP"

	// ProgTypeTC refers to the eBPF TC programs type.
	ProgTypeTC EBPFProgType = "TC"

	// ProgTypeTCX refers to the eBPF TCx programs type.
	ProgTypeTCX EBPFProgType = "TCX"

	// ProgTypeFentry refers to the eBPF Fentry programs type.
	ProgTypeFentry EBPFProgType = "Fentry"

	// ProgTypeFexit refers to the eBPF Fexit programs type.
	ProgTypeFexit EBPFProgType = "Fexit"

	// ProgTypeKprobe refers to the eBPF Kprobe programs type.
	ProgTypeKprobe EBPFProgType = "Kprobe"

	// ProgTypeKretprobe refers to the eBPF Kprobe programs type.
	ProgTypeKretprobe EBPFProgType = "Kretprobe"

	// ProgTypeUprobe refers to the eBPF Uprobe programs type.
	ProgTypeUprobe EBPFProgType = "Uprobe"

	// ProgTypeUretprobe refers to the eBPF Uretprobe programs type.
	ProgTypeUretprobe EBPFProgType = "Uretprobe"

	// ProgTypeTracepoint refers to the eBPF Tracepoint programs type.
	ProgTypeTracepoint EBPFProgType = "Tracepoint"
)

// BpfApplicationProgram defines the desired state of BpfApplication
type BpfApplicationProgram struct {
	// Type specifies the bpf program type
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:="XDP";"TC";"TCX";"Fentry";"Fexit";"Kprobe";"Kretprobe";"Uprobe";"Uretprobe";"Tracepoint"
	// +optional
	Type EBPFProgType `json:"type,omitempty"`

	// xdp defines the desired state of the application's XdpPrograms.
	// +unionMember
	// +optional
	XDP *XdpProgramInfo `json:"xdp,omitempty"`

	// tc defines the desired state of the application's TcPrograms.
	// +unionMember
	// +optional
	TC *TcProgramInfo `json:"tc,omitempty"`

	// fentry defines the desired state of the application's FentryPrograms.
	// +unionMember
	// +optional
	Fentry *FentryProgramInfo `json:"fentry,omitempty"`

	// fexit defines the desired state of the application's FexitPrograms.
	// +unionMember
	// +optional
	Fexit *FexitProgramInfo `json:"fexit,omitempty"`

	// kprobe defines the desired state of the application's KprobePrograms.
	// +unionMember
	// +optional
	Kprobe *KprobeProgramInfo `json:"kprobe,omitempty"`

	// kretprobe defines the desired state of the application's KretprobePrograms.
	// +unionMember
	// +optional
	Kretprobe *KprobeProgramInfo `json:"kretprobe,omitempty"`

	// uprobe defines the desired state of the application's UprobePrograms.
	// +unionMember
	// +optional
	Uprobe *UprobeProgramInfo `json:"uprobe,omitempty"`

	// uretprobe defines the desired state of the application's UretprobePrograms.
	// +unionMember
	// +optional
	Uretprobe *UprobeProgramInfo `json:"uretprobe,omitempty"`

	// tracepoint defines the desired state of the application's TracepointPrograms.
	// +unionMember
	// +optional
	Tracepoint *TracepointProgramInfo `json:"tracepoint,omitempty"`
}

// BpfApplicationSpec defines the desired state of BpfApplication
type BpfApplicationSpec struct {
	BpfAppCommon `json:",inline"`

	// Programs is a list of bpf programs supported for a specific application.
	// It's possible that the application can selectively choose which program(s)
	// to run from this list.
	// +kubebuilder:validation:MinItems:=1
	Programs []BpfApplicationProgram `json:"programs,omitempty"`
}

// BpfApplicationStatus defines the observed state of BpfApplication
type BpfApplicationStatus struct {
	BpfProgramStatusCommon `json:",inline"`
}

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// BpfApplication is the Schema for the bpfapplications API
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
type BpfApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BpfApplicationSpec   `json:"spec,omitempty"`
	Status BpfApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// BpfApplicationList contains a list of BpfApplications
type BpfApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfApplication `json:"items"`
}
