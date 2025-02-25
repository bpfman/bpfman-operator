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

// UprobeProgramReconciler contains the info required to reconcile a UprobeProgram
type UprobeProgramReconciler struct {
	ReconcilerCommon
	ProgramReconcilerCommon
	currentAttachPoint *bpfmaniov1alpha1.UprobeAttachInfoState
}

func (r *UprobeProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *UprobeProgramReconciler) getProgType() internal.ProgramType {
	return internal.Kprobe
}

func (r *UprobeProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_UPROBE
}

func (r *UprobeProgramReconciler) getProgName() string {
	return r.currentProgram.BpfFunctionName
}

func (r *UprobeProgramReconciler) shouldAttach() bool {
	return r.currentAttachPoint.ShouldAttach
}

func (r *UprobeProgramReconciler) isAttached() bool {
	// ANF-TODO: Make this check more robust.  Some ideas include: get the link to
	// confirm it exists.  Confirm, that it matches what we expect. If not,
	// check if there is another link that contains the UUID for this link.
	return r.currentAttachPoint.AttachId != nil
}

func (r *UprobeProgramReconciler) getUUID() string {
	return r.currentAttachPoint.UUID
}

func (r *UprobeProgramReconciler) getAttachId() *uint32 {
	return r.currentAttachPoint.AttachId
}

func (r *UprobeProgramReconciler) setAttachId(id *uint32) {
	r.currentAttachPoint.AttachId = id
}

func (r *UprobeProgramReconciler) setProgramAttachStatus(status bpfmaniov1alpha1.ProgramAttachStatus) {
	r.currentProgramState.ProgramAttachStatus = status
}

func (r *UprobeProgramReconciler) getProgramAttachStatus() bpfmaniov1alpha1.ProgramAttachStatus {
	return r.currentProgramState.ProgramAttachStatus
}

func (r *UprobeProgramReconciler) setCurrentAttachPointStatus(status bpfmaniov1alpha1.AttachPointStatus) {
	r.currentAttachPoint.AttachPointStatus = status
}

func (r *UprobeProgramReconciler) getCurrentAttachPointStatus() bpfmaniov1alpha1.AttachPointStatus {
	return r.currentAttachPoint.AttachPointStatus
}

func (r *UprobeProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {

	attachInfo := &gobpfman.UprobeAttachInfo{
		FnName:   &r.currentAttachPoint.FunctionName,
		Offset:   r.currentAttachPoint.Offset,
		Target:   r.currentAttachPoint.Target,
		Retprobe: r.currentAttachPoint.Retprobe,
		Pid:      r.currentAttachPoint.Pid,
		Metadata: map[string]string{internal.UuidMetadataKey: string(r.currentAttachPoint.UUID)},
	}

	if r.currentAttachPoint.ContainerPid != nil {
		containerPid := int32(*r.currentAttachPoint.ContainerPid)
		attachInfo.ContainerPid = &containerPid
	}

	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: attachInfo,
			},
		},
	}
}

