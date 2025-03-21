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
package v1alpha1

// +kubebuilder:validation:Enum:=Aborted;Drop;Pass;TX;ReDirect;DispatcherReturn;
type XdpProceedOnValue string

type ClXdpProgramInfo struct {
	// links is optional and is the list of hook points to which the XDP program
	// should be attached. The XDP program is loaded in kernel memory when the BPF
	// Application CRD is created and the selected Kubernetes nodes are active.
	// The XDP program will not be triggered until the program has also been
	// attached to an attachment point described in this list. Items may be added
	// or removed from the list at any point, causing the XDP program to be
	// attached or detached.
	//
	// The hook point for an XDP program is a network interface (or device). The
	// interface can be specified by name, or by setting the primaryNodeInterface
	// flag, which instructs bpfman to use the primary interface of a Kubernetes
	// node. Optionally, the XDP program can also be installed into a set of
	// network namespaces.
	// +optional
	// +kubebuilder:default:={}
	Links []ClXdpAttachInfo `json:"links"`
}

type ClXdpAttachInfo struct {
	// interfaceSelector is required and is used to determine the network interface
	// (or interfaces) the XDP program is attached. Either a list of interface
	// names (which can be a list of one name) or primaryNodeInterface flag must be
	// provided, but not both.
	InterfaceSelector InterfaceSelector `json:"interfaceSelector"`

	// containers is an optional field that identifies the set of containers in
	// which to attach the XDP program. If containers is not specified, the XDP
	// program will be attached in the root network namespace.
	// +optional
	Containers *ClContainerSelector `json:"containers"`

	// priority is required and determines the execution order of the XDP program
	// relative to other XDP programs attached to the same hook point. It must be a
	// value between 0 and 1000, where lower values indicate higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// proceedOn is optional and allows the user to call other XDP programs in a
	// chain, or not call the next program in a chain based on the exit code of
	// an XDP program. Allowed values are:
	//   Aborted, Drop, Pass, TX, ReDirect and DispatcherReturn
	// Multiple values are supported. Default is Pass and DispatcherReturn. So
	// using the default values, if an XDP program returns Pass, the next XDP
	// program in the chain will be called. If an XDP program returns Drop, the
	// next XDP program in the chain will NOT be called.
	// +optional
	// +kubebuilder:default:={Pass,DispatcherReturn}
	ProceedOn []XdpProceedOnValue `json:"proceedOn"`
}

type ClXdpProgramInfoState struct {
	// links is the list of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attach point required by the
	// reconciler
	// +optional
	// +kubebuilder:default:={}
	Links []ClXdpAttachInfoState `json:"links"`
}

type ClXdpAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// interfaceName is the interface name to attach the xdp program to.
	InterfaceName string `json:"interfaceName"`

	// containerPid is an optional container pid to attach the xdp program in.
	// +optional
	ContainerPid *int32 `json:"containerPid"`

	// priority specifies the priority of the xdp program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// proceedOn allows the user to call other xdp programs in chain on this exit code.
	// Multiple values are supported by repeating the parameter.
	ProceedOn []XdpProceedOnValue `json:"proceedOn"`
}
