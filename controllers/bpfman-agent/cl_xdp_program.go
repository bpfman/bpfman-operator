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
	"github.com/bpfman/bpfman-operator/pkg/helpers"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/google/uuid"
)

// ClXdpProgramReconciler contains the info required to reconcile an XdpProgram
type ClXdpProgramReconciler struct {
	ReconcilerCommon
	ClProgramReconcilerCommon
	currentLink *bpfmaniov1alpha1.ClXdpAttachInfoState
}

func (r *ClXdpProgramReconciler) getProgId() *uint32 {
	return r.currentProgramState.ProgramId
}

func (r *ClXdpProgramReconciler) getProgType() internal.ProgramType {
	return internal.Xdp
}

func (r *ClXdpProgramReconciler) getBpfmanProgType() gobpfman.BpfmanProgramType {
	return gobpfman.BpfmanProgramType_XDP
}

func (r *ClXdpProgramReconciler) getProgName() string {
	return r.currentProgram.Name
}

func (r *ClXdpProgramReconciler) shouldAttach() bool {
	return r.currentLink.ShouldAttach
}

func (r *ClXdpProgramReconciler) isAttached(ctx context.Context) bool {
	if r.currentProgramState.ProgramId == nil || r.currentLink.LinkId == nil {
		return false
	}
	return r.doesLinkExist(ctx, *r.currentProgramState.ProgramId, *r.currentLink.LinkId)
}

func (r *ClXdpProgramReconciler) getUUID() string {
	return r.currentLink.UUID
}

func (r *ClXdpProgramReconciler) getLinkId() *uint32 {
	return r.currentLink.LinkId
}

func (r *ClXdpProgramReconciler) setLinkId(id *uint32) {
	r.currentLink.LinkId = id
}

func (r *ClXdpProgramReconciler) setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus) {
	r.currentProgramState.ProgramLinkStatus = status
}

func (r *ClXdpProgramReconciler) getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus {
	return r.currentProgramState.ProgramLinkStatus
}

func (r *ClXdpProgramReconciler) setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus) {
	r.currentLink.LinkStatus = status
}

func (r *ClXdpProgramReconciler) getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus {
	return r.currentLink.LinkStatus
}

// Must match with bpfman internal types
func xdpProceedOnToInt(proceedOn []bpfmaniov1alpha1.XdpProceedOnValue) []int32 {
	var out []int32

	for _, p := range proceedOn {
		switch p {
		case "Aborted":
			out = append(out, 0)
		case "Drop":
			out = append(out, 1)
		case "Pass":
			out = append(out, 2)
		case "TX":
			out = append(out, 3)
		case "ReDirect":
			out = append(out, 4)
		case "DispatcherReturn":
			out = append(out, 31)
		}
	}

	return out
}

