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

// FexitProgramReconciler contains the info required to reconcile a
// FexitProgram
type FexitProgramReconciler struct {
	ReconcilerCommon
	ProgramReconcilerCommon
}

func (r *FexitProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *FexitProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracing
}

func (r *FexitProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_FEXIT
}

func (r *FexitProgramReconciler) getProgName() string {
	return r.currentProgram.BpfFunctionName
}

func (r *FexitProgramReconciler) shouldAttach() bool {
	return r.currentProgramState.Fexit.ShouldAttach
}

func (r *FexitProgramReconciler) isAttached() bool {
	// ANF-TODO: Make this check more robust.  Some ideas include: get the link
	// to confirm it exists.  Confirm, that it matches what we expect. If not,
	// check if there is another link that contains the UUID for this link.
	return r.currentProgramState.Fexit.AttachId != nil
}

func (r *FexitProgramReconciler) getUUID() string {
	return r.currentProgramState.Fexit.UUID
}

func (r *FexitProgramReconciler) getAttachId() *uint32 {
	return r.currentProgramState.Fexit.AttachId
}

func (r *FexitProgramReconciler) setAttachId(id *uint32) {
	r.currentProgramState.Fexit.AttachId = id
}

func (r *FexitProgramReconciler) setProgramAttachStatus(status bpfmaniov1alpha1.ProgramAttachStatus) {
	r.currentProgramState.ProgramAttachStatus = status
}

func (r *FexitProgramReconciler) getProgramAttachStatus() bpfmaniov1alpha1.ProgramAttachStatus {
	return r.currentProgramState.ProgramAttachStatus
}

func (r *FexitProgramReconciler) setCurrentAttachPointStatus(status bpfmaniov1alpha1.AttachPointStatus) {
	r.currentProgramState.Fexit.AttachPointStatus = status
}

func (r *FexitProgramReconciler) getCurrentAttachPointStatus() bpfmaniov1alpha1.AttachPointStatus {
	return r.currentProgramState.Fexit.AttachPointStatus
}

func (r *FexitProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {
	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FexitAttachInfo{
				FexitAttachInfo: &gobpfman.FexitAttachInfo{
					Metadata: map[string]string{internal.UuidMetadataKey: string(r.currentProgramState.Fexit.UUID)},
				},
			},
		},
	}
}

// updateAttachInfo processes the *ProgramInfo and updates the list of attach
// points contained in *AttachInfoState.
func (r *FexitProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Fexit updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	r.currentProgramState.Fexit.Attach = r.currentProgram.Fexit.Attach

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		//
		// ANF-TODO: When we have load/attach split, we shouldn't even need to
		// set ShouldAttach to false above, because unloading the program should
		// remove all attachments and updateAttachInfo won't be called.  We
		// probably should delete AttachPoints when unloading the program.

		r.currentProgramState.Fexit.ShouldAttach = false
		return nil
	}

	r.currentProgramState.Fexit.ShouldAttach = r.currentProgram.Fexit.Attach

	return nil
}

// processAttachInfo calls reconcileBpfAttachment() for each attach point. It
// then updates the ProgramAttachStatus based on the updated status of each
// attach point.
func (r *FexitProgramReconciler) processAttachInfo(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.BpfFunctionName)

	_, err := r.reconcileBpfAttachment(ctx, r)
	r.updateProgramAttachStatus()
	return err
}

func (r *FexitProgramReconciler) updateProgramAttachStatus() {
	if !isAttachSuccess(r.shouldAttach(), r.getCurrentAttachPointStatus()) {
		r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachError)
	} else {
		r.setProgramAttachStatus(bpfmaniov1alpha1.ProgAttachSuccess)
	}
}

func (r *FexitProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.BpfFunctionName,
		ProgramType: r.getBpfmanProgType(),
		Info: &gobpfman.ProgSpecificInfo{
			Info: &gobpfman.ProgSpecificInfo_FexitLoadInfo{
				FexitLoadInfo: &gobpfman.FexitLoadInfo{
					FnName: r.currentProgram.Fexit.FunctionName,
				},
			},
		},
	}
}
