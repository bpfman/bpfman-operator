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

// +kubebuilder:validation:Required
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

// ContainerNsSelector identifies a set of containers. It is different from ContainerSelector
// in that "Namespace" was removed. Namespace scoped programs can only attach to the namespace
// they are created in, so namespace at this level doesn't apply.
type ContainerNsSelector struct {
	// Target pods. This field must be specified, to select all pods use
	// standard metav1.LabelSelector semantics and make it empty.
	Pods metav1.LabelSelector `json:"pods"`

	// Name(s) of container(s).  If none are specified, all containers in the
	// pod are selected.
	// +optional
	ContainerNames *[]string `json:"containernames,omitempty"`
}

// BpfAppCommon defines the common attributes for all BpfApp programs
type BpfAppCommon struct {
	// NodeSelector allows the user to specify which nodes to deploy the
	// bpf program to. This field must be specified, to select all nodes
	// use standard metav1.LabelSelector semantics and make it empty.
	NodeSelector metav1.LabelSelector `json:"nodeselector"`

	// GlobalData allows the user to set global variables when the program is loaded
	// with an array of raw bytes. This is a very low level primitive. The caller
	// is responsible for formatting the byte string appropriately considering
	// such things as size, endianness, alignment and packing of data structures.
	// +optional
	GlobalData map[string][]byte `json:"globaldata,omitempty"`

	// Bytecode configures where the bpf program's bytecode should be loaded
	// from.
	ByteCode BytecodeSelector `json:"bytecode"`

	// MapOwnerSelector is used to select the loaded eBPF program this eBPF program
	// will share a map with. The value is a label applied to the BpfProgram to select.
	// The selector must resolve to exactly one instance of a BpfProgram on a given node
	// or the eBPF program will not load.
	// +optional
	MapOwnerSelector *metav1.LabelSelector `json:"mapownerselector"`
}

