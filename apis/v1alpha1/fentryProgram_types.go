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

// FentryProgramInfo defines the Fentry program details
type FentryProgramInfo struct {
	FentryLoadInfo `json:",inline"`
	// Whether the program should be attached to the function.
	// This may be updated after the program has been loaded.
	// +optional
	FentryAttachInfo `json:",inline"`
}

// FentryLoadInfo contains the program-specific load information for Fentry
// programs
type FentryLoadInfo struct {
	// FunctionName is the name of the function to attach the Fentry program to.
	FunctionName string `json:"function_name"`
}

type FentryAttachInfo struct {
	// Whether the bpf program should be attached to the function.
	Attach bool `json:"attach"`
}

type FentryProgramInfoState struct {
	FentryLoadInfo        `json:",inline"`
	FentryAttachInfoState `json:",inline"`
}

type FentryAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`
	Attach                bool `json:"attach"`
}
