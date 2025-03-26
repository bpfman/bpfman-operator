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

// ClKretprobeProgramReconciler contains the info required to reconcile a KretprobeProgram
type ClKretprobeProgramReconciler struct {
	ReconcilerCommon
	ClProgramReconcilerCommon
	currentLink *bpfmaniov1alpha1.ClKretprobeAttachInfoState
}

func (r *ClKretprobeProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *ClKretprobeProgramReconciler) getProgType() internal.ProgramType {
	return internal.Kprobe
}

func (r *ClKretprobeProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_KPROBE
}

func (r *ClKretprobeProgramReconciler) getProgName() string {
	return r.currentProgram.Name
}

func (r *ClKretprobeProgramReconciler) shouldAttach() bool {
	return r.currentLink.ShouldAttach
}

func (r *ClKretprobeProgramReconciler) isAttached(ctx context.Context) bool {
	if r.currentProgramState.ProgramId == nil || r.currentLink.LinkId == nil {
		return false
	}
	return r.doesLinkExist(ctx, *r.currentProgramState.ProgramId, *r.currentLink.LinkId)
}

func (r *ClKretprobeProgramReconciler) getUUID() string {
	return r.currentLink.UUID
}

func (r *ClKretprobeProgramReconciler) getLinkId() *uint32 {
	return r.currentLink.LinkId
}

func (r *ClKretprobeProgramReconciler) setLinkId(id *uint32) {
	r.currentLink.LinkId = id
}

func (r *ClKretprobeProgramReconciler) setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus) {
	r.currentProgramState.ProgramLinkStatus = status
}

func (r *ClKretprobeProgramReconciler) getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus {
	return r.currentProgramState.ProgramLinkStatus
}

func (r *ClKretprobeProgramReconciler) setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus) {
	r.currentLink.LinkStatus = status
}

func (r *ClKretprobeProgramReconciler) getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus {
	return r.currentLink.LinkStatus
}

func (r *ClKretprobeProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {
	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_KprobeAttachInfo{
				KprobeAttachInfo: &gobpfman.KprobeAttachInfo{
					FnName:   r.currentLink.Function,
					Metadata: map[string]string{internal.UuidMetadataKey: string(r.currentLink.UUID)},
				},
			},
		},
	}
}

// updateLinks processes the *ProgramInfo and updates the list of links
// contained in *AttachInfoState.
func (r *ClKretprobeProgramReconciler) updateLinks(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Kretprobe updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all links in the node CRD to false.  We'll
	// update this in the next step for all links that are still
	// present.

	appStateLinks := r.getAppStateLinks()
	for i := range *appStateLinks {
		(*appStateLinks)[i].ShouldAttach = false
	}

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		return nil
	}

	appLinks := r.getAppLinks()
	for _, attachInfo := range *appLinks {
		expectedLinks, error := r.getExpectedLinks(attachInfo)
		if error != nil {
			return fmt.Errorf("failed to get node links: %v", error)
		}
		for _, link := range expectedLinks {
			index := r.findLink(link, appStateLinks)
			if index != nil {
				// Link already exists, so set ShouldAttach to true.
				(*appStateLinks)[*index].AttachInfoStateCommon.ShouldAttach = true
			} else {
				// Link doesn't exist, so add it.
				r.Logger.Info("Link doesn't exist.  Adding it.")
				*appStateLinks = append(*appStateLinks, link)
			}
		}
	}

	// If any existing link is no longer on a list of expected links
	// ShouldAttach will remain set to false and it will get detached in a
	// following step.
	// a following step.

	return nil
}

func (r *ClKretprobeProgramReconciler) findLink(attachInfoState bpfmaniov1alpha1.ClKretprobeAttachInfoState,
	links *[]bpfmaniov1alpha1.ClKretprobeAttachInfoState) *int {
	for i, a := range *links {
		// attachInfoState is the same as a if the the following fields are the
		// same: Function.
		if a.Function == attachInfoState.Function {
			return &i
		}
	}
	return nil
}

