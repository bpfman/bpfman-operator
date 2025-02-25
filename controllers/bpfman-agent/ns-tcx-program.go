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

package bpfmanagent

import (
	"context"
	"fmt"
	"reflect"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/google/uuid"
)

// TcxNsProgramReconciler contains the info required to reconcile a TcxNsProgram
type TcxNsProgramReconciler struct {
	ReconcilerCommon
	ProgramNsReconcilerCommon
	currentAttachPoint *bpfmaniov1alpha1.TcxNsAttachInfoState
}

func (r *TcxNsProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *TcxNsProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tc
}

func (r *TcxNsProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_TCX
}

func (r *TcxNsProgramReconciler) getProgName() string {
	return r.currentProgram.BpfFunctionName
}

func (r *TcxNsProgramReconciler) shouldAttach() bool {
	return r.currentAttachPoint.ShouldAttach
}

func (r *TcxNsProgramReconciler) isAttached() bool {
	// ANF-TODO: Make this check more robust.  Some ideas include: get the link to
	// confirm it exists.  Confirm, that it matches what we expect. If not,
	// check if there is another link that contains the UUID for this link.
	return r.currentAttachPoint.AttachId != nil
}

func (r *TcxNsProgramReconciler) getUUID() string {
	return r.currentAttachPoint.UUID
}

func (r *TcxNsProgramReconciler) getAttachId() *uint32 {
	return r.currentAttachPoint.AttachId
}

func (r *TcxNsProgramReconciler) setAttachId(id *uint32) {
	r.currentAttachPoint.AttachId = id
}

func (r *TcxNsProgramReconciler) setProgramAttachStatus(status bpfmaniov1alpha1.ProgramAttachStatus) {
	r.currentProgramState.ProgramAttachStatus = status
}

func (r *TcxNsProgramReconciler) getProgramAttachStatus() bpfmaniov1alpha1.ProgramAttachStatus {
	return r.currentProgramState.ProgramAttachStatus
}

func (r *TcxNsProgramReconciler) setCurrentAttachPointStatus(status bpfmaniov1alpha1.AttachPointStatus) {
	r.currentAttachPoint.AttachPointStatus = status
}

func (r *TcxNsProgramReconciler) getNamespace() string {
	return r.namespace
}

func (r *TcxNsProgramReconciler) getCurrentAttachPointStatus() bpfmaniov1alpha1.AttachPointStatus {
	return r.currentAttachPoint.AttachPointStatus
}

func (r *TcxNsProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {

	attachInfo := &gobpfman.TCXAttachInfo{
		Priority:  r.currentAttachPoint.Priority,
		Iface:     r.currentAttachPoint.IfName,
		Direction: r.currentAttachPoint.Direction,
		Metadata:  map[string]string{internal.UuidMetadataKey: string(r.currentAttachPoint.UUID)},
	}

	netns := fmt.Sprintf("/host/proc/%d/ns/net", r.currentAttachPoint.ContainerPid)
	attachInfo.Netns = &netns

	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcxAttachInfo{
				TcxAttachInfo: attachInfo,
			},
		},
	}
}

// updateAttachInfo processes the *ProgramInfo and updates the list of attach
// points contained in *AttachInfoState.
func (r *TcxNsProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("TCX updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all attach points in the node CRD to false.  We'll
	// update this in the next step for all attach points that are still
	// present.
	for i := range r.currentProgramState.TCX.AttachPoints {
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
				r.currentProgramState.TCX.AttachPoints[*index].AttachInfoStateCommon.ShouldAttach = true
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

// ANF-TODO: Confirm what constitutes a match between two attach points.
func (r *TcxNsProgramReconciler) findAttachPoint(attachInfoState bpfmaniov1alpha1.TcxNsAttachInfoState) *int {
	for i, a := range r.currentProgramState.TCX.AttachPoints {
		// attachInfoState is the same as a if the the following fields are the
		// same: IfName, ContainerPid, Priority, and Direction.
		if a.IfName == attachInfoState.IfName && a.Priority == attachInfoState.Priority &&
			a.Direction == attachInfoState.Direction &&
			reflect.DeepEqual(a.ContainerPid, attachInfoState.ContainerPid) {
			return &i
		}
	}
	return nil
}

// processAttachInfo calls reconcileBpfAttachment() for each attach point. It
// then updates the ProgramAttachStatus based on the updated status of each
// attach point.
func (r *TcxNsProgramReconciler) processAttachInfo(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.BpfFunctionName)

	// The following map is used to keep track of attach points that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// attach points.
	attachPointsToRemove := make(map[int]bool)

	var lastReconcileAttachmentError error = nil
	for i := range r.currentProgramState.TCX.AttachPoints {
		r.currentAttachPoint = &r.currentProgramState.TCX.AttachPoints[i]
		remove, err := r.reconcileBpfAttachment(ctx, r)
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

	r.updateProgramAttachStatus()

	return lastReconcileAttachmentError
}

func (r *TcxNsProgramReconciler) updateProgramAttachStatus() {
	for _, attachPoint := range r.currentProgramState.TCX.AttachPoints {
		if !isAttachSuccess(attachPoint.ShouldAttach, attachPoint.AttachPointStatus) {
			r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeAttachPoints removes attach points from a slice of attach points based on the keys in the map.
func (r *TcxNsProgramReconciler) removeAttachPoints(attachPoints []bpfmaniov1alpha1.TcxNsAttachInfoState, attachPointsToRemove map[int]bool) []bpfmaniov1alpha1.TcxNsAttachInfoState {
	var remainingAttachPoints []bpfmaniov1alpha1.TcxNsAttachInfoState
	for i, a := range attachPoints {
		if _, ok := attachPointsToRemove[i]; !ok {
			remainingAttachPoints = append(remainingAttachPoints, a)
		}
	}
	return remainingAttachPoints
}

// getExpectedAttachPoints expands *AttachInfo into a list of specific attach
// points.
func (r *TcxNsProgramReconciler) getExpectedAttachPoints(ctx context.Context, attachInfo bpfmaniov1alpha1.TcxNsAttachInfo,
) ([]bpfmaniov1alpha1.TcxNsAttachInfoState, error) {
	interfaces, err := getInterfaces(&attachInfo.InterfaceSelector, r.ourNode)
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces for TcxNsProgram: %v", err)
	}

	nodeAttachPoints := []bpfmaniov1alpha1.TcxNsAttachInfoState{}

	containerInfo, err := r.Containers.GetContainers(
		ctx,
		r.getNamespace(),
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
				containerPid := container.pid
				attachPoint := bpfmaniov1alpha1.TcxNsAttachInfoState{
					AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
						ShouldAttach:      true,
						UUID:              uuid.New().String(),
						AttachId:          nil,
						AttachPointStatus: bpfmaniov1alpha1.ApAttachNotAttached,
					},
					IfName:       iface,
					ContainerPid: containerPid,
					Priority:     attachInfo.Priority,
					Direction:    attachInfo.Direction,
				}
				nodeAttachPoints = append(nodeAttachPoints, attachPoint)
			}
		}
	}

	return nodeAttachPoints, nil
}

func (r *TcxNsProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.BpfFunctionName,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