// BpfAppStatus reflects the status of a BpfApplication or BpfApplicationState object
type BpfAppStatus struct {
	// For a BpfApplication object, Conditions contains the global cluster state
	// for the object. For a BpfApplicationState object, Conditions contains the
	// state of the BpfApplication object on the given node.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// AttachInfoStateCommon reflects the status for one attach point for a given bpf
// application program
type AttachInfoStateCommon struct {
	// ShouldAttach reflects whether the attachment should exist.
	ShouldAttach bool `json:"should_attach"`
	// Unique identifier for the attach point assigned by bpfman agent.
	UUID string `json:"uuid"`
	// An identifier for the attach point assigned by bpfman. This field is
	// empty until the program is successfully attached and bpfman returns the
	// id.
	// ANF-TODO: For the POC, this will be the program ID.
	// +optional
	AttachId *uint32 `json:"attachid"`
	// AttachPointStatus reflects whether the attachment has been reconciled
	// successfully, and if not, why.
	AttachPointStatus AttachPointStatus `json:"attachpointstatus"`
}

type BpfProgramStateCommon struct {
	// BpfFunctionName is the name of the function that is the entry point for the BPF
	// program
	BpfFunctionName string `json:"bpffunctionname"`
	// ProgramAttachStatus records whether the program should be loaded and whether
	// the program is loaded.
	ProgramAttachStatus ProgramAttachStatus `json:"programattachstatus"`
	// ProgramId is the id of the program in the kernel.  Not set until the
	// program is loaded.
	// +optional
	ProgramId *uint32 `json:"program_id"`
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

// -----------------------------------------------------------------------------
// Status Conditions - BPF Programs
// -----------------------------------------------------------------------------

// ProgramConditionType is a condition type to indicate the status of a BPF
// program at the cluster level.
type ProgramConditionType string

const (
	// ProgramNotYetLoaded indicates that the program in question has not
	// yet been loaded on all nodes in the cluster.
	ProgramNotYetLoaded ProgramConditionType = "NotYetLoaded"

	// ProgramReconcileError indicates that an unforeseen situation has
	// occurred in the controller logic, and the controller will retry.
	ProgramReconcileError ProgramConditionType = "ReconcileError"

	// BpfmanProgConfigReconcileSuccess indicates that the BPF program has been
	// successfully reconciled.
	//
	// TODO: we should consider removing "reconciled" type logic from the
	// public API as it's an implementation detail of our use of controller
	// runtime, but not necessarily relevant to human users or integrations.
	//
	// See: https://github.com/bpfman/bpfman/issues/430
	ProgramReconcileSuccess ProgramConditionType = "ReconcileSuccess"

	// ProgramDeleteError indicates that the BPF program was marked for
	// deletion, but deletion was unsuccessful.
	ProgramDeleteError ProgramConditionType = "DeleteError"
)

// Condition is a helper method to promote any given ProgramConditionType to
// a full metav1.Condition in an opinionated fashion.
//
// TODO: this was created in the early days to provide at least SOME status
// information to the user, but the hardcoded messages need to be replaced
// in the future with dynamic and situation-aware messages later.
//
// See: https://github.com/bpfman/bpfman/issues/430
func (b ProgramConditionType) Condition(message string) metav1.Condition {
	cond := metav1.Condition{}

	switch b {
	case ProgramNotYetLoaded:
		if len(message) == 0 {
			message = "Waiting for Program Object to be reconciled to all nodes"
		}

		cond = metav1.Condition{
			Type:    string(ProgramNotYetLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "ProgramsNotYetLoaded",
			Message: message,
		}
	case ProgramReconcileError:
		if len(message) == 0 {
			message = "bpfProgramReconciliation failed"
		}

		cond = metav1.Condition{
			Type:    string(ProgramReconcileError),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileError",
			Message: message,
		}
	case ProgramReconcileSuccess:
		if len(message) == 0 {
			message = "bpfProgramReconciliation Succeeded on all nodes"
		}

		cond = metav1.Condition{
			Type:    string(ProgramReconcileSuccess),
			Status:  metav1.ConditionTrue,
			Reason:  "ReconcileSuccess",
			Message: message,
		}
	case ProgramDeleteError:
		if len(message) == 0 {
			message = "Program Deletion failed"
		}

		cond = metav1.Condition{
			Type:    string(ProgramDeleteError),
			Status:  metav1.ConditionTrue,
			Reason:  "DeleteError",
			Message: message,
		}
	}

	return cond
}

// BpfProgramConditionType is a condition type to indicate the status of a BPF
// program at the individual node level.
type BpfProgramConditionType string

const (
	// BpfProgCondLoaded indicates that the eBPF program was successfully loaded
	// into the kernel on a specific node.
	BpfProgCondLoaded BpfProgramConditionType = "Loaded"

	// BpfProgCondNotLoaded indicates that the eBPF program has not yet been
	// loaded into the kernel on a specific node.
	BpfProgCondNotLoaded BpfProgramConditionType = "NotLoaded"

	// BpfProgCondUnloaded indicates that in the midst of trying to remove the
	// eBPF program from the kernel on the node, that program has not yet been
	// removed.
	BpfProgCondNotUnloaded BpfProgramConditionType = "NotUnLoaded"

	// BpfProgCondNotSelected indicates that the eBPF program is not scheduled to be loaded
	// on a specific node.
	BpfProgCondNotSelected BpfProgramConditionType = "NotSelected"

	// BpfProgCondUnloaded indicates that the eBPF program has been unloaded from
	// the kernel on a specific node.
	BpfProgCondUnloaded BpfProgramConditionType = "Unloaded"

	// BpfProgCondMapOwnerNotFound indicates that the eBPF program sharing a map with another
	// eBPF program and that program does not exist.
	BpfProgCondMapOwnerNotFound BpfProgramConditionType = "MapOwnerNotFound"

	// BpfProgCondMapOwnerNotLoaded indicates that the eBPF program sharing a map with another
	// eBPF program and that program is not loaded.
	BpfProgCondMapOwnerNotLoaded BpfProgramConditionType = "MapOwnerNotLoaded"

	// BpfProgCondBytecodeSelectorError indicates that an error occurred when trying to
	// process the bytecode selector.
	BpfProgCondBytecodeSelectorError BpfProgramConditionType = "BytecodeSelectorError"

	// BpfProgCondNoContainersOnNode indicates that there are no containers on the node
	// that match the container selector.
	BpfProgCondNoContainersOnNode BpfProgramConditionType = "NoContainersOnNode"

	// BpfProgCondAttached indicates that the attachment is attached.  Whether
	// this is good depends on whether it should be attached.
	BpfProgCondAttached BpfProgramConditionType = "Attached"

	// BpfProgCondNotAttached indicates that the attachment is not attached.
	// Whether this is good depends on whether it should be attached.
	BpfProgCondNotAttached BpfProgramConditionType = "NotAttached"

	// BpfProgCondAttachSuccess indicates that all attachments for a given bpf
	// program were successful.
	BpfProgCondAttachSuccess BpfProgramConditionType = "AttachSuccess"

	// BpfProgCondError indicates that one or more attachments for a given bpf
	// program was not successful.
	BpfProgCondAttachError BpfProgramConditionType = "AttachError"

	// None of the above conditions apply
	BpfProgCondNone BpfProgramConditionType = "None"
)

// Condition is a helper method to promote any given BpfProgramConditionType to
// a full metav1.Condition in an opinionated fashion.
func (b BpfProgramConditionType) Condition() metav1.Condition {
	cond := metav1.Condition{}

	switch b {
	case BpfProgCondLoaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfmanLoaded",
			Message: "Successfully loaded bpfProgram",
		}
	case BpfProgCondNotLoaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNotLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfmanNotLoaded",
			Message: "Failed to load bpfProgram",
		}
	case BpfProgCondNotUnloaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNotUnloaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfmanNotUnloaded",
			Message: "Failed to unload bpfProgram",
		}
	case BpfProgCondNotSelected:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNotSelected),
			Status:  metav1.ConditionTrue,
			Reason:  "nodeNotSelected",
			Message: "This node is not selected to run the bpfProgram",
		}
	case BpfProgCondUnloaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondUnloaded),
			Status:  metav1.ConditionTrue,
			Reason:  "bpfmanUnloaded",
			Message: "This BpfProgram object and all it's bpfman programs have been unloaded",
		}
	case BpfProgCondMapOwnerNotFound:
		cond = metav1.Condition{
			Type:    string(BpfProgCondMapOwnerNotFound),
			Status:  metav1.ConditionTrue,
			Reason:  "mapOwnerNotFound",
			Message: "BpfProgram map owner not found",
		}
	case BpfProgCondMapOwnerNotLoaded:
		cond = metav1.Condition{
			Type:    string(BpfProgCondMapOwnerNotLoaded),
			Status:  metav1.ConditionTrue,
			Reason:  "mapOwnerNotLoaded",
			Message: "BpfProgram map owner not loaded",
		}

	case BpfProgCondBytecodeSelectorError:
		cond = metav1.Condition{
			Type:    string(BpfProgCondBytecodeSelectorError),
			Status:  metav1.ConditionTrue,
			Reason:  "bytecodeSelectorError",
			Message: "There was an error processing the provided bytecode selector",
		}

	case BpfProgCondNoContainersOnNode:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNoContainersOnNode),
			Status:  metav1.ConditionTrue,
			Reason:  "noContainersOnNode",
			Message: "There are no containers on the node that match the container selector",
		}

	case BpfProgCondAttached:
		cond = metav1.Condition{
			Type:    string(BpfProgCondAttached),
			Status:  metav1.ConditionTrue,
			Reason:  "attached",
			Message: "Attachment is currently active",
		}

	case BpfProgCondNotAttached:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNotAttached),
			Status:  metav1.ConditionTrue,
			Reason:  "notAttached",
			Message: "Attachment is currently not active",
		}

	case BpfProgCondAttachSuccess:
		cond = metav1.Condition{
			Type:    string(BpfProgCondAttachSuccess),
			Status:  metav1.ConditionTrue,
			Reason:  "attachFailed",
			Message: "All attachments were successful",
		}

	case BpfProgCondAttachError:
		cond = metav1.Condition{
			Type:    string(BpfProgCondAttachError),
			Status:  metav1.ConditionTrue,
			Reason:  "attachFailed",
			Message: "One or more attachments were not successful",
		}

	case BpfProgCondNone:
		cond = metav1.Condition{
			Type:    string(BpfProgCondNone),
			Status:  metav1.ConditionTrue,
			Reason:  "None",
			Message: "None of the conditions apply",
		}
	}

	return cond
}