// processLinks calls reconcileBpfLink() for each link. It
// then updates the ProgramAttachStatus based on the updated status of each
// link.
func (r *ClKretprobeProgramReconciler) processLinks(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.Name)

	// The following map is used to keep track of links that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// links.
	linksToRemove := make(map[int]bool)

	appStateLinks := r.getAppStateLinks()

	var lastReconcileLinkError error = nil
	for i := range *appStateLinks {
		r.currentLink = &(*appStateLinks)[i]
		remove, err := r.reconcileBpfLink(ctx, r)
		if err != nil {
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
		*appStateLinks = r.removeLinks(*appStateLinks, linksToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileLinkError
}

func (r *ClKretprobeProgramReconciler) updateProgramAttachStatus() {
	appStateLinks := r.getAppStateLinks()
	for _, link := range *appStateLinks {
		if !isAttachSuccess(link.ShouldAttach, link.LinkStatus) {
			r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

func (r *ClKretprobeProgramReconciler) getAppStateLinks() *[]bpfmaniov1alpha1.ClKretprobeAttachInfoState {
	var appStateLinks *[]bpfmaniov1alpha1.ClKretprobeAttachInfoState
	switch r.currentProgramState.Type {
	case bpfmaniov1alpha1.ProgTypeKretprobe:
		appStateLinks = &r.currentProgramState.KRetProbe.Links
	default:
		r.Logger.Error(fmt.Errorf("unexpected programState type: %v", r.currentProgramState.Type), "")
		appStateLinks = &[]bpfmaniov1alpha1.ClKretprobeAttachInfoState{}
	}
	return appStateLinks
}

func (r *ClKretprobeProgramReconciler) getAppLinks() *[]bpfmaniov1alpha1.ClKretprobeAttachInfo {
	appLinks := &[]bpfmaniov1alpha1.ClKretprobeAttachInfo{}
	switch r.currentProgram.Type {
	case bpfmaniov1alpha1.ProgTypeKretprobe:
		if r.currentProgram.KRetProbe != nil && r.currentProgram.KRetProbe.Links != nil {
			appLinks = &r.currentProgram.KRetProbe.Links
		}
	default:
		r.Logger.Error(fmt.Errorf("unexpected program type: %v", r.currentProgram.Type), "")
	}
	return appLinks
}

// removeLinks removes links from a slice of links based on the keys in the map.
func (r *ClKretprobeProgramReconciler) removeLinks(links []bpfmaniov1alpha1.ClKretprobeAttachInfoState, linksToRemove map[int]bool) []bpfmaniov1alpha1.ClKretprobeAttachInfoState {
	var remainingLinks []bpfmaniov1alpha1.ClKretprobeAttachInfoState
	for i, a := range links {
		if _, ok := linksToRemove[i]; !ok {
			remainingLinks = append(remainingLinks, a)
		}
	}
	return remainingLinks
}

// getExpectedLinks expands *AttachInfo into a list of specific attach
// points.
func (r *ClKretprobeProgramReconciler) getExpectedLinks(attachInfo bpfmaniov1alpha1.ClKretprobeAttachInfo,
) ([]bpfmaniov1alpha1.ClKretprobeAttachInfoState, error) {
	nodeLinks := []bpfmaniov1alpha1.ClKretprobeAttachInfoState{}

	link := bpfmaniov1alpha1.ClKretprobeAttachInfoState{
		AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
			ShouldAttach: true,
			UUID:         uuid.New().String(),
			LinkId:       nil,
			LinkStatus:   bpfmaniov1alpha1.ApAttachNotAttached,
		},
		Function: attachInfo.Function,
	}
	nodeLinks = append(nodeLinks, link)

	return nodeLinks, nil
}

func (r *ClKretprobeProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.Name,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
