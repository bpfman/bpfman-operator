/*
Copyright 2022.

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

// TracepointProgram is the Schema for the TracepointPrograms API
// +kubebuilder:printcolumn:name="BpfFunctionName",type=string,JSONPath=`.spec.bpffunctionname`
// +kubebuilder:printcolumn:name="NodeSelector",type=string,JSONPath=`.spec.nodeselector`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="TracePoint",type=string,JSONPath=`.spec.name`,priority=1
type TracepointProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TracepointProgramSpec `json:"spec"`
	Status BpfAppStatus          `json:"status,omitempty"`
}

// TracepointProgramSpec defines the desired state of TracepointProgram
// +kubebuilder:printcolumn:name="TracePoint",type=string,JSONPath=`.spec.name`
type TracepointProgramSpec struct {
	TracepointProgramInfo `json:",inline"`
	BpfAppCommon          `json:",inline"`
}

// TracepointProgramInfo defines the Tracepoint program details
type TracepointProgramInfo struct {
	BpfProgramCommon `json:",inline"`
	// The list of points to which the program should be attached.  The list is
	// optional and may be udated after the bpf program has been loaded
	// +optional
	AttachPoints []TracepointAttachInfo `json:"attach_points"`
}

type TracepointAttachInfo struct {
	// Name refers to the name of a kernel tracepoint to attach the
	// bpf program to.
	Name string `json:"name"`
}

// +kubebuilder:object:root=true
// TracepointProgramList contains a list of TracepointPrograms
type TracepointProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TracepointProgram `json:"items"`
}
