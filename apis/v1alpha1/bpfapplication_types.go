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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InterfaceSelector defines interface to attach to.
// +kubebuilder:validation:MaxProperties=1
// +kubebuilder:validation:MinProperties=1
type InterfaceSelector struct {
	// Interfaces refers to a list of network interfaces to attach the BPF
	// program to.
	// +optional
	Interfaces *[]string `json:"interfaces,omitempty"`

	// Attach BPF program to the primary interface on the node. Only 'true' accepted.
	// +optional
	PrimaryNodeInterface *bool `json:"primarynodeinterface,omitempty"`
}

// ContainerSelector identifies a set of containers. For example, this can be
// used to identify a set of containers in which to attach uprobes.
type ContainerSelector struct {
	// Target namespaces.
	// +optional
	// +kubebuilder:default:=""
	Namespace string `json:"namespace"`

	// Target pods. This field must be specified, to select all pods use
	// standard metav1.LabelSelector semantics and make it empty.
	Pods metav1.LabelSelector `json:"pods"`

	// Name(s) of container(s).  If none are specified, all containers in the
	// pod are selected.
	// +optional
	ContainerNames *[]string `json:"containernames,omitempty"`
}

// BpfProgramCommon defines the common attributes for all BPF programs
type BpfProgramCommon struct {
	// BpfFunctionName is the name of the function that is the entry point for the BPF
	// program
	BpfFunctionName string `json:"bpffunctionname"`

	// Bytecode configures where the bpf program's bytecode should be loaded
	// from.
	ByteCode BytecodeSelector `json:"bytecode"`

	// MapOwnerSelector is used to select the loaded eBPF program this eBPF program
	// will share a map with. The value is a label applied to the BpfProgram to select.
	// The selector must resolve to exactly one instance of a BpfProgram on a given node
	// or the eBPF program will not load.
	// +optional
	MapOwnerSelector metav1.LabelSelector `json:"mapownerselector"`
}

// PullPolicy describes a policy for if/when to pull a container image
// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
type PullPolicy string

const (
	// PullAlways means that bpfman always attempts to pull the latest bytecode image. Container will fail If the pull fails.
	PullAlways PullPolicy = "Always"
	// PullNever means that bpfman never pulls an image, but only uses a local image. Container will fail if the image isn't present
	PullNever PullPolicy = "Never"
	// PullIfNotPresent means that bpfman pulls if the image isn't present on disk. Container will fail if the image isn't present and the pull fails.
	PullIfNotPresent PullPolicy = "IfNotPresent"
)

// BytecodeSelector defines the various ways to reference bpf bytecode objects.
type BytecodeSelector struct {
	// Image used to specify a bytecode container image.
	Image *BytecodeImage `json:"image,omitempty"`

	// Path is used to specify a bytecode object via filepath.
	Path *string `json:"path,omitempty"`
}

// BytecodeImage defines how to specify a bytecode container image.
type BytecodeImage struct {
	// Valid container image URL used to reference a remote bytecode image.
	Url string `json:"url"`

	// PullPolicy describes a policy for if/when to pull a bytecode image. Defaults to IfNotPresent.
	// +kubebuilder:default:=IfNotPresent
	// +optional
	ImagePullPolicy PullPolicy `json:"imagepullpolicy"`

	// ImagePullSecret is the name of the secret bpfman should use to get remote image
	// repository secrets.
	// +optional
	ImagePullSecret *ImagePullSecretSelector `json:"imagepullsecret,omitempty"`
}

// ImagePullSecretSelector defines the name and namespace of an image pull secret.
type ImagePullSecretSelector struct {
	// Name of the secret which contains the credentials to access the image repository.
	Name string `json:"name"`

	// Namespace of the secret which contains the credentials to access the image repository.
	Namespace string `json:"namespace"`
}

// EBPFProgType defines the supported eBPF program types
type EBPFProgType string

