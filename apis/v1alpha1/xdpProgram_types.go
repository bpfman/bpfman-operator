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

// +kubebuilder:validation:Enum=aborted;drop;pass;tx;redirect;dispatcher_return
type XdpProceedOnValue string

// XdpProgramInfo contains the xdp program details
type XdpProgramInfo struct {
	// The list of points to which the program should be attached.  The list is
	// optional and may be udated after the bpf program has been loaded
	// +optional
	AttachPoints []XdpAttachInfo `json:"attach_points"`
}

type XdpAttachInfo struct {
	// Selector to determine the network interface (or interfaces)
	InterfaceSelector InterfaceSelector `json:"interfaceselector"`

	// Containers identifies the set of containers in which to attach the eBPF
	// program. If Containers is not specified, the BPF program will be attached
	// in the root network namespace.
	// +optional
	Containers *ContainerSelector `json:"containers"`

	// Priority specifies the priority of the bpf program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// ProceedOn allows the user to call other xdp programs in chain on this exit code.
	// Multiple values are supported by repeating the parameter.
	// +optional
	// +kubebuilder:validation:MaxItems=6
	// +kubebuilder:default:={pass,dispatcher_return}
	ProceedOn []XdpProceedOnValue `json:"proceedon"`
}

type XdpProgramInfoState struct {
	// List of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attach point required by the
	// reconciler
	// +optional
	AttachPoints []XdpAttachInfoState `json:"attach_points"`
}

type XdpAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// Interface name to attach the xdp program to.
	IfName string `json:"ifname"`

	// Optional container pid to attach the xdp program in.
	// +optional
	ContainerPid *int32 `json:"containerpid"`

	// Priority specifies the priority of the xdp program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// ProceedOn allows the user to call other xdp programs in chain on this exit code.
	// Multiple values are supported by repeating the parameter.
	// +kubebuilder:validation:MaxItems=6
	ProceedOn []XdpProceedOnValue `json:"proceedon"`
}
