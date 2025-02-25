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

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
)

// FentryProgramReconciler contains the info required to reconcile a
// FentryProgram
type FentryProgramReconciler struct {
	ReconcilerCommon
	ProgramReconcilerCommon
}

func (r *FentryProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *FentryProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracing
}

func (r *FentryProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_FENTRY
}

func (r *FentryProgramReconciler) getProgName() string {
	return r.currentProgram.BpfFunctionName
}

func (r *FentryProgramReconciler) shouldAttach() bool {
	return r.currentProgramState.Fentry.ShouldAttach
}

func (r *FentryProgramReconciler) isAttached() bool {
	// ANF-TODO: Make this check more robust.  Some ideas include: get the link
	// to confirm it exists.  Confirm, that it matches what we expect. If not,
	// check if there is another link that contains the UUID for this link.
	return r.currentProgramState.Fentry.AttachId != nil
}

func (r *FentryProgramReconciler) getUUID() string {
	return r.currentProgramState.Fentry.UUID
}

func (r *FentryProgramReconciler) getAttachId() *uint32 {
	return r.currentProgramState.Fentry.AttachId
}

func (r *FentryProgramReconciler) setAttachId(id *uint32) {
	r.currentProgramState.Fentry.AttachId = id
}

func (r *FentryProgramReconciler) setProgramAttachStatus(status bpfmaniov1alpha1.ProgramAttachStatus) {
	r.currentProgramState.ProgramAttachStatus = status
}

func (r *FentryProgramReconciler) getProgramAttachStatus() bpfmaniov1alpha1.ProgramAttachStatus {
	return r.currentProgramState.ProgramAttachStatus
}

func (r *FentryProgramReconciler) setCurrentAttachPointStatus(status bpfmaniov1alpha1.AttachPointStatus) {
	r.currentProgramState.Fentry.AttachPointStatus = status
}

func (r *FentryProgramReconciler) getCurrentAttachPointStatus() bpfmaniov1alpha1.AttachPointStatus {
	return r.currentProgramState.Fentry.AttachPointStatus
}

func (r *FentryProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {
	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &gobpfman.FentryAttachInfo{
					Metadata: map[string]string{internal.UuidMetadataKey: string(r.currentProgramState.Fentry.UUID)},
				},
			},
		},
	}
}

// updateAttachInfo processes the *ProgramInfo and updates the list of attach
// points contained in *AttachInfoState.
func (r *FentryProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Fentry updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	r.currentProgramState.Fentry.Attach = r.currentProgram.Fentry.Attach

	if isBeingDeleted {
		// ANF-TODO: When we have load/attach split, we shouldn't need to
		// set ShouldAttach to false above, because unloading the program should
		// remove all attachments and updateAttachInfo won't be called.
		r.currentProgramState.Fentry.ShouldAttach = false
		// If the program is being deleted, we don't need to do anything else.
		return nil
	}

	r.currentProgramState.Fentry.ShouldAttach = r.currentProgram.Fentry.Attach

	return nil
}

// processAttachInfo calls reconcileBpfAttachment() for each attach point. It
// then updates the ProgramAttachStatus based on the updated status of each
// attach point.
func (r *FentryProgramReconciler) processAttachInfo(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.BpfFunctionName)

	_, err := r.reconcileBpfAttachment(ctx, r)
	r.updateProgramAttachStatus()
	return err
}

func (r *FentryProgramReconciler) updateProgramAttachStatus() {
	if !isAttachSuccess(r.shouldAttach(), r.getCurrentAttachPointStatus()) {
		r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachError)
	} else {
		r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachSuccess)
	}
}

func (r *FentryProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.BpfFunctionName,
		ProgramType: r.getBpfmanProgType(),
		Info: &gobpfman.ProgSpecificInfo{
			Info: &gobpfman.ProgSpecificInfo_FentryLoadInfo{
				FentryLoadInfo: &gobpfman.FentryLoadInfo{
					FnName: r.currentProgram.Fentry.FunctionName,
				},
			},
		},
	}
}
