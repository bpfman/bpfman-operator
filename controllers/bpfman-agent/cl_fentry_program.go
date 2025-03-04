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

// ClFentryProgramReconciler contains the info required to reconcile a
// FentryProgram
type ClFentryProgramReconciler struct {
	ReconcilerCommon
	ClProgramReconcilerCommon
	currentLink *bpfmaniov1alpha1.ClFentryAttachInfoState
}

func (r *ClFentryProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *ClFentryProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracing
}

func (r *ClFentryProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_FENTRY
}

func (r *ClFentryProgramReconciler) getProgName() string {
	return r.currentProgram.Name
}

func (r *ClFentryProgramReconciler) shouldAttach() bool {
	return r.currentLink.ShouldAttach
}

func (r *ClFentryProgramReconciler) isAttached(ctx context.Context) bool {
	if r.currentProgramState.ProgramId == nil || r.currentLink.LinkId == nil {
		return false
	}
	return r.doesLinkExist(ctx, *r.currentProgramState.ProgramId, *r.currentLink.LinkId)
}

func (r *ClFentryProgramReconciler) getUUID() string {
	return r.currentLink.UUID
}

func (r *ClFentryProgramReconciler) getLinkId() *uint32 {
	return r.currentLink.LinkId
}

func (r *ClFentryProgramReconciler) setLinkId(id *uint32) {
	r.currentLink.LinkId = id
}

func (r *ClFentryProgramReconciler) setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus) {
	r.currentProgramState.ProgramLinkStatus = status
}

func (r *ClFentryProgramReconciler) getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus {
	return r.currentProgramState.ProgramLinkStatus
}

func (r *ClFentryProgramReconciler) setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus) {
	r.currentLink.LinkStatus = status
}

func (r *ClFentryProgramReconciler) getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus {
	return r.currentLink.LinkStatus
}

func (r *ClFentryProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {
	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &gobpfman.FentryAttachInfo{
					Metadata: map[string]string{internal.UuidMetadataKey: string(r.currentLink.UUID)},
				},
			},
		},
	}
}

// updateLinks processes the *ProgramInfo and updates the list of links
// contained in *AttachInfoState.
func (r *ClFentryProgramReconciler) updateLinks(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Fentry updateAttachInfo()", "isBeingDeleted", isBeingDeleted)
	// Set ShouldAttach for all links in the node CRD to false.  We'll
	// update this in the next step for all links that are still
	// present.

	for i := range r.currentProgramState.FentryInfo.Links {
		r.currentProgramState.FentryInfo.Links[i].ShouldAttach = false
	}

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		return nil
	}

	if r.currentProgram.FentryInfo != nil && r.currentProgram.FentryInfo.Links != nil {
		for _, attachInfo := range r.currentProgram.FentryInfo.Links {
			expectedLinks, error := r.getExpectedLinks(attachInfo)
			if error != nil {
				return fmt.Errorf("failed to get node links: %v", error)
			}
			for _, link := range expectedLinks {
				index := r.findLink(link)
				if index != nil {
					// Link already exists, so set ShouldAttach to true.
					r.currentProgramState.FentryInfo.Links[*index].AttachInfoStateCommon.ShouldAttach = true
				} else {
					// Link doesn't exist, so add it.
					r.Logger.Info("Link doesn't exist.  Adding it.")
					r.currentProgramState.FentryInfo.Links = append(r.currentProgramState.FentryInfo.Links, link)
				}
			}
		}
	}

	// If any existing link is no longer on a list of expected links
	// ShouldAttach will remain set to false and it will get detached in a
	// following step.

	return nil
}

func (r *ClFentryProgramReconciler) findLink(_ bpfmaniov1alpha1.ClFentryAttachInfoState) *int {
	for i := range r.currentProgramState.FentryInfo.Links {
		return &i
	}
	return nil
}

// processLinks calls reconcileBpfLink() for each link. It
// then updates the ProgramAttachStatus based on the updated status of each
// link.
func (r *ClFentryProgramReconciler) processLinks(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.Name)

	// The following map is used to keep track of links that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// links.
	linksToRemove := make(map[int]bool)

	var lastReconcileLinkError error = nil
	for i := range r.currentProgramState.FentryInfo.Links {
		r.currentLink = &r.currentProgramState.FentryInfo.Links[i]
		remove, err := r.reconcileBpfLink(ctx, r)
		if err != nil {
			r.Logger.Error(err, "failed to reconcile bpf link", "index", i)
			// All errors are logged, but the last error is saved to return and
			// we continue to process the rest of the links so errors
			// don't block valid links.
			lastReconcileLinkError = err
		}

		if remove {
			r.Logger.Info("Marking link for removal", "index", i)
			linksToRemove[i] = true
		}
	}

	if len(linksToRemove) > 0 {
		r.Logger.Info("Removing links", "linksToRemove", linksToRemove)
		r.currentProgramState.FentryInfo.Links = r.removeLinks(r.currentProgramState.FentryInfo.Links, linksToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileLinkError
}

func (r *ClFentryProgramReconciler) updateProgramAttachStatus() {
	for _, link := range r.currentProgramState.FentryInfo.Links {
		if !isAttachSuccess(link.ShouldAttach, link.LinkStatus) {
			r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeLinks removes links from a slice of links based on the keys in the map.
func (r *ClFentryProgramReconciler) removeLinks(links []bpfmaniov1alpha1.ClFentryAttachInfoState, linksToRemove map[int]bool) []bpfmaniov1alpha1.ClFentryAttachInfoState {
	var remainingLinks []bpfmaniov1alpha1.ClFentryAttachInfoState
	for i, a := range links {
		if _, ok := linksToRemove[i]; !ok {
			remainingLinks = append(remainingLinks, a)
		}
	}
	return remainingLinks
}

// getExpectedLinks expands *AttachInfo into a list of specific attach
// points.
func (r *ClFentryProgramReconciler) getExpectedLinks(_ bpfmaniov1alpha1.ClFentryAttachInfo,
) ([]bpfmaniov1alpha1.ClFentryAttachInfoState, error) {
	nodeLinks := []bpfmaniov1alpha1.ClFentryAttachInfoState{}

	link := bpfmaniov1alpha1.ClFentryAttachInfoState{
		AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
			ShouldAttach: true,
			UUID:         uuid.New().String(),
			LinkId:       nil,
			LinkStatus:   bpfmaniov1alpha1.ApAttachNotAttached,
		},
	}
	nodeLinks = append(nodeLinks, link)

	return nodeLinks, nil
}

func (r *ClFentryProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.Name,
		ProgramType: r.getBpfmanProgType(),
		Info: &gobpfman.ProgSpecificInfo{
			Info: &gobpfman.ProgSpecificInfo_FentryLoadInfo{
				FentryLoadInfo: &gobpfman.FentryLoadInfo{
					FnName: r.currentProgram.FentryInfo.Function,
				},
			},
		},
	}
}
