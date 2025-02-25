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

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/google/uuid"
)

// TracepointProgramReconciler contains the info required to reconcile a TracepointProgram
type TracepointProgramReconciler struct {
	ReconcilerCommon
	ProgramReconcilerCommon
	currentAttachPoint *bpfmaniov1alpha1.TracepointAttachInfoState
}

func (r *TracepointProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *TracepointProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracepoint
}

func (r *TracepointProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_TRACEPOINT
}

func (r *TracepointProgramReconciler) getProgName() string {
	return r.currentProgram.BpfFunctionName
}

func (r *TracepointProgramReconciler) shouldAttach() bool {
	return r.currentAttachPoint.ShouldAttach
}

func (r *TracepointProgramReconciler) isAttached() bool {
	// ANF-TODO: Make this check more robust.  Some ideas include: get the link to
	// confirm it exists.  Confirm, that it matches what we expect. If not,
	// check if there is another link that contains the UUID for this link.
	return r.currentAttachPoint.AttachId != nil
}

func (r *TracepointProgramReconciler) getUUID() string {
	return r.currentAttachPoint.UUID
}

func (r *TracepointProgramReconciler) getAttachId() *uint32 {
	return r.currentAttachPoint.AttachId
}

func (r *TracepointProgramReconciler) setAttachId(id *uint32) {
	r.currentAttachPoint.AttachId = id
}

func (r *TracepointProgramReconciler) setProgramAttachStatus(status bpfmaniov1alpha1.ProgramAttachStatus) {
	r.currentProgramState.ProgramAttachStatus = status
}

func (r *TracepointProgramReconciler) getProgramAttachStatus() bpfmaniov1alpha1.ProgramAttachStatus {
	return r.currentProgramState.ProgramAttachStatus
}

func (r *TracepointProgramReconciler) setCurrentAttachPointStatus(status bpfmaniov1alpha1.AttachPointStatus) {
	r.currentAttachPoint.AttachPointStatus = status
}

func (r *TracepointProgramReconciler) getCurrentAttachPointStatus() bpfmaniov1alpha1.AttachPointStatus {
	return r.currentAttachPoint.AttachPointStatus
}

func (r *TracepointProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {
	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TracepointAttachInfo{
				TracepointAttachInfo: &gobpfman.TracepointAttachInfo{
					Tracepoint: r.currentAttachPoint.Name,
					Metadata:   map[string]string{internal.UuidMetadataKey: string(r.currentAttachPoint.UUID)},
				},
			},
		},
	}
}

// updateAttachInfo processes the *ProgramInfo and updates the list of attach
// points contained in *AttachInfoState.
func (r *TracepointProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Tracepoint updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all attach points in the node CRD to false.  We'll
	// update this in the next step for all attach points that are still
	// present.
	for i := range r.currentProgramState.Tracepoint.AttachPoints {
		r.currentProgramState.Tracepoint.AttachPoints[i].ShouldAttach = false
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

	for _, attachInfo := range r.currentProgram.Tracepoint.AttachPoints {
		expectedAttachPoints, error := r.getExpectedAttachPoints(attachInfo)
		if error != nil {
			return fmt.Errorf("failed to get node attach points: %v", error)
		}
		for _, attachPoint := range expectedAttachPoints {
			index := r.findAttachPoint(attachPoint)
			if index != nil {
				// Attach point already exists, so set ShouldAttach to true.
				r.currentProgramState.Tracepoint.AttachPoints[*index].AttachInfoStateCommon.ShouldAttach = true
			} else {
				// Attach point doesn't exist, so add it.
				r.Logger.Info("Attach point doesn't exist.  Adding it.")
				r.currentProgramState.Tracepoint.AttachPoints = append(r.currentProgramState.Tracepoint.AttachPoints, attachPoint)
			}
		}
	}

	// If any existing attach point is no longer on a list of expected attach
	// points, ShouldAttach will remain set to false and it will get detached in
	// a following step.

	return nil
}

// ANF-TODO: Confirm what constitutes a match between two attach points.
func (r *TracepointProgramReconciler) findAttachPoint(attachInfoState bpfmaniov1alpha1.TracepointAttachInfoState) *int {
	for i, a := range r.currentProgramState.Tracepoint.AttachPoints {
		// attachInfoState is the same as a if the the following fields are the
		// same: IfName, ContainerPid, Priority, and Direction.
		if a.Name == attachInfoState.Name {
			return &i
		}
	}
	return nil
}

// processAttachInfo calls reconcileBpfAttachment() for each attach point. It
// then updates the ProgramAttachStatus based on the updated status of each
// attach point.
func (r *TracepointProgramReconciler) processAttachInfo(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.BpfFunctionName)

	// The following map is used to keep track of attach points that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// attach points.
	attachPointsToRemove := make(map[int]bool)

	var lastReconcileAttachmentError error = nil
	for i := range r.currentProgramState.Tracepoint.AttachPoints {
		r.currentAttachPoint = &r.currentProgramState.Tracepoint.AttachPoints[i]
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
		r.currentProgramState.Tracepoint.AttachPoints = r.removeAttachPoints(r.currentProgramState.Tracepoint.AttachPoints, attachPointsToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileAttachmentError
}

func (r *TracepointProgramReconciler) updateProgramAttachStatus() {
	for _, attachPoint := range r.currentProgramState.Tracepoint.AttachPoints {
		if !isAttachSuccess(attachPoint.ShouldAttach, attachPoint.AttachPointStatus) {
			r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeAttachPoints removes attach points from a slice of attach points based on the keys in the map.
func (r *TracepointProgramReconciler) removeAttachPoints(attachPoints []bpfmaniov1alpha1.TracepointAttachInfoState, attachPointsToRemove map[int]bool) []bpfmaniov1alpha1.TracepointAttachInfoState {
	var remainingAttachPoints []bpfmaniov1alpha1.TracepointAttachInfoState
	for i, a := range attachPoints {
		if _, ok := attachPointsToRemove[i]; !ok {
			remainingAttachPoints = append(remainingAttachPoints, a)
		}
	}
	return remainingAttachPoints
}

// getExpectedAttachPoints expands *AttachInfo into a list of specific attach
// points.
func (r *TracepointProgramReconciler) getExpectedAttachPoints(attachInfo bpfmaniov1alpha1.TracepointAttachInfo,
) ([]bpfmaniov1alpha1.TracepointAttachInfoState, error) {
	nodeAttachPoints := []bpfmaniov1alpha1.TracepointAttachInfoState{}

	attachPoint := bpfmaniov1alpha1.TracepointAttachInfoState{
		AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
			ShouldAttach:      true,
			UUID:              uuid.New().String(),
			AttachId:          nil,
			AttachPointStatus: bpfmaniov1alpha1.ApAttachNotAttached,
		},
		Name: attachInfo.Name,
	}
	nodeAttachPoints = append(nodeAttachPoints, attachPoint)

	return nodeAttachPoints, nil
}

func (r *TracepointProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.BpfFunctionName,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
