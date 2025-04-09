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

// NsXdpProgramReconciler contains the info required to reconcile an XdpNsProgram
type NsXdpProgramReconciler struct {
	ReconcilerCommon
	NsProgramReconcilerCommon
	currentLink *bpfmaniov1alpha1.XdpAttachInfoState
}

func (r *NsXdpProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *NsXdpProgramReconciler) getProgType() internal.ProgramType {
	return internal.Xdp
}

func (r *NsXdpProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_XDP
}

func (r *NsXdpProgramReconciler) getProgName() string {
	return r.currentProgram.Name
}

func (r *NsXdpProgramReconciler) shouldAttach() bool {
	return r.currentLink.ShouldAttach
}

func (r *NsXdpProgramReconciler) isAttached(ctx context.Context) bool {
	if r.currentProgramState.ProgramId == nil || r.currentLink.LinkId == nil {
		return false
	}
	return r.doesLinkExist(ctx, *r.currentProgramState.ProgramId, *r.currentLink.LinkId)
}

func (r *NsXdpProgramReconciler) getUUID() string {
	return r.currentLink.UUID
}

func (r *NsXdpProgramReconciler) getLinkId() *uint32 {
	return r.currentLink.LinkId
}

func (r *NsXdpProgramReconciler) setLinkId(id *uint32) {
	r.currentLink.LinkId = id
}

func (r *NsXdpProgramReconciler) setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus) {
	r.currentProgramState.ProgramLinkStatus = status
}

func (r *NsXdpProgramReconciler) getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus {
	return r.currentProgramState.ProgramLinkStatus
}

func (r *NsXdpProgramReconciler) setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus) {
	r.currentLink.LinkStatus = status
}

func (r *NsXdpProgramReconciler) getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus {
	return r.currentLink.LinkStatus
}

func (r *NsXdpProgramReconciler) getNamespace() string {
	return r.namespace
}

func (r *NsXdpProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {

	attachInfo := &gobpfman.XDPAttachInfo{
		Priority:  r.currentLink.Priority,
		Iface:     r.currentLink.InterfaceName,
		ProceedOn: xdpProceedOnToInt(r.currentLink.ProceedOn),
		Metadata:  map[string]string{internal.UuidMetadataKey: string(r.currentLink.UUID)},
		Netns:     &r.currentLink.NetnsPath,
	}

	return &gobpfman.AttachRequest{
		Id: *r.currentProgramState.ProgramId,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_XdpAttachInfo{
				XdpAttachInfo: attachInfo,
			},
		},
	}
}