type AppLoadStatus string

const (
	// The initial load condition
	AppLoadNotLoaded AppLoadStatus = "NotLoaded"
	// All programs for app have been loaded
	AppLoadSuccess AppLoadStatus = "LoadSuccess"
	// One or more programs for app has not been loaded
	AppLoadError AppLoadStatus = "LoadError"
	// All programs for app have been unloaded
	AppUnLoadSuccess AppLoadStatus = "UnloadSuccess"
	// One or more programs for app has not been unloaded
	AppUnloadError AppLoadStatus = "UnloadError"
	// The app is not selected to run on the node
	NotSelected AppLoadStatus = "NotSelected"
)

type ProgramAttachStatus string

const (
	// The initial program attach state
	ProgAttachInit ProgramAttachStatus = "AttachInit"
	// All attachments for program are in the correct state
	ProgAttachSuccess ProgramAttachStatus = "AttachSuccess"
	// One or more attachments for program are not in the correct state
	ProgAttachError ProgramAttachStatus = "AttachError"
	// ANF-TODO: This will probably become a list attach point error with the
	// load/attach split
	BpfmanListProgramError ProgramAttachStatus = "BpfmanListProgramError"
	// There was an error processing the Map Owner
	MapOwnerError ProgramAttachStatus = "MapOwnerError"
	// There was an error updating the attach info
	UpdateAttachInfoError ProgramAttachStatus = "UpdateAttachInfoError"
)

type AttachPointStatus string

const (
	// ANF-TODO: This error will move to ApploadStatus with load/attch split
	ApBytecodeSelectorError AttachPointStatus = "BytecodeSelectorError"
	// Attach point is attached
	ApAttachAttached AttachPointStatus = "Attached"
	// Attach point is not attached
	ApAttachNotAttached AttachPointStatus = "NotAttached"
	// An attach was attempted, but there was an error
	ApAttachError AttachPointStatus = "AttachError"
	// A detach was attempted, but there was an error
	ApDetachError AttachPointStatus = "DetachError"
)
