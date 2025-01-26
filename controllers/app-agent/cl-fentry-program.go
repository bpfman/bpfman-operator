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

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/app-agent/internal"
	internal "github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/google/uuid"

	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type FentryProgramReconciler struct {
	ReconcilerCommon
	ProgramReconcilerCommon
	// ANF-TODO: appCommon is needed to load the program. It won't be needed
	// after the load/attch split is ready.
	currentAttachPoint *bpfmaniov1alpha1.FentryAttachInfoState
}

func (r *FentryProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *FentryProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracing
}

func (r *FentryProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *FentryProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.appCommon.GlobalData
}

func (r *FentryProgramReconciler) shouldAttach() bool {
	return r.currentAttachPoint.ShouldAttach
}

func (r *FentryProgramReconciler) getUUID() string {
	return r.currentAttachPoint.UUID
}

func (r *FentryProgramReconciler) getAttachId() *uint32 {
	return r.currentAttachPoint.AttachId
}

func (r *FentryProgramReconciler) setAttachId(id *uint32) {
	r.currentAttachPoint.AttachId = id
}

func (r *FentryProgramReconciler) setAttachStatus(status bpfmaniov1alpha1.BpfProgramConditionType) bool {
	return updateSimpleStatus(&r.currentAttachPoint.AttachStatus, status)
}

func (r *FentryProgramReconciler) getAttachStatus() bpfmaniov1alpha1.BpfProgramConditionType {
	return r.currentAttachPoint.AttachStatus
}

func (r *FentryProgramReconciler) getLoadRequest(mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	r.Logger.Info("Getting load request", "bpfFunctionName", r.currentProgram.Fentry.BpfFunctionName, "reqAttachInfo", r.currentAttachPoint, "mapOwnerId",
		mapOwnerId, "ByteCode", r.appCommon.ByteCode)

	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.appCommon.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentProgram.Fentry.BpfFunctionName,
		ProgramType: uint32(internal.Tracing),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &gobpfman.FentryAttachInfo{
					FnName: r.currentProgram.Fentry.FunctionName,
				},
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
func (r *FentryProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Fentry updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all attach points in the node CRD to false.  We'll
	// update this in the next step for all attach points that are still
	// present.
	for i := range r.currentProgramState.Fentry.AttachPoints {
		r.Logger.Info("Setting ShouldAttach to false", "index", i)
		r.currentProgramState.Fentry.AttachPoints[i].ShouldAttach = false
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

	// ANF-TODO: Fentry and Fexit just have a single attach point, so we don't
	// have to do a loop here.  This makes them different from most of the other
	// program types, so we'd have to handle this if we try to make this
	// function common.
	expectedAttachPoints, error := r.getExpectedAttachPoints(ctx, r.currentProgram.Fentry.FentryAttachInfo)
	if error != nil {
		return fmt.Errorf("failed to get node attach points: %v", error)
	}
	for _, attachPoint := range expectedAttachPoints {
		index := r.findAttachPoint(attachPoint)
		if index != nil {
			// Attach point already exists, so set ShouldAttach to true.
			r.Logger.Info("Setting ShouldAttach to true", "index", *index)
			r.currentProgramState.Fentry.AttachPoints[*index].AttachInfoCommon.ShouldAttach = true
		} else {
			// Attach point doesn't exist, so add it.
			r.Logger.Info("Attach point doesn't exist.  Adding it.")
			r.currentProgramState.Fentry.AttachPoints = append(r.currentProgramState.Fentry.AttachPoints, attachPoint)
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
func (r *FentryProgramReconciler) findAttachPoint(attachInfoState bpfmaniov1alpha1.FentryAttachInfoState) *int {
	for i, a := range r.currentProgramState.Fentry.AttachPoints {
		// attachInfoState is the same as a if the the following fields are the
		// same: FunctionName.
		if a.FunctionName == attachInfoState.FunctionName {
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
func (r *FentryProgramReconciler) processAttachInfo(ctx context.Context, mapOwnerStatus *MapOwnerParamStatus) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.Fentry.BpfFunctionName,
		"mapOwnerStatus", mapOwnerStatus)

	// Get existing ebpf state from bpfman.
	loadedBpfPrograms, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, r.getProgType())
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
	for i := range r.currentProgramState.Fentry.AttachPoints {
		r.currentAttachPoint = &r.currentProgramState.Fentry.AttachPoints[i]
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
		r.currentProgramState.Fentry.AttachPoints = r.removeAttachPoints(r.currentProgramState.Fentry.AttachPoints, attachPointsToRemove)
	}

	return lastReconcileAttachmentError
}

// removeAttachPoints removes attach points from a slice of attach points based on the keys in the map.
func (r *FentryProgramReconciler) removeAttachPoints(attachPoints []bpfmaniov1alpha1.FentryAttachInfoState, attachPointsToRemove map[int]bool) []bpfmaniov1alpha1.FentryAttachInfoState {
	var remainingAttachPoints []bpfmaniov1alpha1.FentryAttachInfoState
	for i, a := range attachPoints {
		if _, ok := attachPointsToRemove[i]; !ok {
			remainingAttachPoints = append(remainingAttachPoints, a)
		}
	}
	return remainingAttachPoints
}

// getInterfaces expands FentryAttachInfo into a list of specific attach points.  It works pretty much like the old getExpectedBpfPrograms.
func (r *FentryProgramReconciler) getExpectedAttachPoints(ctx context.Context, attachInfo bpfmaniov1alpha1.FentryAttachInfo) ([]bpfmaniov1alpha1.FentryAttachInfoState, error) {
	attachPoints := []bpfmaniov1alpha1.FentryAttachInfoState{}

	if r.currentProgram.Fentry.Attach {
		attachPoint := bpfmaniov1alpha1.FentryAttachInfoState{
			AttachInfoCommon: bpfmaniov1alpha1.AttachInfoCommon{
				ShouldAttach: r.currentProgram.Fentry.Attach,
				UUID:         uuid.New().String(),
				AttachId:     nil,
				AttachStatus: bpfmaniov1alpha1.BpfProgCondNotAttached,
			},
			FunctionName: r.currentProgram.Fentry.FunctionName,
		}
		attachPoints = append(attachPoints, attachPoint)
	}

	return attachPoints, nil
}
