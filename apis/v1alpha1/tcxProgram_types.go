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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// TcxProgram is the Schema for the TcxProgram API
// +kubebuilder:printcolumn:name="BpfFunctionName",type=string,JSONPath=`.spec.bpffunctionname`
// +kubebuilder:printcolumn:name="NodeSelector",type=string,JSONPath=`.spec.nodeselector`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="Direction",type=string,JSONPath=`.spec.direction`,priority=1
// +kubebuilder:printcolumn:name="InterfaceSelector",type=string,JSONPath=`.spec.interfaceselector`,priority=1
// +kubebuilder:printcolumn:name="Position",type=string,JSONPath=`.spec.position`,priority=1
type TcxProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TcxProgramSpec `json:"spec"`
	// +optional
	Status TcxProgramStatus `json:"status,omitempty"`
}

// TcxProgramSpec defines the desired state of TcxProgram
type TcxProgramSpec struct {
	TcxProgramInfo `json:",inline"`
	BpfAppCommon   `json:",inline"`
}

// TCXPositionType defines the TCX position string
type TCXPositionType string

const (
	// TCXFirst TCX program will be the first program to execute.
	TCXFirst TCXPositionType = "First"

	// TCXLast TCX program will be the last program to execute.
	TCXLast TCXPositionType = "Last"

	// TCXBefore TCX program will be before a specific TCX program
	TCXBefore TCXPositionType = "Before"

	// TCXAfter TCX program will be after a specific TCX program
	TCXAfter TCXPositionType = "After"
)

// TcxProgramInfo defines the tc program details
type TcxProgramInfo struct {
	BpfProgramCommon `json:",inline"`

	// Selector to determine the network interface (or interfaces)
	InterfaceSelector InterfaceSelector `json:"interfaceselector"`

	// Direction specifies the direction of traffic the tcx program should
	// attach to for a given network device.
	// +kubebuilder:validation:Enum=ingress;egress
	Direction string `json:"direction"`

	// TargetProgramName defined the existing TCX program that will be used in case we need set this TCX program
	// priority against.
	//+optional
	TargetProgramName string `json:"targetprogramname,omitempty"`

	// Position defines the order of the TCX relative to other TCX attached to the same interface
	// +kubebuilder:validation:Enum:="First";"Last";"Before";"After"
	//+kubebuilder:default:=Last
	//+optional
	Position TCXPositionType `json:"position,omitempty"`

	// ExpectedRevision Attach TCX hook only if the expected revision matches.
	//+optional
	ExpectedRevision uint64 `json:"expectedRevision,omitempty"`
}

// TcxProgramStatus defines the observed state of TcProgram
type TcxProgramStatus struct {
	BpfProgramStatusCommon `json:",inline"`
}

// +kubebuilder:object:root=true
// TcxProgramList contains a list of TcxPrograms
type TcxProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TcxProgram `json:"items"`
}