// updateLinks processes the *ProgramInfo and updates the list of links
// contained in *AttachInfoState.
func (r *NsXdpProgramReconciler) updateLinks(ctx context.Context, isBeingDeleted bool) error {
	r.Logger.Info("XDP updateAttachInfo()", "isBeingDeleted", isBeingDeleted)

	// Set ShouldAttach for all links in the node CRD to false.  We'll
	// update this in the next step for all links that are still
	// present.
	for i := range r.currentProgramState.XDP.Links {
		r.currentProgramState.XDP.Links[i].ShouldAttach = false
	}

	if isBeingDeleted {
		// If the program is being deleted, we don't need to do anything else.
		return nil
	}

	if r.currentProgram.XDP != nil && r.currentProgram.XDP.Links != nil {
		for _, attachInfo := range r.currentProgram.XDP.Links {
			expectedLinks, error := r.getExpectedLinks(ctx, attachInfo)
			if error != nil {
				return fmt.Errorf("failed to get node links: %v", error)
			}
			for _, link := range expectedLinks {
				index, err := r.findLink(link)
				if err != nil {
					r.Logger.Info("Error", "Invalid link", r.printAttachInfo(link), "Error", err)
					continue
				}
				if index != nil {
					// Link already exists, so set ShouldAttach to true.
					r.currentProgramState.XDP.Links[*index].AttachInfoStateCommon.ShouldAttach = true
				} else {
					// Link doesn't exist, so add it.
					r.currentProgramState.XDP.Links = append(r.currentProgramState.XDP.Links, link)
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

func (r *NsXdpProgramReconciler) printAttachInfo(attachInfoState bpfmaniov1alpha1.XdpAttachInfoState) string {
	var netnsPath string
	if attachInfoState.NetnsPath == "" {
		netnsPath = "host"
	} else {
		netnsPath = attachInfoState.NetnsPath
	}

	return fmt.Sprintf("interfaceName: %s, netnsPath: %s, priority: %d",
		attachInfoState.InterfaceName, netnsPath, attachInfoState.Priority)
}

func (r *NsXdpProgramReconciler) findLink(attachInfoState bpfmaniov1alpha1.XdpAttachInfoState) (*int, error) {
	newNetnsId := getNetnsId(r.Logger, attachInfoState.NetnsPath)
	if newNetnsId == nil {
		return nil, fmt.Errorf("failed to get netnsId for path %s", attachInfoState.NetnsPath)
	}
	r.Logger.V(1).Info("findlink", "New Path", attachInfoState.NetnsPath, "NetnsId", newNetnsId)
	for i, a := range r.currentProgramState.XDP.Links {
		// attachInfoState is the same as a if the the following fields are the
		// same: InterfaceName, Priority, ProceedOn, and network namespace.
		if a.InterfaceName == attachInfoState.InterfaceName &&
			a.Priority == attachInfoState.Priority &&
			reflect.DeepEqual(a.ProceedOn, attachInfoState.ProceedOn) &&
			reflect.DeepEqual(getNetnsId(r.Logger, a.NetnsPath), newNetnsId) {
			return &i, nil
		}
	}
	return nil, nil
}

// processLinks calls reconcileBpfLink() for each link. It
// then updates the ProgramAttachStatus based on the updated status of each
// link.
func (r *NsXdpProgramReconciler) processLinks(ctx context.Context) error {
	r.Logger.Info("Processing attach info", "bpfFunctionName", r.currentProgram.Name)

	// The following map is used to keep track of links that need to be
	// removed.  If it's not empty at the end of the loop, we'll remove the
	// links.
	linksToRemove := make(map[int]bool)

	var lastReconcileLinkError error = nil
	for i := range r.currentProgramState.XDP.Links {
		r.currentLink = &r.currentProgramState.XDP.Links[i]
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
		r.currentProgramState.XDP.Links = r.removeLinks(r.currentProgramState.XDP.Links, linksToRemove)
	}

	r.updateProgramAttachStatus()

	return lastReconcileLinkError
}

func (r *NsXdpProgramReconciler) updateProgramAttachStatus() {
	for _, link := range r.currentProgramState.XDP.Links {
		if !isAttachSuccess(link.ShouldAttach, link.LinkStatus) {
			r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeLinks removes links from a slice of links based on the keys in the map.
func (r *NsXdpProgramReconciler) removeLinks(links []bpfmaniov1alpha1.XdpAttachInfoState, linksToRemove map[int]bool) []bpfmaniov1alpha1.XdpAttachInfoState {
	var remainingLinks []bpfmaniov1alpha1.XdpAttachInfoState
	for i, a := range links {
		if _, ok := linksToRemove[i]; !ok {
			remainingLinks = append(remainingLinks, a)
		}
	}
	return remainingLinks
}

// getExpectedLinks expands *AttachInfo into a list of specific attach
// points.
func (r *NsXdpProgramReconciler) getExpectedLinks(ctx context.Context, attachInfo bpfmaniov1alpha1.XdpAttachInfo,
) ([]bpfmaniov1alpha1.XdpAttachInfoState, error) {
	interfaces, err := getInterfaces(&attachInfo.InterfaceSelector, r.ourNode)
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces for XdpNsProgram: %v", err)
	}

	nodeLinks := []bpfmaniov1alpha1.XdpAttachInfoState{}

	// There is a network namespace selector, so see if there are any matching
	// pods on this node.
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
				link := bpfmaniov1alpha1.XdpAttachInfoState{
					AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
						ShouldAttach: true,
						UUID:         uuid.New().String(),
						LinkId:       nil,
						LinkStatus:   bpfmaniov1alpha1.ApAttachNotAttached,
					},
					InterfaceName: iface,
					NetnsPath:     netnsPath,
					Priority:      attachInfo.Priority,
					ProceedOn:     attachInfo.ProceedOn,
				}
				nodeLinks = append(nodeLinks, link)
			}
		}
	}

	return nodeLinks, nil
}

func (r *NsXdpProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.Name,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
