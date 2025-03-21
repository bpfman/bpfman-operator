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

// +kubebuilder:validation:Enum:=UnSpec;OK;ReClassify;Shot;Pipe;Stolen;Queued;Repeat;ReDirect;Trap;DispatcherReturn;
type TcProceedOnValue string

type ClTcProgramInfo struct {
	// links is optional and is the list of hook points to which the TC program
	// should be attached. The TC program is loaded in kernel memory when the BPF
	// Application CRD is created and the selected Kubernetes nodes are active.
	// The TC program will not be triggered until the program has also been
	// attached to an attachment point described in this list. Items may be added
	// or removed from the list at any point, causing the TC program to be
	// attached or detached.
	//
	// The hook point for a TC program is an interface (or network device). An
	// interface can be specified by name, or using the primaryNodeInterface flag,
	// which indicates to use the primary interface of a Kubernetes node or
	// container. Optionally, the TC program can be installed in a set of
	// containers.
	// +optional
	// +kubebuilder:default:={}
	Links []ClTcAttachInfo `json:"links"`
}

type ClTcAttachInfo struct {
	// interfaceSelector is required and is used to determine the network interface
	// (or interfaces) the TC program is attached. Either a list of interface
	// names (which can be a list of one name) or primaryNodeInterface flag must be
	// provided, but not both.
	InterfaceSelector InterfaceSelector `json:"interfaceSelector"`

	// containers is an optional field that identifies the set of containers in
	// which to attach the TC program. If containers is not specified, the TC
	// program will be attached in the root network namespace.
	// +optional
	Containers *ClContainerSelector `json:"containers"`

	// direction is required and specifies the direction of traffic the TC program
	// should attach to for a given network device. Allowed values are:
	//   Ingress and Egress
	// +kubebuilder:validation:Enum=Ingress;Egress
	Direction TCDirectionType `json:"direction"`

	// priority is required and specifies the order of the TC program is executed
	// in relation to other TC programs with the same hook point. It is a value
	// from 0 to 1000, where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// proceedOn is optional and allows the user to call other TC programs in a
	// chain, or not call the next program in a chain based on the exit code of
	// a TC program. Allowed values are:
	//   UnSpec, OK, ReClassify, Shot, Pipe, Stolen, Queued, Repeat, ReDirect,
	//   Trap and DispatcherReturn
	// Multiple values are supported. Default is Pipe and DispatcherReturn. So
	// using the default values, if a TC program returns Pipe, the next TC
	// program in the chain will be called. If a TC program returns Stolen, the
	// next TC program in the chain will NOT be called.
	// +optional
	// +kubebuilder:default:={Pipe,DispatcherReturn}
	ProceedOn []TcProceedOnValue `json:"proceedOn"`
}

type ClTcProgramInfoState struct {
	// links is the List of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attached point required by the
	// reconciler
	// +optional
	// +kubebuilder:default:={}
	Links []ClTcAttachInfoState `json:"links"`
}

type ClTcAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// interfaceName is the Interface name to attach the tc program to.
	InterfaceName string `json:"interfaceName"`

	// Optional container pid to attach the tc program in.
	// +optional
	ContainerPid *int32 `json:"containerPid"`

	// direction specifies the direction of traffic the tc program should
	// attach to for a given network device.
	// +kubebuilder:validation:Enum=Ingress;Egress
	Direction TCDirectionType `json:"direction"`

	// priority specifies the priority of the tc program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// proceedOn allows the user to call other tc programs in chain on this exit code.
	// Multiple values are supported by repeating the parameter.
	ProceedOn []TcProceedOnValue `json:"proceedOn"`
}
