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

// NsUprobeProgramReconciler contains the info required to reconcile a UprobeNsProgram
type NsUprobeProgramReconciler struct {
	ReconcilerCommon
	NsProgramReconcilerCommon
	currentLink *bpfmaniov1alpha1.UprobeAttachInfoState
}

func (r *NsUprobeProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *NsUprobeProgramReconciler) getProgType() internal.ProgramType {
	return internal.Kprobe
}

func (r *NsUprobeProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_UPROBE
}

func (r *NsUprobeProgramReconciler) getProgName() string {
	return r.currentProgram.Name
}

func (r *NsUprobeProgramReconciler) shouldAttach() bool {
	return r.currentLink.ShouldAttach
}

func (r *NsUprobeProgramReconciler) isAttached(ctx context.Context) bool {
	if r.currentProgramState.ProgramId == nil || r.currentLink.LinkId == nil {
		return false
	}
	return r.doesLinkExist(ctx, *r.currentProgramState.ProgramId, *r.currentLink.LinkId)
}

func (r *NsUprobeProgramReconciler) getUUID() string {
	return r.currentLink.UUID
}

func (r *NsUprobeProgramReconciler) getLinkId() *uint32 {
	return r.currentLink.LinkId
}

func (r *NsUprobeProgramReconciler) setLinkId(id *uint32) {
	r.currentLink.LinkId = id
}

func (r *NsUprobeProgramReconciler) setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus) {
	r.currentProgramState.ProgramLinkStatus = status
}

func (r *NsUprobeProgramReconciler) getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus {
	return r.currentProgramState.ProgramLinkStatus
}

func (r *NsUprobeProgramReconciler) setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus) {
	r.currentLink.LinkStatus = status
}

func (r *NsUprobeProgramReconciler) getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus {
	return r.currentLink.LinkStatus
}

func (r *NsUprobeProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {

	attachInfo := &gobpfman.UprobeAttachInfo{
		FnName:   &r.currentLink.Function,
		Offset:   r.currentLink.Offset,
		Target:   r.currentLink.Target,
		Pid:      r.currentLink.Pid,
		Metadata: map[string]string{internal.UuidMetadataKey: string(r.currentLink.UUID)},
	}

	containerPid := int32(r.currentLink.ContainerPid)
	attachInfo.ContainerPid = &containerPid

	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: attachInfo,
			},
		},
	}
}

// updateLinks processes the *ProgramInfo and updates the list of links
// contained in *AttachInfoState.
func (r *NsUprobeProgramReconciler) updateLinks(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("Uprobe updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

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
		expectedLinks, error := r.getExpectedLinks(ctx, attachInfo)
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

func (r *NsUprobeProgramReconciler) findLink(attachInfoState bpfmaniov1alpha1.UprobeAttachInfoState,
	links *[]bpfmaniov1alpha1.UprobeAttachInfoState) *int {
	for i, a := range *links {
		// attachInfoState is the same as a if the the following fields are the
		// same: IfName, ContainerPid, Priority, and Direction.
		if a.Function == attachInfoState.Function && a.Offset == attachInfoState.Offset {
			return &i
		}
	}
	return nil
}

// processLinks calls reconcileBpfLink() for each link. It
// then updates the ProgramAttachStatus based on the updated status of each
// link.
func (r *NsUprobeProgramReconciler) processLinks(ctx context.Context) error {
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
		*appStateLinks = r.removeLinks(*appStateLinks, linksToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileLinkError
}

func (r *NsUprobeProgramReconciler) updateProgramAttachStatus() {
	appStateLinks := r.getAppStateLinks()
	for _, link := range *appStateLinks {
		if !isAttachSuccess(link.ShouldAttach, link.LinkStatus) {
			r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

func (r *NsUprobeProgramReconciler) getAppStateLinks() *[]bpfmaniov1alpha1.UprobeAttachInfoState {
	var appStateLinks *[]bpfmaniov1alpha1.UprobeAttachInfoState
	switch r.currentProgramState.Type {
	case bpfmaniov1alpha1.ProgTypeUprobe:
		appStateLinks = &r.currentProgramState.UprobeInfo.Links
	case bpfmaniov1alpha1.ProgTypeUretprobe:
		appStateLinks = &r.currentProgramState.UretprobeInfo.Links
	default:
		r.Logger.Error(fmt.Errorf("unexpected programState type: %v", r.currentProgramState.Type), "")
		appStateLinks = &[]bpfmaniov1alpha1.UprobeAttachInfoState{}
	}
	return appStateLinks
}

func (r *NsUprobeProgramReconciler) getAppLinks() *[]bpfmaniov1alpha1.UprobeAttachInfo {
	appLinks := &[]bpfmaniov1alpha1.UprobeAttachInfo{}
	switch r.currentProgram.Type {
	case bpfmaniov1alpha1.ProgTypeUprobe:
		if r.currentProgram.UprobeInfo != nil && r.currentProgram.UprobeInfo.Links != nil {
			appLinks = &r.currentProgram.UprobeInfo.Links
		}
	case bpfmaniov1alpha1.ProgTypeUretprobe:
		if r.currentProgram.UretprobeInfo != nil && r.currentProgram.UretprobeInfo.Links != nil {
			appLinks = &r.currentProgram.UretprobeInfo.Links
		}
	default:
		r.Logger.Error(fmt.Errorf("unexpected program type: %v", r.currentProgram.Type), "")
	}
	return appLinks
}

// removeLinks removes links from a slice of links based on the keys in the map.
func (r *NsUprobeProgramReconciler) removeLinks(links []bpfmaniov1alpha1.UprobeAttachInfoState, linksToRemove map[int]bool) []bpfmaniov1alpha1.UprobeAttachInfoState {
	var remainingLinks []bpfmaniov1alpha1.UprobeAttachInfoState
	for i, a := range links {
		if _, ok := linksToRemove[i]; !ok {
			remainingLinks = append(remainingLinks, a)
		}
	}
	return remainingLinks
}

// getExpectedLinks expands *AttachInfo into a list of specific attach
// points.
func (r *NsUprobeProgramReconciler) getExpectedLinks(ctx context.Context, attachInfo bpfmaniov1alpha1.UprobeAttachInfo,
) ([]bpfmaniov1alpha1.UprobeAttachInfoState, error) {
	nodeLinks := []bpfmaniov1alpha1.UprobeAttachInfoState{}

	// See if there are any matching containers on this node.
	containerInfo, err := r.Containers.GetContainers(
		ctx,
		r.namespace,
		attachInfo.Containers.Pods,
		attachInfo.Containers.ContainerNames,
		r.Logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get container pids: %v", err)
	}

	if containerInfo != nil && len(*containerInfo) != 0 {
		// Containers were found, so create links.
		for i := range *containerInfo {
			container := (*containerInfo)[i]
			containerPid := container.pid
			link := bpfmaniov1alpha1.UprobeAttachInfoState{
				AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
					ShouldAttach: true,
					UUID:         uuid.New().String(),
					LinkId:       nil,
					LinkStatus:   bpfmaniov1alpha1.ApAttachNotAttached,
				},
				Function:     attachInfo.Function,
				Offset:       attachInfo.Offset,
				Target:       attachInfo.Target,
				Pid:          attachInfo.Pid,
				ContainerPid: containerPid,
			}
			nodeLinks = append(nodeLinks, link)
		}
	}

	return nodeLinks, nil
}

func (r *NsUprobeProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.Name,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
