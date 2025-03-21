/*
Copyright 2024 The bpfman Authors.

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

// ClTcxProgramInfo defines the tcx program details
type ClTcxProgramInfo struct {
	// links is optional and is the list of hook points to which the TCX program
	// should be attached. The TCX program is loaded in kernel memory when the BPF
	// Application CRD is created and the selected Kubernetes nodes are active.
	// The TCX program will not be triggered until the program has also been
	// attached to an attachment point described in this list. Items may be added
	// or removed from the list at any point, causing the TCX program to be
	// attached or detached.
	//
	// The hook point for a TCX program is a network interface (or device). The
	// interface can be specified by name, or by setting the primaryNodeInterface
	// flag, which instructs bpfman to use the primary interface of a Kubernetes
	// node. Optionally, the TCX program can also be installed into a set of
	// network namespaces.
	// +optional
	// +kubebuilder:default:={}
	Links []ClTcxAttachInfo `json:"links"`
}

type ClTcxAttachInfo struct {
	// interfaceSelector is required and is used to determine the network interface
	// (or interfaces) the TCX program is attached. Either a list of interface
	// names (which can be a list of one name) or primaryNodeInterface flag must be
	// provided, but not both.
	InterfaceSelector InterfaceSelector `json:"interfaceSelector"`

	// containers is an optional field that identifies the set of containers in
	// which to attach the TCX program. If containers is not specified, the TCX
	// program will be attached in the root network namespace.
	// +optional
	Containers *ClContainerSelector `json:"containers"`

	// direction is required and specifies the direction of traffic, Ingress or
	// Egress, that the TCX program should attach to for a given network device.
	// +kubebuilder:validation:Enum=Ingress;Egress
	Direction TCDirectionType `json:"direction"`

	// priority is required and determines the execution order of the TCX program
	// relative to other TCX programs attached to the same hook point. It must be a
	// value between 0 and 1000, where lower values indicate higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`
}

type ClTcxProgramInfoState struct {
	// List of attach points for the BPF program on the given node. Each entry
	// in *AttachInfoState represents a specific, unique attach point that is
	// derived from *AttachInfo by fully expanding any selectors.  Each entry
	// also contains information about the attach point required by the
	// reconciler
	// +optional
	// +kubebuilder:default:={}
	Links []ClTcxAttachInfoState `json:"links"`
}

type ClTcxAttachInfoState struct {
	AttachInfoStateCommon `json:",inline"`

	// interfaceName is the Interface name to attach the tc program to.
	InterfaceName string `json:"interfaceName"`

	// containerPid is the container pid to attach the tcx program in.
	// +optional
	ContainerPid *int32 `json:"containerPid"`

	// direction specifies the direction of traffic the tcx program should
	// attach to for a given network device.
	// +kubebuilder:validation:Enum=Ingress;Egress
	Direction TCDirectionType `json:"direction"`

	// priority specifies the priority of the tcx program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`
}