const (
	// ProgTypeXDP refers to the eBPF XDP programs type.
	ProgTypeXDP EBPFProgType = "XDP"

	// ProgTypeTC refers to the eBPF TC programs type.
	ProgTypeTC EBPFProgType = "TC"

	// ProgTypeTCX refers to the eBPF TCx programs type.
	ProgTypeTCX EBPFProgType = "TCX"

	// ProgTypeFentry refers to the eBPF Fentry programs type.
	ProgTypeFentry EBPFProgType = "Fentry"

	// ProgTypeFexit refers to the eBPF Fexit programs type.
	ProgTypeFexit EBPFProgType = "Fexit"

	// ProgTypeKprobe refers to the eBPF Kprobe programs type.
	ProgTypeKprobe EBPFProgType = "Kprobe"

	// ProgTypeKretprobe refers to the eBPF Kprobe programs type.
	ProgTypeKretprobe EBPFProgType = "Kretprobe"

	// ProgTypeUprobe refers to the eBPF Uprobe programs type.
	ProgTypeUprobe EBPFProgType = "Uprobe"

	// ProgTypeUretprobe refers to the eBPF Uretprobe programs type.
	ProgTypeUretprobe EBPFProgType = "Uretprobe"

	// ProgTypeTracepoint refers to the eBPF Tracepoint programs type.
	ProgTypeTracepoint EBPFProgType = "Tracepoint"
)

// +kubebuilder:validation:Enum=aborted;drop;pass;tx;redirect;dispatcher_return
type XdpProceedOnValue string

