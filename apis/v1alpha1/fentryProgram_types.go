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

// FentryProgram is the Schema for the FentryPrograms API
// +kubebuilder:printcolumn:name="BpfFunctionName",type=string,JSONPath=`.spec.bpffunctionname`
// +kubebuilder:printcolumn:name="NodeSelector",type=string,JSONPath=`.spec.nodeselector`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`,priority=1
type FentryProgram struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec FentryProgramSpec `json:"spec"`
}

// FentryProgramSpec defines the desired state of FentryProgram
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`
type FentryProgramSpec struct {
	FentryProgramInfo `json:",inline"`
	BpfAppCommon      `json:",inline"`
}

// FentryProgramInfo defines the Fentry program details
type FentryProgramInfo struct {
	BpfProgramCommon `json:",inline"`
	FentryLoadInfo   `json:",inline"`
	// Whether the program should be attached to the function.  
	// This may be updated after the program has been loaded. 
	// +optional
	// +kubebuilder:default=false
	attach bool `json:"attach,omitempty"`
}

// FentryLoadInfo contains the program-specific load information for Fentry
// programs
type FentryLoadInfo struct {
	// FunctionName is the name of the function to attach the Fentry program to.
	FunctionName string `json:"function_name"`
}

// +kubebuilder:object:root=true
// FentryProgramList contains a list of FentryPrograms
type FentryProgramList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FentryProgram `json:"items"`
}
