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

//+kubebuilder:rbac:groups=bpfman.io,resources=tcxprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type TcxProgramReconciler struct {
	ReconcilerCommon
	ProgramReconcilerCommon
	currentAttachPoint *bpfmaniov1alpha1.TcxAttachInfoState
}

func (r *TcxProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *TcxProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tc
}

func (r *TcxProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *TcxProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.appCommon.GlobalData
}

func (r *TcxProgramReconciler) shouldAttach() bool {
	return r.currentAttachPoint.ShouldAttach
}

func (r *TcxProgramReconciler) getUUID() string {
	return r.currentAttachPoint.UUID
}

func (r *TcxProgramReconciler) getAttachId() *uint32 {
	return r.currentAttachPoint.AttachId
}

func (r *TcxProgramReconciler) setAttachId(id *uint32) {
	r.currentAttachPoint.AttachId = id
}

func (r *TcxProgramReconciler) setAttachStatus(status bpfmaniov1alpha1.BpfProgramConditionType) bool {
	return updateSimpleStatus(&r.currentAttachPoint.AttachStatus, status)
}

func (r *TcxProgramReconciler) getAttachStatus() bpfmaniov1alpha1.BpfProgramConditionType {
	return r.currentAttachPoint.AttachStatus
}

func (r *TcxProgramReconciler) getLoadRequest(mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {

	r.Logger.Info("Getting load request", "bpfFunctionName", r.currentProgram.TCX.BpfFunctionName, "reqAttachInfo", r.currentAttachPoint, "mapOwnerId",
		mapOwnerId, "ByteCode", r.appCommon.ByteCode)

	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.appCommon.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	attachInfo := &gobpfman.TCXAttachInfo{
		Priority:  r.currentAttachPoint.Priority,
		Iface:     r.currentAttachPoint.IfName,
		Direction: r.currentAttachPoint.Direction,
	}

	if r.currentAttachPoint.ContainerPid != nil {
		netns := fmt.Sprintf("/host/proc/%d/ns/net", *r.currentAttachPoint.ContainerPid)
		attachInfo.Netns = &netns
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentProgram.TCX.BpfFunctionName,
		ProgramType: uint32(internal.Tc),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcxAttachInfo{
				TcxAttachInfo: attachInfo,
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
func (r *TcxProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("TCX updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all attach points in the node CRD to false.  We'll
	// update this in the next step for all attach points that are still
	// present.
	for i := range r.currentProgramState.TCX.AttachPoints {
		r.Logger.Info("Setting ShouldAttach to false", "index", i)
		r.currentProgramState.TCX.AttachPoints[i].ShouldAttach = false
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

	for _, attachInfo := range r.currentProgram.TCX.AttachPoints {
		expectedAttachPoints, error := r.getExpectedAttachPoints(ctx, attachInfo)
		if error != nil {
			return fmt.Errorf("failed to get node attach points: %v", error)
		}
		for _, attachPoint := range expectedAttachPoints {
			index := r.findAttachPoint(attachPoint)
			if index != nil {
				// Attach point already exists, so set ShouldAttach to true.
				r.Logger.Info("Setting ShouldAttach to true", "index", *index)
				r.currentProgramState.TCX.AttachPoints[*index].AttachInfoCommon.ShouldAttach = true
			} else {
				// Attach point doesn't exist, so add it.
				r.Logger.Info("Attach point doesn't exist.  Adding it.")
				r.currentProgramState.TCX.AttachPoints = append(r.currentProgramState.TCX.AttachPoints, attachPoint)
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
func (r *TcxProgramReconciler) findAttachPoint(attachInfoState bpfmaniov1alpha1.TcxAttachInfoState) *int {
	for i, a := range r.currentProgramState.TCX.AttachPoints {
		// attachInfoState is the same as a if the the following fields are the
		// same: IfName, ContainerPid, Priority, and Direction.
		if a.IfName == attachInfoState.IfName && reflect.DeepEqual(a.ContainerPid, attachInfoState.ContainerPid) &&
			a.Priority == attachInfoState.Priority && a.Direction == attachInfoState.Direction {
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
func (r *TcxProgramReconciler) processAttachInfo(ctx context.Context, mapOwnerStatus *MapOwnerParamStatus) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.TCX.BpfFunctionName,
		"mapOwnerStatus", mapOwnerStatus)

	// Get existing ebpf state from bpfman.
	loadedBpfPrograms, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, internal.Tc)
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
	for i := range r.currentProgramState.TCX.AttachPoints {
		r.currentAttachPoint = &r.currentProgramState.TCX.AttachPoints[i]
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
		r.currentProgramState.TCX.AttachPoints = r.removeAttachPoints(r.currentProgramState.TCX.AttachPoints, attachPointsToRemove)
	}

	return lastReconcileAttachmentError
}

// removeAttachPoints removes attach points from a slice of attach points based on the keys in the map.
func (r *TcxProgramReconciler) removeAttachPoints(attachPoints []bpfmaniov1alpha1.TcxAttachInfoState, attachPointsToRemove map[int]bool) []bpfmaniov1alpha1.TcxAttachInfoState {
	var remainingAttachPoints []bpfmaniov1alpha1.TcxAttachInfoState
	for i, a := range attachPoints {
		if _, ok := attachPointsToRemove[i]; !ok {
			remainingAttachPoints = append(remainingAttachPoints, a)
		}
	}
	return remainingAttachPoints
}

// getInterfaces expands TcxAttachInfo into a list of specific attach points.  It works pretty much like the old getExpectedBpfPrograms.
func (r *TcxProgramReconciler) getExpectedAttachPoints(ctx context.Context, attachInfo bpfmaniov1alpha1.TcxAttachInfo,
) ([]bpfmaniov1alpha1.TcxAttachInfoState, error) {
	interfaces, err := getInterfaces(&attachInfo.InterfaceSelector, r.ourNode)
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces for TcxProgram: %v", err)
	}

	nodeAttachPoints := []bpfmaniov1alpha1.TcxAttachInfoState{}

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
					attachPoint := bpfmaniov1alpha1.TcxAttachInfoState{
						AttachInfoCommon: bpfmaniov1alpha1.AttachInfoCommon{
							ShouldAttach: true,
							UUID:         uuid.New().String(),
							AttachId:     nil,
							AttachStatus: bpfmaniov1alpha1.BpfProgCondNotAttached,
						},
						IfName:       iface,
						ContainerPid: &containerPid,
						Priority:     attachInfo.Priority,
						Direction:    attachInfo.Direction,
					}
					nodeAttachPoints = append(nodeAttachPoints, attachPoint)
				}
			}
		}
	} else {
		for _, iface := range interfaces {
			attachPoint := bpfmaniov1alpha1.TcxAttachInfoState{
				AttachInfoCommon: bpfmaniov1alpha1.AttachInfoCommon{
					ShouldAttach: true,
					UUID:         uuid.New().String(),
					AttachId:     nil,
					AttachStatus: bpfmaniov1alpha1.BpfProgCondNotAttached,
				},
				IfName:       iface,
				ContainerPid: nil,
				Priority:     attachInfo.Priority,
				Direction:    attachInfo.Direction,
			}
			nodeAttachPoints = append(nodeAttachPoints, attachPoint)
		}
	}

	return nodeAttachPoints, nil
}