func (r *ClXdpProgramReconciler) getAttachRequest() *gobpfman.AttachRequest {

	var netnsPath *string = nil
	if len(r.currentLink.NetnsPath) > 0 {
		netnsPath = &r.currentLink.NetnsPath
	}

	attachInfo := &gobpfman.XDPAttachInfo{
		Priority:  helpers.GetPriority(r.currentLink.Priority),
		Iface:     r.currentLink.InterfaceName,
		ProceedOn: xdpProceedOnToInt(r.currentLink.ProceedOn),
		Metadata:  map[string]string{internal.UuidMetadataKey: string(r.currentLink.UUID)},
		Netns:     netnsPath,
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
func (r *ClXdpProgramReconciler) updateLinks(ctx context.Context, isBeingDeleted bool) error {
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
			expectedLinks, err := r.getExpectedLinks(ctx, attachInfo)
			if err != nil {
				r.Logger.V(1).Info("updateLinks() failed", "error", err)
				return fmt.Errorf("failed to get node links: %v", err)
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
					r.Logger.Info("Link doesn't exist.  Adding it.")
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

func (r *ClXdpProgramReconciler) printAttachInfo(attachInfoState bpfmaniov1alpha1.ClXdpAttachInfoState) string {
	var netnsPath string
	if attachInfoState.NetnsPath == "" {
		netnsPath = "host"
	} else {
		netnsPath = attachInfoState.NetnsPath
	}

	return fmt.Sprintf("interfaceName: %s, netnsPath: %s, priority: %d",
		attachInfoState.InterfaceName, netnsPath, attachInfoState.Priority)
}

func (r *ClXdpProgramReconciler) findLink(attachInfoState bpfmaniov1alpha1.ClXdpAttachInfoState) (*int, error) {
	newNetnsId := r.NetNsCache.GetNetNsId(attachInfoState.NetnsPath)
	if newNetnsId == nil {
		return nil, fmt.Errorf("failed to get netnsId for path %s", attachInfoState.NetnsPath)
	}
	r.Logger.V(1).Info("findlink", "New Path", attachInfoState.NetnsPath, "NetnsId", newNetnsId)
	for i, a := range r.currentProgramState.XDP.Links {
		// attachInfoState is the same as a if the the following fields are the
		// same: InterfaceName, Priority, ProceedOn, and network namespace.
		if a.InterfaceName == attachInfoState.InterfaceName &&
			helpers.GetPriority(a.Priority) == helpers.GetPriority(attachInfoState.Priority) &&
			reflect.DeepEqual(a.ProceedOn, attachInfoState.ProceedOn) &&
			reflect.DeepEqual(r.NetNsCache.GetNetNsId(a.NetnsPath), newNetnsId) {
			return &i, nil
		}
	}
	return nil, nil
}

// processLinks calls reconcileBpfLink() for each link. It
// then updates the ProgramAttachStatus based on the updated status of each
// link.
func (r *ClXdpProgramReconciler) processLinks(ctx context.Context) error {
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

func (r *ClXdpProgramReconciler) updateProgramAttachStatus() {
	for _, link := range r.currentProgramState.XDP.Links {
		if !isAttachSuccess(link.ShouldAttach, link.LinkStatus) {
			r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachError)
			return
		}
	}
	r.setProgramLinkStatus(bpfmaniov1alpha1.ProgAttachSuccess)
}

// removeLinks removes links from a slice of links based on the keys in the map.
func (r *ClXdpProgramReconciler) removeLinks(links []bpfmaniov1alpha1.ClXdpAttachInfoState, linksToRemove map[int]bool) []bpfmaniov1alpha1.ClXdpAttachInfoState {
	var remainingLinks []bpfmaniov1alpha1.ClXdpAttachInfoState
	for i, a := range links {
		if _, ok := linksToRemove[i]; !ok {
			remainingLinks = append(remainingLinks, a)
		}
	}
	return remainingLinks
}

// getExpectedLinks expands *AttachInfo into a list of specific attach
// points.
func (r *ClXdpProgramReconciler) getExpectedLinks(ctx context.Context, attachInfo bpfmaniov1alpha1.ClXdpAttachInfo) ([]bpfmaniov1alpha1.ClXdpAttachInfoState, error) {
	nodeLinks := []bpfmaniov1alpha1.ClXdpAttachInfoState{}
	// Helper function to create a ClXdpAttachInfoState entry
	createLinkEntry := func(interfaceName, netnsPath string) bpfmaniov1alpha1.ClXdpAttachInfoState {
		return bpfmaniov1alpha1.ClXdpAttachInfoState{
			AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
				ShouldAttach: true,
				UUID:         uuid.New().String(),
				LinkId:       nil,
				LinkStatus:   bpfmaniov1alpha1.ApAttachNotAttached,
			},
			InterfaceName: interfaceName,
			NetnsPath:     netnsPath,
			Priority:      helpers.GetPriorityPointer(attachInfo.Priority),
			ProceedOn:     attachInfo.ProceedOn,
		}
	}

	// Handle interface discovery
	if isInterfacesDiscoveryEnabled(&attachInfo.InterfaceSelector) {
		discoveredInterfaces := getDiscoveredInterfaces(&attachInfo.InterfaceSelector, r.Interfaces)
		r.Logger.Info("getExpectedLinks", "num discoveredInterfaces", len(discoveredInterfaces))
		for _, intf := range discoveredInterfaces {
			nodeLinks = append(nodeLinks, createLinkEntry(intf.interfaceName, intf.netNSPath))
		}
		r.Logger.V(1).Info("getExpectedLinks-discovery", "Links created", len(nodeLinks))
		return nodeLinks, nil
	}

	// Fetch interfaces if discovery is disabled
	interfaces, err := getInterfaces(&attachInfo.InterfaceSelector, r.ourNode)
	if err != nil {
		r.Logger.V(1).Info("getExpectedLinks failed to get interfaces", "error", err)
		return nil, fmt.Errorf("failed to get interfaces for XdpProgram: %w", err)
	}

	r.Logger.Info("getExpectedLinks", "Number of interfaces", len(interfaces))

	// Handle network namespaces if provided
	if attachInfo.NetworkNamespaces != nil {
		containerInfo, err := r.Containers.GetContainers(
			ctx,
			attachInfo.NetworkNamespaces.Namespace,
			attachInfo.NetworkNamespaces.Pods,
			nil,
			r.Logger,
		)
		if err != nil {
			r.Logger.V(1).Info("getExpectedLinks failed to get container pids", "error", err)
			return nil, fmt.Errorf("failed to get container pids: %w", err)
		}

		if containerInfo == nil {
			r.Logger.Info("NetworkNamespaces is configured but no matching container found")
			return nodeLinks, nil
		}

		containerInfo = GetOneContainerPerPod(containerInfo)
		for _, container := range *containerInfo {
			netnsPath := netnsPathFromPID(container.pid)
			for _, iface := range interfaces {
				nodeLinks = append(nodeLinks, createLinkEntry(iface, netnsPath))
			}
		}
		r.Logger.V(1).Info("getExpectedLinks", "Links created", len(nodeLinks))
		return nodeLinks, nil
	}

	// Fallback: Assign interfaces without a namespace
	for _, iface := range interfaces {
		nodeLinks = append(nodeLinks, createLinkEntry(iface, ""))
	}

	r.Logger.V(1).Info("getExpectedLinks", "Links created", len(nodeLinks))
	return nodeLinks, nil
}

func (r *ClXdpProgramReconciler) getProgramLoadInfo() *gobpfman.LoadInfo {
	return &gobpfman.LoadInfo{
		Name:        r.currentProgram.Name,
		ProgramType: r.getBpfmanProgType(),
		Info:        nil,
	}
}