// updateAttachInfo processes the *ProgramInfo and updates the list of attach
// points contained in *AttachInfoState.
func (r *UprobeProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Uprobe updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all attach points in the node CRD to false.  We'll
	// update this in the next step for all attach points that are still
	// present.
	for i := range r.currentProgramState.Uprobe.AttachPoints {
		r.currentProgramState.Uprobe.AttachPoints[i].ShouldAttach = false
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

	for _, attachInfo := range r.currentProgram.Uprobe.AttachPoints {
		expectedAttachPoints, error := r.getExpectedAttachPoints(ctx, attachInfo)
		if error != nil {
			return fmt.Errorf("failed to get node attach points: %v", error)
		}
		for _, attachPoint := range expectedAttachPoints {
			index := r.findAttachPoint(attachPoint)
			if index != nil {
				// Attach point already exists, so set ShouldAttach to true.
				r.currentProgramState.Uprobe.AttachPoints[*index].AttachInfoStateCommon.ShouldAttach = true
			} else {
				// Attach point doesn't exist, so add it.
				r.Logger.Info("Attach point doesn't exist.  Adding it.")
				r.currentProgramState.Uprobe.AttachPoints = append(r.currentProgramState.Uprobe.AttachPoints, attachPoint)
			}
		}
	}

	// If any existing attach point is no longer on a list of expected attach
	// points, ShouldAttach will remain set to false and it will get detached in
	// a following step.

	return nil
}

// ANF-TODO: Confirm what constitutes a match between two attach points.
func (r *UprobeProgramReconciler) findAttachPoint(attachInfoState bpfmaniov1alpha1.UprobeAttachInfoState) *int {
	for i, a := range r.currentProgramState.Uprobe.AttachPoints {
		// attachInfoState is the same as a if the the following fields are the
		// same: IfName, ContainerPid, Priority, and Direction.
		if a.FunctionName == attachInfoState.FunctionName && a.Offset == attachInfoState.Offset &&
			a.Target == attachInfoState.Target &&
			reflect.DeepEqual(a.Pid, attachInfoState.Pid) &&
			reflect.DeepEqual(a.ContainerPid, attachInfoState.ContainerPid) {
			return &i
		}
	}
	return nil
}

// processAttachInfo calls reconcileBpfAttachment() for each attach point. It
// then updates the ProgramAttachStatus based on the updated status of each
// attach point.
func (r *UprobeProgramReconciler) processAttachInfo(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.BpfFunctionName)

	// The following map is used to keep track of attach points that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// attach points.
	attachPointsToRemove := make(map[int]bool)

	var lastReconcileAttachmentError error = nil
	for i := range r.currentProgramState.Uprobe.AttachPoints {
		r.currentAttachPoint = &r.currentProgramState.Uprobe.AttachPoints[i]
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
		r.currentProgramState.Uprobe.AttachPoints = r.removeAttachPoints(r.currentProgramState.Uprobe.AttachPoints, attachPointsToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileAttachmentError
}

func (r *UprobeProgramReconciler) updateProgramAttachStatus() {
	for _, attachPoint := range r.currentProgramState.Uprobe.AttachPoints {
		if !isAttachSuccess(attachPoint.ShouldAttach, attachPoint.AttachPointStatus) {
			r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeAttachPoints removes attach points from a slice of attach points based on the keys in the map.
func (r *UprobeProgramReconciler) removeAttachPoints(attachPoints []bpfmaniov1alpha1.UprobeAttachInfoState, attachPointsToRemove map[int]bool) []bpfmaniov1alpha1.UprobeAttachInfoState {
	var remainingAttachPoints []bpfmaniov1alpha1.UprobeAttachInfoState
	for i, a := range attachPoints {
		if _, ok := attachPointsToRemove[i]; !ok {
			remainingAttachPoints = append(remainingAttachPoints, a)
		}
	}
	return remainingAttachPoints
}

// getExpectedAttachPoints expands *AttachInfo into a list of specific attach
// points.
func (r *UprobeProgramReconciler) getExpectedAttachPoints(ctx context.Context, attachInfo bpfmaniov1alpha1.UprobeAttachInfo,
) ([]bpfmaniov1alpha1.UprobeAttachInfoState, error) {
	nodeAttachPoints := []bpfmaniov1alpha1.UprobeAttachInfoState{}

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
				containerPid := container.pid
				attachPoint := bpfmaniov1alpha1.UprobeAttachInfoState{
					AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
						ShouldAttach:      true,
						UUID:              uuid.New().String(),
						AttachId:          nil,
						AttachPointStatus: bpfmaniov1alpha1.ApAttachNotAttached,
					},
					FunctionName: attachInfo.FunctionName,
					Offset:       attachInfo.Offset,
					Target:       attachInfo.Target,
					Retprobe:     attachInfo.Retprobe,
					Pid:          attachInfo.Pid,
					ContainerPid: &containerPid,
				}
				nodeAttachPoints = append(nodeAttachPoints, attachPoint)
			}
		}
	} else {
		attachPoint := bpfmaniov1alpha1.UprobeAttachInfoState{
			AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
				ShouldAttach:      true,
				UUID:              uuid.New().String(),
				AttachId:          nil,
				AttachPointStatus: bpfmaniov1alpha1.ApAttachNotAttached,
			},
			FunctionName: attachInfo.FunctionName,
			Offset:       attachInfo.Offset,
			Target:       attachInfo.Target,
			Retprobe:     attachInfo.Retprobe,
			Pid:          attachInfo.Pid,
		}
		nodeAttachPoints = append(nodeAttachPoints, attachPoint)
	}

	return nodeAttachPoints, nil
}

func (r *UprobeProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.BpfFunctionName,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
