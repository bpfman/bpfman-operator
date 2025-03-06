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
	"reflect"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/google/uuid"
)

// NsTcProgramReconciler contains the info required to reconcile a TcNsProgram
type NsTcProgramReconciler struct {
	ReconcilerCommon
	NsProgramReconcilerCommon
	currentLink *bpfmaniov1alpha1.TcAttachInfoState
}

func (r *NsTcProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *NsTcProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tc
}

func (r *NsTcProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_TC
}

func (r *NsTcProgramReconciler) getProgName() string {
	return r.currentProgram.Name
}

func (r *NsTcProgramReconciler) shouldAttach() bool {
	return r.currentLink.ShouldAttach
}

func (r *NsTcProgramReconciler) isAttached(ctx context.Context) bool {
	if r.currentProgramState.ProgramId == nil || r.currentLink.LinkId == nil {
		return false
	}
	return r.doesLinkExist(ctx, *r.currentProgramState.ProgramId, *r.currentLink.LinkId)
}

func (r *NsTcProgramReconciler) getUUID() string {
	return r.currentLink.UUID
}

func (r *NsTcProgramReconciler) getLinkId() *uint32 {
	return r.currentLink.LinkId
}

func (r *NsTcProgramReconciler) setLinkId(id *uint32) {
	r.currentLink.LinkId = id
}

func (r *NsTcProgramReconciler) setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus) {
	r.currentProgramState.ProgramLinkStatus = status
}

func (r *NsTcProgramReconciler) getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus {
	return r.currentProgramState.ProgramLinkStatus
}

func (r *NsTcProgramReconciler) setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus) {
	r.currentLink.LinkStatus = status
}

func (r *NsTcProgramReconciler) getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus {
	return r.currentLink.LinkStatus
}

func (r *NsTcProgramReconciler) getNamespace() string {
	return r.namespace
}

func (r *NsTcProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {

	attachInfo := &gobpfman.TCAttachInfo{
		Priority:  r.currentLink.Priority,
		Iface:     r.currentLink.InterfaceName,
		Direction: directionToStr(r.currentLink.Direction),
		ProceedOn: tcProceedOnToInt(r.currentLink.ProceedOn),
		Metadata:  map[string]string{internal.UuidMetadataKey: string(r.currentLink.UUID)},
		Netns:     &r.currentLink.NetnsPath,
	}

	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcAttachInfo{
				TcAttachInfo: attachInfo,
			},
		},
	}
}

// updateLinks processes the *ProgramInfo and updates the list of links
// contained in *AttachInfoState.
func (r *NsTcProgramReconciler) updateLinks(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("TC updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all links in the node CRD to false.  We'll
	// update this in the next step for all links that are still
	// present.
	for i := range r.currentProgramState.TC.Links {
		r.currentProgramState.TC.Links[i].ShouldAttach = false
	}

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		return nil
	}

	if r.currentProgram.TC != nil && r.currentProgram.TC.Links != nil {
		for _, attachInfo := range r.currentProgram.TC.Links {
			expectedLinks, error := r.getExpectedLinks(ctx, attachInfo)
			if error != nil {
				return fmt.Errorf("failed to get node links: %v", error)
			}
			for _, link := range expectedLinks {
				index := r.findLink(link)
				if index != nil {
					// Link already exists, so set ShouldAttach to true.
					r.currentProgramState.TC.Links[*index].AttachInfoStateCommon.ShouldAttach = true
				} else {
					// Link doesn't exist, so add it.
					r.Logger.Info("Link doesn't exist.  Adding it.")
					r.currentProgramState.TC.Links = append(r.currentProgramState.TC.Links, link)
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

func (r *NsTcProgramReconciler) findLink(attachInfoState bpfmaniov1alpha1.TcAttachInfoState) *int {
	for i, a := range r.currentProgramState.TC.Links {
		// attachInfoState is the same as a if the the following fields are the
		// same: InterfaceName, Direction, Priority, NetnsPath, and ProceedOn.
		if a.InterfaceName == attachInfoState.InterfaceName && a.Direction == attachInfoState.Direction &&
			a.Priority == attachInfoState.Priority &&
			reflect.DeepEqual(a.NetnsPath, attachInfoState.NetnsPath) &&
			reflect.DeepEqual(a.ProceedOn, attachInfoState.ProceedOn) {
			return &i
		}
	}
	return nil
}

// processLinks calls reconcileBpfLink() for each link. It
// then updates the ProgramAttachStatus based on the updated status of each
// link.
func (r *NsTcProgramReconciler) processLinks(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.Name)

	// The following map is used to keep track of links that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// links.
	linksToRemove := make(map[int]bool)

	var lastReconcileLinkError error = nil
	for i := range r.currentProgramState.TC.Links {
		r.currentLink = &r.currentProgramState.TC.Links[i]
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
		r.currentProgramState.TC.Links = r.removeLinks(r.currentProgramState.TC.Links, linksToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileLinkError
}

func (r *NsTcProgramReconciler) updateProgramAttachStatus() {
	for _, link := range r.currentProgramState.TC.Links {
		if !isAttachSuccess(link.ShouldAttach, link.LinkStatus) {
			r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeLinks removes links from a slice of links based on the keys in the map.
func (r *NsTcProgramReconciler) removeLinks(links []bpfmaniov1alpha1.TcAttachInfoState, linksToRemove map[int]bool) []bpfmaniov1alpha1.TcAttachInfoState {
	var remainingLinks []bpfmaniov1alpha1.TcAttachInfoState
	for i, a := range links {
		if _, ok := linksToRemove[i]; !ok {
			remainingLinks = append(remainingLinks, a)
		}
	}
	return remainingLinks
}

// getExpectedLinks expands *AttachInfo into a list of specific attach
// points.
func (r *NsTcProgramReconciler) getExpectedLinks(ctx context.Context, attachInfo bpfmaniov1alpha1.TcAttachInfo,
) ([]bpfmaniov1alpha1.TcAttachInfoState, error) {
	interfaces, err := getInterfaces(&attachInfo.InterfaceSelector, r.ourNode, r.Interfaces)
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces for TcProgram: %v", err)
	}

	nodeLinks := []bpfmaniov1alpha1.TcAttachInfoState{}

	// See if there are any matching network namespaces on this node.
	containerInfo, err := r.Containers.GetContainers(
		ctx,
		r.getNamespace(),
		attachInfo.NetworkNamespaces.Pods,
		nil,
		r.Logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get container pids: %v", err)
	}

	if containerInfo != nil {
		// Just use one container per pod to get the pod's network
		// namespace.
		containerInfo = GetOneContainerPerPod(containerInfo)
		for _, container := range *containerInfo {
			netnsPath := netnsPathFromPID(container.pid)
			for _, iface := range interfaces {
				link := bpfmaniov1alpha1.TcAttachInfoState{
					AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
						ShouldAttach: true,
						UUID:         uuid.New().String(),
						LinkId:       nil,
						LinkStatus:   bpfmaniov1alpha1.ApAttachNotAttached,
					},
					InterfaceName: iface,
					NetnsPath:     netnsPath,
					Priority:      attachInfo.Priority,
					Direction:     attachInfo.Direction,
					ProceedOn:     attachInfo.ProceedOn,
				}
				nodeLinks = append(nodeLinks, link)
			}
		}
	}

	return nodeLinks, nil
}

func (r *NsTcProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.Name,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
