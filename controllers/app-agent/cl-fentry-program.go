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

	v1 "k8s.io/api/core/v1"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms,verbs=get;list;watch

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

func (r *FentryProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *FentryProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.appCommon.GlobalData
}

func (r *FentryProgramReconciler) shouldAttach() bool {
	return r.currentProgramState.Fentry.ShouldAttach
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

func (r *FentryProgramReconciler) setAttachStatus(status bpfmaniov1alpha1.BpfProgramConditionType) bool {
	return updateSimpleStatus(&r.currentProgramState.Fentry.AttachStatus, status)
}

func (r *FentryProgramReconciler) getAttachStatus() bpfmaniov1alpha1.BpfProgramConditionType {
	return r.currentProgramState.Fentry.AttachStatus
}

func (r *FentryProgramReconciler) getLoadRequest(mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	r.Logger.Info("Getting load request", "bpfFunctionName", r.currentProgram.BpfFunctionName,
		"mapOwnerId", mapOwnerId, "ByteCode", r.appCommon.ByteCode)

	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.appCommon.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	// ANF-TODO: This is a temporary workaround for backwards compatibility.
	// Fix it after old code removed.
	var bpfFunctionName string
	if r.currentProgram.BpfFunctionName != "" {
		bpfFunctionName = r.currentProgram.BpfFunctionName
	} else {
		bpfFunctionName = r.currentProgram.Fentry.BpfFunctionName
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        bpfFunctionName,
		ProgramType: uint32(internal.Tracing),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &gobpfman.FentryAttachInfo{
					FnName: r.currentProgram.Fentry.FunctionName,
				},
			},
		},
		Metadata: map[string]string{internal.UuidMetadataKey: string(r.currentProgramState.Fentry.UUID),
			internal.ProgramNameKey: "BpfApplication"},
		GlobalData: r.appCommon.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}

// updateAttachInfo processes the *ProgramInfo and updates the list of attach
// points contained in *AttachInfoState.
func (r *FentryProgramReconciler) updateAttachInfo(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Fentry updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	r.currentProgramState.Fentry.Attach = r.currentProgram.Fentry.Attach

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		//
		// ANF-TODO: When we have load/attach split, we shouldn't even need to
		// set ShouldAttach to false above, because unloading the program should
		// remove all attachments and updateAttachInfo won't be called.  We
		// probably should delete AttachPoints when unloading the program.

		r.currentProgramState.Fentry.ShouldAttach = false
		return nil
	}

	r.currentProgramState.Fentry.ShouldAttach = r.currentProgram.Fentry.Attach

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

	_, err = r.reconcileBpfAttachment(ctx, r, loadedBpfPrograms, mapOwnerStatus)

	return err
}
