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

// FexitProgram is the Schema for the FexitPrograms API
// +kubebuilder:printcolumn:name="BpfFunctionName",type=string,JSONPath=`.spec.bpffunctionname`
// +kubebuilder:printcolumn:name="NodeSelector",type=string,JSONPath=`.spec.nodeselector`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`,priority=1
type FexitProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FexitProgramSpec `json:"spec"`
	Status BpfAppStatus     `json:"status,omitempty"`
}

// FexitProgramSpec defines the desired state of FexitProgram
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`
type FexitProgramSpec struct {
	FexitProgramInfo `json:",inline"`
	BpfAppCommon     `json:",inline"`
}

// FexitProgramInfo defines the Fexit program details
type FexitProgramInfo struct {
	BpfProgramCommon `json:",inline"`
	FexitLoadInfo    `json:",inline"`
	// Whether the program should be attached to the function.
	// This may be updated after the program has been loaded.
	// +optional
	// +kubebuilder:default=false
	Attach bool `json:"attach,omitempty"`
}

// FexitLoadInfo contains the program-specific load information for Fexit
// programs
type FexitLoadInfo struct {
	// FunctionName is the name of the function to attach the Fexit program to.
	FunctionName string `json:"function_name"`
}

// +kubebuilder:object:root=true
// FexitProgramList contains a list of FexitPrograms
type FexitProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FexitProgram `json:"items"`
}
