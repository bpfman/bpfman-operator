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

// FexitProgramInfo defines the Fexit program details
type FexitProgramInfo struct {
	FexitLoadInfo `json:",inline"`
	// Whether the program should be attached to the function.
	// This may be updated after the program has been loaded.
	// +optional
	FexitAttachInfo `json:",inline"`
}

// FexitLoadInfo contains the program-specific load information for Fexit
// programs
type FexitLoadInfo struct {
	// FunctionName is the name of the function to attach the Fexit program to.
	FunctionName string `json:"function_name"`
}

type FexitAttachInfo struct {
	Attach bool `json:"attach"`
}

type FexitProgramInfoState struct {
	FexitLoadInfo        `json:",inline"`
	FexitAttachInfoState `json:",inline"`
}

type FexitAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`
	Attach                bool `json:"attach"`
}
