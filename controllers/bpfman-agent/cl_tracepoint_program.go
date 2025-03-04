/*
Copyright 2025 The bpfman Authors.

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

// ClTracepointProgramReconciler contains the info required to reconcile a TracepointProgram
type ClTracepointProgramReconciler struct {
	ReconcilerCommon
	ClProgramReconcilerCommon
	currentLink *bpfmaniov1alpha1.ClTracepointAttachInfoState
}

func (r *ClTracepointProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *ClTracepointProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracepoint
}

func (r *ClTracepointProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_TRACEPOINT
}

func (r *ClTracepointProgramReconciler) getProgName() string {
	return r.currentProgram.Name
}

func (r *ClTracepointProgramReconciler) shouldAttach() bool {
	return r.currentLink.ShouldAttach
}

func (r *ClTracepointProgramReconciler) isAttached(ctx context.Context) bool {
	if r.currentProgramState.ProgramId == nil || r.currentLink.LinkId == nil {
		return false
	}
	return r.doesLinkExist(ctx, *r.currentProgramState.ProgramId, *r.currentLink.LinkId)
}

func (r *ClTracepointProgramReconciler) getUUID() string {
	return r.currentLink.UUID
}

func (r *ClTracepointProgramReconciler) getLinkId() *uint32 {
	return r.currentLink.LinkId
}

func (r *ClTracepointProgramReconciler) setLinkId(id *uint32) {
	r.currentLink.LinkId = id
}

func (r *ClTracepointProgramReconciler) setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus) {
	r.currentProgramState.ProgramLinkStatus = status
}

func (r *ClTracepointProgramReconciler) getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus {
	return r.currentProgramState.ProgramLinkStatus
}

func (r *ClTracepointProgramReconciler) setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus) {
	r.currentLink.LinkStatus = status
}

func (r *ClTracepointProgramReconciler) getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus {
	return r.currentLink.LinkStatus
}

func (r *ClTracepointProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {
	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TracepointAttachInfo{
				TracepointAttachInfo: &gobpfman.TracepointAttachInfo{
					Tracepoint: r.currentLink.Name,
					Metadata:   map[string]string{internal.UuidMetadataKey: string(r.currentLink.UUID)},
				},
			},
		},
	}
}

// updateLinks processes the *ProgramInfo and updates the list of links
// contained in *AttachInfoState.
func (r *ClTracepointProgramReconciler) updateLinks(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Tracepoint updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all links in the node CRD to false.  We'll
	// update this in the next step for all links that are still
	// present.
	for i := range r.currentProgramState.TracepointInfo.Links {
		r.currentProgramState.TracepointInfo.Links[i].ShouldAttach = false
	}

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		return nil
	}

	if r.currentProgram.TracepointInfo != nil && r.currentProgram.TracepointInfo.Links != nil {
		for _, attachInfo := range r.currentProgram.TracepointInfo.Links {
			expectedLinks, error := r.getExpectedLinks(attachInfo)
			if error != nil {
				return fmt.Errorf("failed to get node links: %v", error)
			}
			for _, link := range expectedLinks {
				index := r.findLink(link)
				if index != nil {
					// Link already exists, so set ShouldAttach to true.
					r.currentProgramState.TracepointInfo.Links[*index].AttachInfoStateCommon.ShouldAttach = true
				} else {
					// Link doesn't exist, so add it.
					r.Logger.Info("Link doesn't exist.  Adding it.")
					r.currentProgramState.TracepointInfo.Links = append(r.currentProgramState.TracepointInfo.Links, link)
				}
			}
		}
	}

	// If any existing link is no longer on a list of expected links
	// ShouldAttach will remain set to false and it will get detached in a
	// following step.
	// a following step.

	return nil
}

func (r *ClTracepointProgramReconciler) findLink(attachInfoState bpfmaniov1alpha1.ClTracepointAttachInfoState) *int {
	for i, a := range r.currentProgramState.TracepointInfo.Links {
		// attachInfoState is the same as a if the the following fields are the
		// same: IfName, ContainerPid, Priority, and Direction.
		if a.Name == attachInfoState.Name {
			return &i
		}
	}
	return nil
}

// processLinks calls reconcileBpfLink() for each link. It
// then updates the ProgramAttachStatus based on the updated status of each
// link.
func (r *ClTracepointProgramReconciler) processLinks(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.Name)

	// The following map is used to keep track of links that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// links.
	linksToRemove := make(map[int]bool)

	var lastReconcileLinkError error = nil
	for i := range r.currentProgramState.TracepointInfo.Links {
		r.currentLink = &r.currentProgramState.TracepointInfo.Links[i]
		remove, err := r.reconcileBpfLink(ctx, r)
		if err != nil {
			r.Logger.Error(err, "failed to reconcile bpf attachment", "index", i)
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
		r.currentProgramState.TracepointInfo.Links = r.removeLinks(r.currentProgramState.TracepointInfo.Links, linksToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileLinkError
}

func (r *ClTracepointProgramReconciler) updateProgramAttachStatus() {
	for _, link := range r.currentProgramState.TracepointInfo.Links {
		if !isAttachSuccess(link.ShouldAttach, link.LinkStatus) {
			r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeLinks removes links from a slice of links based on the keys in the map.
func (r *ClTracepointProgramReconciler) removeLinks(links []bpfmaniov1alpha1.ClTracepointAttachInfoState, linksToRemove map[int]bool) []bpfmaniov1alpha1.ClTracepointAttachInfoState {
	var remainingLinks []bpfmaniov1alpha1.ClTracepointAttachInfoState
	for i, a := range links {
		if _, ok := linksToRemove[i]; !ok {
			remainingLinks = append(remainingLinks, a)
		}
	}
	return remainingLinks
}

// getExpectedLinks expands *AttachInfo into a list of specific attach
// points.
func (r *ClTracepointProgramReconciler) getExpectedLinks(attachInfo bpfmaniov1alpha1.ClTracepointAttachInfo,
) ([]bpfmaniov1alpha1.ClTracepointAttachInfoState, error) {
	nodeLinks := []bpfmaniov1alpha1.ClTracepointAttachInfoState{}

	link := bpfmaniov1alpha1.ClTracepointAttachInfoState{
		AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
			ShouldAttach: true,
			UUID:         uuid.New().String(),
			LinkId:       nil,
			LinkStatus:   bpfmaniov1alpha1.ApAttachNotAttached,
		},
		Name: attachInfo.Name,
	}
	nodeLinks = append(nodeLinks, link)

	return nodeLinks, nil
}

func (r *ClTracepointProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.Name,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