// XdpProgramSpec defines the desired state of XdpProgram
type XdpProgramSpec struct {
	BpfProgramCommon `json:",inline"`

	// Selector to determine the network interface (or interfaces)
	InterfaceSelector InterfaceSelector `json:"interfaceselector"`

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

// +kubebuilder:validation:Enum=unspec;ok;reclassify;shot;pipe;stolen;queued;repeat;redirect;trap;dispatcher_return
type TcProceedOnValue string

// TcProgramSpec defines the desired state of TcProgram
type TcProgramSpec struct {
	BpfProgramCommon `json:",inline"`

	// Selector to determine the network interface (or interfaces)
	InterfaceSelector InterfaceSelector `json:"interfaceselector"`

	// Priority specifies the priority of the tc program in relation to
	// other programs of the same type with the same attach point. It is a value
	// from 0 to 1000 where lower values have higher precedence.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Priority int32 `json:"priority"`

	// Direction specifies the direction of traffic the tc program should
	// attach to for a given network device.
	// +kubebuilder:validation:Enum=ingress;egress
	Direction string `json:"direction"`

	// ProceedOn allows the user to call other tc programs in chain on this exit code.
	// Multiple values are supported by repeating the parameter.
	// +optional
	// +kubebuilder:validation:MaxItems=11
	// +kubebuilder:default:={pipe,dispatcher_return}
	ProceedOn []TcProceedOnValue `json:"proceedon"`
}

// FentryProgramSpec defines the desired state of FentryProgram
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`
type FentryProgramSpec struct {
	BpfProgramCommon `json:",inline"`

	// Function to attach the fentry to.
	FunctionName string `json:"func_name"`
}

// KprobeProgramSpec defines the desired state of KprobeProgram
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`
// +kubebuilder:printcolumn:name="Offset",type=integer,JSONPath=`.spec.offset`
// +kubebuilder:printcolumn:name="RetProbe",type=boolean,JSONPath=`.spec.retprobe`
// +kubebuilder:validation:XValidation:message="offset cannot be set for kretprobes",rule="self.retprobe == false || self.offset == 0"
type KprobeProgramSpec struct {
	BpfProgramCommon `json:",inline"`

	// Functions to attach the kprobe to.
	FunctionName string `json:"func_name"`

	// Offset added to the address of the function for kprobe.
	// Not allowed for kretprobes.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// Whether the program is a kretprobe.  Default is false
	// +optional
	// +kubebuilder:default:=false
	RetProbe bool `json:"retprobe"`

	// // Host PID of container to attach the uprobe in. (Not supported yet by bpfman.)
	// // +optional
	// ContainerPid string `json:"containerpid"`
}

// UprobeProgramSpec defines the desired state of UprobeProgram
// +kubebuilder:printcolumn:name="FunctionName",type=string,JSONPath=`.spec.func_name`
// +kubebuilder:printcolumn:name="Offset",type=integer,JSONPath=`.spec.offset`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target`
// +kubebuilder:printcolumn:name="RetProbe",type=boolean,JSONPath=`.spec.retprobe`
// +kubebuilder:printcolumn:name="Pid",type=integer,JSONPath=`.spec.pid`
type UprobeProgramSpec struct {
	BpfProgramCommon `json:",inline"`

	// Function to attach the uprobe to.
	// +optional
	FunctionName string `json:"func_name"`

	// Offset added to the address of the function for uprobe.
	// +optional
	// +kubebuilder:default:=0
	Offset uint64 `json:"offset"`

	// Library name or the absolute path to a binary or library.
	Target string `json:"target"`

	// Whether the program is a uretprobe.  Default is false
	// +optional
	// +kubebuilder:default:=false
	RetProbe bool `json:"retprobe"`

	// Only execute uprobe for given process identification number (PID). If PID
	// is not provided, uprobe executes for all PIDs.
	// +optional
	Pid int32 `json:"pid"`

	// Containers identifes the set of containers in which to attach the uprobe.
	// If Containers is not specified, the uprobe will be attached in the
	// bpfman-agent container.  The ContainerSelector is very flexible and even
	// allows the selection of all containers in a cluster.  If an attempt is
	// made to attach uprobes to too many containers, it can have a negative
	// impact on the cluster.
	// +optional
	Containers *ContainerSelector `json:"containers"`
}

// TracepointProgramSpec defines the desired state of TracepointProgram
// +kubebuilder:printcolumn:name="TracePoint",type=string,JSONPath=`.spec.name`
type TracepointProgramSpec struct {
	BpfProgramCommon `json:",inline"`

	// Names refers to the names of kernel tracepoints to attach the
	// bpf program to.
	Names []string `json:"names"`
}

// BpfApplicationProgram defines the desired state of BpfApplication
type BpfApplicationProgram struct {
	// Type specifies the bpf program type
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:="XDP";"TC";"TCX";"Fentry";"Fexit";"Kprobe";"Kretprobe";"Uprobe";"Uretprobe";"Tracepoint"
	// +optional
	Type EBPFProgType `json:"type,omitempty"`

	// xdp defines the desired state of XdpProgram.
	// +unionMember
	// +optional
	XDP *XdpProgramSpec `json:"xdp,omitempty"`

	// tc defines the desired state of TcProgram.
	// +unionMember
	// +optional
	TC *TcProgramSpec `json:"tc,omitempty"`

	// tcx defines the desired state of TcXProgram.
	// +unionMember
	// +optional
	TCX *TcProgramSpec `json:"tcx,omitempty"`

	// fentry defines the desired state of FentryProgram.
	// +unionMember
	// +optional
	Fentry *FentryProgramSpec `json:"fentry,omitempty"`

	// fexit defines the desired state of FexitProgram.
	// +unionMember
	// +optional
	Fexit *FentryProgramSpec `json:"fexit,omitempty"`

	// kprobe defines the desired state of KprobeProgram.
	// +unionMember
	// +optional
	Kprobe *KprobeProgramSpec `json:"kprobe,omitempty"`

	// kretprobe defines the desired state of KretprobeProgram.
	// +unionMember
	// +optional
	Kretprobe *KprobeProgramSpec `json:"kretprobe,omitempty"`

	// uprobe defines the desired state of UprobeProgram.
	// +unionMember
	// +optional
	Uprobe *UprobeProgramSpec `json:"uprobe,omitempty"`

	// uretprobe defines the desired state of UretprobeProgram.
	// +unionMember
	// +optional
	Uretprobe *UprobeProgramSpec `json:"uretprobe,omitempty"`

	// tracepoint defines the desired state of TracepointProgram.
	// +unionMember
	// +optional
	Tracepoint *TracepointProgramSpec `json:"tracepoint,omitempty"`
}

// BpfApplicationSpec defines the desired state of BpfApplication
type BpfApplicationSpec struct {
	// NodeSelector allows the user to specify which nodes to deploy the application
	// bpf programs to.  This field must be specified, to select all nodes
	// use standard metav1.LabelSelector semantics and make it empty.
	NodeSelector metav1.LabelSelector `json:"nodeselector"`

	// GlobalData allows the user to set global variables when the program is loaded
	// with an array of raw bytes. This is a very low level primitive. The caller
	// is responsible for formatting the byte string appropriately considering
	// such things as size, endianness, alignment and packing of data structures.
	// +optional
	GlobalData map[string][]byte `json:"globaldata,omitempty"`

	// Programs is a list of bpf programs supported for a specific application.
	// It's possible that the application can selectively choose which program(s)
	// to run from this list.
	// +kubebuilder:validation:MinItems:=1
	Programs []BpfApplicationProgram `json:"programs,omitempty"`
}

// BpfApplicationStatus defines the observed state of BpfApplication
type BpfApplicationStatus struct {
	// `conditions` represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +genclient
// +genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// BpfApplication is the Schema for the bpfapplications API
type BpfApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BpfApplicationSpec   `json:"spec,omitempty"`
	Status BpfApplicationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BpfApplicationList contains a list of BpfApplication
type BpfApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BpfApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&BpfApplication{},
		&BpfApplicationList{},
	)
}
