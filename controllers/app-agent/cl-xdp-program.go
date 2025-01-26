/*
Copyright 2025.

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

//lint:file-ignore U1000 Linter claims functions unused, but are required for generic

package appagent

import (
	"context"
	"fmt"
	"reflect"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/app-agent/internal"
	internal "github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/google/uuid"

	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=xdpprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type XdpProgramReconciler struct {
	ReconcilerCommon
	ProgramReconcilerCommon
	currentAttachPoint *bpfmaniov1alpha1.XdpAttachInfoState
}

func (r *XdpProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *XdpProgramReconciler) getProgType() internal.ProgramType {
	return internal.Xdp
}

func (r *XdpProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *XdpProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.appCommon.GlobalData
}

func (r *XdpProgramReconciler) shouldAttach() bool {
	return r.currentAttachPoint.ShouldAttach
}

func (r *XdpProgramReconciler) getUUID() string {
	return r.currentAttachPoint.UUID
}

func (r *XdpProgramReconciler) getAttachId() *uint32 {
	return r.currentAttachPoint.AttachId
}

func (r *XdpProgramReconciler) setAttachId(id *uint32) {
	r.currentAttachPoint.AttachId = id
}

func (r *XdpProgramReconciler) setAttachStatus(status bpfmaniov1alpha1.BpfProgramConditionType) bool {
	return updateSimpleStatus(&r.currentAttachPoint.AttachStatus, status)
}

func (r *XdpProgramReconciler) getAttachStatus() bpfmaniov1alpha1.BpfProgramConditionType {
	return r.currentAttachPoint.AttachStatus
}

// Must match with bpfman internal types
func xdpProceedOnToInt(proceedOn []bpfmaniov1alpha1.XdpProceedOnValue) []int32 {
	var out []int32

	for _, p := range proceedOn {
		switch p {
		case "aborted":
			out = append(out, 0)
		case "drop":
			out = append(out, 1)
		case "pass":
			out = append(out, 2)
		case "tx":
			out = append(out, 3)
		case "redirect":
			out = append(out, 4)
		case "dispatcher_return":
			out = append(out, 31)
		}
	}

	return out
}

func (r *XdpProgramReconciler) getLoadRequest(mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {

	r.Logger.Info("Getting load request", "bpfFunctionName", r.currentProgram.XDP.BpfFunctionName, "reqAttachInfo", r.currentAttachPoint, "mapOwnerId",
		mapOwnerId, "ByteCode", r.appCommon.ByteCode)

	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.appCommon.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	attachInfo := &gobpfman.XDPAttachInfo{
		Priority:  r.currentAttachPoint.Priority,
		Iface:     r.currentAttachPoint.IfName,
		ProceedOn: xdpProceedOnToInt(r.currentAttachPoint.ProceedOn),
	}

	if r.currentAttachPoint.ContainerPid != nil {
		netns := fmt.Sprintf("/host/proc/%d/ns/net", *r.currentAttachPoint.ContainerPid)
		attachInfo.Netns = &netns
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentProgram.XDP.BpfFunctionName,
		ProgramType: uint32(internal.Xdp),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_XdpAttachInfo{
				XdpAttachInfo: attachInfo,
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(r.currentAttachPoint.UUID), internal.ProgramNameKey: "BpfApplication"},
		GlobalData: r.appCommon.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

// updateAttachInfo processes the *ProgramInfo and updates the list of attach
// points contained in *AttachInfoState.
func (r *XdpProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("XDP updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all attach points in the node CRD to false.  We'll
	// update this in the next step for all attach points that are still
	// present.
	for i := range r.currentProgramState.XDP.AttachPoints {
		r.currentProgramState.XDP.AttachPoints[i].ShouldAttach = false
	}

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		//
		// ANF-TODO: When we have load/attach split, we shouldn't even need to
		// set ShouldAttach to false above, because unloading the program should
		// remove all attachments and updateAttachInfo won't be called.  We
		// probably should delete AttachPoints when unloading the program.
		return nil
	}

	for _, attachInfo := range r.currentProgram.XDP.AttachPoints {
		expectedAttachPoints, error := r.getExpectedAttachPoints(ctx, attachInfo)
		if error != nil {
			return fmt.Errorf("failed to get node attach points: %v", error)
		}
		for _, attachPoint := range expectedAttachPoints {
			index := r.findAttachPoint(attachPoint)
			if index != nil {
				// Attach point already exists, so set ShouldAttach to true.
				r.currentProgramState.XDP.AttachPoints[*index].AttachInfoCommon.ShouldAttach = true
			} else {
				// Attach point doesn't exist, so add it.
				r.currentProgramState.XDP.AttachPoints = append(r.currentProgramState.XDP.AttachPoints, attachPoint)
			}
		}
	}

	// If any existing attach point is no longer on a list of expected attach
	// points, ShouldAttach will remain set to false and it will get detached in
	// a following step.

	return nil
}

// ANF-TODO: Confirm what constitutes a match between two attach points.  E.g.,
// what if everything the same, but the priority and/or proceed_on values are
// different?
func (r *XdpProgramReconciler) findAttachPoint(attachInfoState bpfmaniov1alpha1.XdpAttachInfoState) *int {
	for i, a := range r.currentProgramState.XDP.AttachPoints {
		// attachInfoState is the same as a if the the following fields are the
		// same: IfName, ContainerPid, Priority, and ProceedOn.
		if a.IfName == attachInfoState.IfName && reflect.DeepEqual(a.ContainerPid, attachInfoState.ContainerPid) &&
			a.Priority == attachInfoState.Priority && reflect.DeepEqual(a.ProceedOn, attachInfoState.ProceedOn) {
			return &i
		}
	}
	return nil
}

// processAttachInfo processes the attach points in *AttachInfoState. Based on
// the current state, it calls bpfman to attach or detach, or does nothing if
// the state is correct. It returns a boolean indicating if any changes were
// made.
//
// ANF-TODO: Generalize this function and move it into common.
func (r *XdpProgramReconciler) processAttachInfo(ctx context.Context, mapOwnerStatus *MapOwnerParamStatus) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.XDP.BpfFunctionName,
		"mapOwnerStatus", mapOwnerStatus)

	// Get existing ebpf state from bpfman.
	loadedBpfPrograms, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, internal.Xdp)
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfman programs")
		updateSimpleStatus(&r.currentProgramState.ProgramAttachStatus, bpfmaniov1alpha1.BpfProgCondAttachError)
		return fmt.Errorf("failed to list loaded bpfman programs: %v", err)
	}

	// The following map is used to keep track of attach points that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// attach points.
	attachPointsToRemove := make(map[int]bool)

	var lastReconcileAttachmentError error = nil
	for i := range r.currentProgramState.XDP.AttachPoints {
		r.currentAttachPoint = &r.currentProgramState.XDP.AttachPoints[i]
		remove, err := r.reconcileBpfAttachment(ctx, r, loadedBpfPrograms, mapOwnerStatus)
		if err != nil {
			r.Logger.Error(err, "failed to reconcile bpf attachment", "index", i)
			// All errors are logged, but the last error is saved to return and
			// we continue to process the rest of the attach points so errors
			// don't block valid attach points.
			lastReconcileAttachmentError = err
		}

		if remove {
			r.Logger.Info("Marking attach point for removal", "index", i)
			attachPointsToRemove[i] = true
		}
	}

	if len(attachPointsToRemove) > 0 {
		r.Logger.Info("Removing attach points", "attachPointsToRemove", attachPointsToRemove)
		r.currentProgramState.XDP.AttachPoints = r.removeAttachPoints(r.currentProgramState.XDP.AttachPoints, attachPointsToRemove)
	}

	return lastReconcileAttachmentError
}

// removeAttachPoints removes attach points from a slice of attach points based on the keys in the map.
func (r *XdpProgramReconciler) removeAttachPoints(attachPoints []bpfmaniov1alpha1.XdpAttachInfoState, attachPointsToRemove map[int]bool) []bpfmaniov1alpha1.XdpAttachInfoState {
	var newAttachPoints []bpfmaniov1alpha1.XdpAttachInfoState
	for i, a := range attachPoints {
		if _, ok := attachPointsToRemove[i]; !ok {
			newAttachPoints = append(newAttachPoints, a)
		}
	}
	return newAttachPoints
}

// getInterfaces expands XdpAttachInfo into a list of specific attach points.  It works pretty much like the old getExpectedBpfPrograms.
func (r *XdpProgramReconciler) getExpectedAttachPoints(ctx context.Context, attachInfo bpfmaniov1alpha1.XdpAttachInfo,
) ([]bpfmaniov1alpha1.XdpAttachInfoState, error) {
	interfaces, err := getInterfaces(&attachInfo.InterfaceSelector, r.ourNode)
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces for XdpProgram: %v", err)
	}

	nodeAttachPoints := []bpfmaniov1alpha1.XdpAttachInfoState{}

	if attachInfo.Containers != nil {
		// There is a container selector, so see if there are any matching
		// containers on this node.
		containerInfo, err := r.Containers.GetContainers(
			ctx,
			attachInfo.Containers.Namespace,
			attachInfo.Containers.Pods,
			attachInfo.Containers.ContainerNames,
			r.Logger,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get container pids: %v", err)
		}

		if containerInfo != nil && len(*containerInfo) != 0 {
			// Containers were found, so create attach points.
			for i := range *containerInfo {
				container := (*containerInfo)[i]
				for _, iface := range interfaces {
					containerPid := uint32(container.pid)
					attachPoint := bpfmaniov1alpha1.XdpAttachInfoState{
						AttachInfoCommon: bpfmaniov1alpha1.AttachInfoCommon{
							ShouldAttach: true,
							UUID:         uuid.New().String(),
							AttachId:     nil,
							AttachStatus: bpfmaniov1alpha1.BpfProgCondNotAttached,
						},
						IfName:       iface,
						ContainerPid: &containerPid,
						Priority:     attachInfo.Priority,
						ProceedOn:    attachInfo.ProceedOn,
					}
					nodeAttachPoints = append(nodeAttachPoints, attachPoint)
				}
			}
		}
	} else {
		for _, iface := range interfaces {
			attachPoint := bpfmaniov1alpha1.XdpAttachInfoState{
				AttachInfoCommon: bpfmaniov1alpha1.AttachInfoCommon{
					ShouldAttach: true,
					UUID:         uuid.New().String(),
					AttachId:     nil,
					AttachStatus: bpfmaniov1alpha1.BpfProgCondNotAttached,
				},
				IfName:       iface,
				ContainerPid: nil,
				Priority:     attachInfo.Priority,
				ProceedOn:    attachInfo.ProceedOn,
			}
			nodeAttachPoints = append(nodeAttachPoints, attachPoint)
		}
	}

	return nodeAttachPoints, nil
}
