package status

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProgramConditionType is a condition type to indicate the status of a BPF
// program at the cluster level.
type ProgramConditionType string

const (
	// ProgramNotYetLoaded indicates that the program in question has not
	// yet been loaded on all nodes in the cluster.
	ProgramNotYetLoaded ProgramConditionType = "NotYetLoaded"

	// ProgramReconcileError indicates that an unforseen situation has
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

	// BpfProgCondByteCodeError indicates that an error occured when trying to
	// process the bytecode selector.
	BpfProgCondBytecodeSelectorError BpfProgramConditionType = "BytecodeSelectorError"

	// BpfProgCondNoContainersOnNode indicates that there are no containers on the node
	// that match the container selector.
	BpfProgCondNoContainersOnNode BpfProgramConditionType = "NoContainersOnNode"

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
