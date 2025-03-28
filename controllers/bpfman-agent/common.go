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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/vishvananda/netns"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	"github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/netobserv/netobserv-ebpf-agent/pkg/ifaces"
	"google.golang.org/grpc"
)

// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates/finalizers,verbs=update
// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get

const (
	retryDurationAgent = 1 * time.Second
)

type ReconcilerCommon struct {
	client.Client
	Scheme       *runtime.Scheme
	GrpcConn     *grpc.ClientConn
	BpfmanClient gobpfman.BpfmanClient
	Logger       logr.Logger
	NodeName     string
	finalizer    string
	recType      string
	Containers   ContainerGetter
	ourNode      *v1.Node
	Interfaces   *sync.Map
}

// ApplicationReconciler is an interface that defines the methods needed to
// reconcile a BpfApplication.
type ApplicationReconciler interface {
	// SetupWithManager registers the reconciler with the manager and defines
	// which kubernetes events will trigger a reconcile.
	SetupWithManager(mgr ctrl.Manager) error
	// Reconcile is the main entry point to the reconciler. It will be called by
	// the controller runtime when something happens that the reconciler is
	// interested in. When Reconcile is invoked, it initializes some state in
	// the given bpfmanReconciler, retrieves a list of all programs of the given
	// type, and then calls reconcileCommon.
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)

	getAppStateName() string
	getNode() *v1.Node
	getNodeSelector() *metav1.LabelSelector
	GetStatus() *bpfmaniov1alpha1.BpfAppStatus
	isBeingDeleted() bool
	updateBpfAppStatus(ctx context.Context, condition metav1.Condition) error
	updateLoadStatus(updateStatus bpfmaniov1alpha1.AppLoadStatus)
	validateProgramList() error
	load(ctx context.Context) error
	isLoaded(ctx context.Context) bool
	getLoadRequest() (*gobpfman.LoadRequest, error)
	unload(ctx context.Context)
}

// ProgramReconciler is an interface that defines the methods needed to
// reconcile a program contained in a BpfApplication.
type ProgramReconciler interface {
	getAttachRequest() *gobpfman.AttachRequest
	getProgId() *uint32
	getProgType() internal.ProgramType
	getBpfmanProgType() gobpfman.BpfmanProgramType
	getProgName() string
	updateLinks(ctx context.Context, isBeingDeleted bool) error
	processLinks(ctx context.Context) error
	shouldAttach() bool
	isAttached(ctx context.Context) bool
	getUUID() string
	setLinkId(id *uint32)
	getLinkId() *uint32
	setProgramLinkStatus(status bpfmaniov1alpha1.ProgramLinkStatus)
	getProgramLinkStatus() bpfmaniov1alpha1.ProgramLinkStatus
	setCurrentLinkStatus(status bpfmaniov1alpha1.LinkStatus)
	getCurrentLinkStatus() bpfmaniov1alpha1.LinkStatus
	reconcileProgram(ctx context.Context, program ProgramReconciler, isBeingDeleted bool) error
	getProgramLoadInfo() *gobpfman.LoadInfo
}

// Load or unload the programs as appropriate.
func (r *ReconcilerCommon) reconcileLoad(ctx context.Context, rec ApplicationReconciler) error {
	isNodeSelected, err := isNodeSelected(rec.getNodeSelector(), rec.getNode().Labels)
	if err != nil {
		return fmt.Errorf("check if node is selected failed: %v", err)
	}

	if !isNodeSelected {
		// The program should not be loaded.  Unload it if necessary
		rec.unload(ctx)
		rec.updateLoadStatus(bpfmaniov1alpha1.NotSelected)
	} else if rec.isBeingDeleted() {
		// The program should not be loaded.  Unload it if necessary
		rec.unload(ctx)
		rec.updateLoadStatus(bpfmaniov1alpha1.AppUnLoadSuccess)
	} else {
		err := rec.validateProgramList()
		if err != nil {
			rec.updateLoadStatus(bpfmaniov1alpha1.ProgListChangedError)
			return err
		}
		if rec.isLoaded(ctx) {
			rec.updateLoadStatus(bpfmaniov1alpha1.AppLoadSuccess)
		} else {
			err := rec.load(ctx)
			if err != nil {
				rec.updateLoadStatus(bpfmaniov1alpha1.AppLoadError)
				return fmt.Errorf("failed to load program: %v", err)
			} else {
				rec.updateLoadStatus(bpfmaniov1alpha1.AppLoadSuccess)
			}
		}
	}

	return nil
}

// updateStatus updates the status of a BpfApplicationState object if needed,
// returning true if the status was changed, and false if the status was not
// changed.
func (r *ReconcilerCommon) updateStatus(
	ctx context.Context,
	rec ApplicationReconciler,
	condition bpfmaniov1alpha1.BpfApplicationStateConditionType,
) (bool, error) {
	status := rec.GetStatus()
	r.Logger.V(1).Info("updateStatus()", "existing conds", status.Conditions, "new cond", condition)

	if status.Conditions != nil {
		numConditions := len(status.Conditions)

		if numConditions == 1 {
			if status.Conditions[0].Type == string(condition) {
				// No change, so just return false -- not updated
				return false, nil
			} else {
				// We're changing the condition, so delete this one.  The
				// new condition will be added below.
				status.Conditions = nil
			}
		} else if numConditions > 1 {
			// We should only ever have one condition, so we shouldn't hit this
			// case.  However, if we do, log a message, delete the existing
			// conditions, and add the new one below.
			r.Logger.Info("more than one condition detected", "numConditions", numConditions)
			status.Conditions = nil
		}
		// if numConditions == 0, just add the new condition below.
	}

	r.Logger.Info("Calling KubeAPI to update BpfAppState condition", "Name", rec.getAppStateName, "condition", condition.Condition().Type)
	err := rec.updateBpfAppStatus(ctx, condition.Condition())
	if err != nil {
		r.Logger.Info("failed to set BpfApplication object status", "reason", err)
	}

	r.Logger.V(1).Info("condition updated", "new condition", condition, "existing conds", status.Conditions)
	return true, err
}

// reconcileProgram is a common function for reconciling programs contained in a
// BpfApplication. It is called by the BpfApplication reconciler for each
// program.  reconcileProgram updates the program's attach status when it's
// done.
func (r *ReconcilerCommon) reconcileProgram(ctx context.Context, program ProgramReconciler, isBeingDeleted bool) error {
	err := program.updateLinks(ctx, isBeingDeleted)
	if err != nil {
		program.setProgramLinkStatus(bpfmaniov1alpha1.UpdateAttachInfoError)
		return err
	}

	return program.processLinks(ctx)
}

// get Clientset returns a kubernetes clientset.
func getClientset() (*kubernetes.Clientset, error) {

	// get the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting config: %v", err)
	}
	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating clientset: %v", err)
	}

	return clientset, nil
}

func interfaceInExcludeList(interfaceSelector *bpfmaniov1alpha1.InterfaceSelector, intf string) bool {
	for _, i := range interfaceSelector.InterfacesDiscoveryConfig.ExcludeInterfaces {
		if i == intf {
			return true
		}
	}
	return false
}

func setupAllowedInterfacesLists(interfaceSelector *bpfmaniov1alpha1.InterfaceSelector) ([]*regexp.Regexp, []string) {
	var isRegexp = regexp.MustCompile("^/(.*)/$")
	var allowedRegexpes []*regexp.Regexp
	var allowedMatches []string

	for _, definition := range interfaceSelector.InterfacesDiscoveryConfig.AllowedInterfaces {
		definition = strings.Trim(definition, " ")
		// the user defined a /regexp/ between slashes: compile and store it as regular expression
		if sm := isRegexp.FindStringSubmatch(definition); len(sm) > 1 {
			re, err := regexp.Compile(sm[1])
			if err != nil {
				return allowedRegexpes, allowedMatches
			}
			allowedRegexpes = append(allowedRegexpes, re)
		} else {
			// otherwise, store it as exact match definition
			allowedMatches = append(allowedMatches, definition)
		}
	}
	return allowedRegexpes, allowedMatches
}

func interfaceInAllowedList(intf string, allowedRegexpes []*regexp.Regexp, allowedMatches []string) bool {
	if len(allowedRegexpes) == 0 && len(allowedMatches) == 0 {
		return true
	}

	for _, re := range allowedRegexpes {
		if re.MatchString(intf) {
			return true
		}
	}

	for _, n := range allowedMatches {
		if n == intf {
			return true
		}
	}
	return false
}

func getInterfaces(interfaceSelector *bpfmaniov1alpha1.InterfaceSelector, ourNode *v1.Node, discoveredInterfaces *sync.Map) ([]string, error) {
	var interfaces []string

	if isInterfacesDiscoveryEnabled(interfaceSelector) {
		allowedRegexpes, allowedMatches := setupAllowedInterfacesLists(interfaceSelector)
		discoveredInterfaces.Range(func(key, value any) bool {
			if value.(bool) {
				intf := key.(ifaces.Interface)
				if !interfaceInExcludeList(interfaceSelector, intf.Name) && interfaceInAllowedList(intf.Name, allowedRegexpes, allowedMatches) {
					interfaces = append(interfaces, intf.Name)
				}
			}
			return true
		})
		if len(interfaces) > 0 {
			return interfaces, nil
		}
		return nil, fmt.Errorf("interfaces discovery is enabled but no interface discovered")
	}

	if len(interfaceSelector.Interfaces) > 0 {
		return interfaceSelector.Interfaces, nil
	}

	if interfaceSelector.PrimaryNodeInterface != nil {
		nodeIface, err := bpfmanagentinternal.GetPrimaryNodeInterface(ourNode)
		if err != nil {
			return nil, err
		}

		interfaces = append(interfaces, nodeIface)
		return interfaces, nil
	}

	return nil, fmt.Errorf("no interfaces selected")
}

// Only return node updates for our node (all events)
func nodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetLabels()["kubernetes.io/hostname"] == nodeName
		},
	}
}

// Predicate to watch for Pod events on a specific node which checks if the
// event's Pod is scheduled on the given node.
func podOnNodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			pod, ok := e.Object.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			pod, ok := e.Object.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName && pod.Status.Phase == v1.PodRunning
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			pod, ok := e.ObjectNew.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName && pod.Status.Phase == v1.PodRunning
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			pod, ok := e.Object.(*v1.Pod)
			return ok && pod.Spec.NodeName == nodeName
		},
	}
}

func generateUniqueName(baseName string) string {
	uuid := uuid.New().String()
	return fmt.Sprintf("%s-%s", baseName, uuid[:8])
}

// TODO: Need to re-work map owner logic for load/attach split
// See: https://github.com/bpfman/bpfman-operator/issues/393
//
// // MapOwnerParamStatus provides the output from a MapOwerSelector being parsed.
// type MapOwnerParamStatus struct {
// 	isSet      bool
// 	isFound    bool
// 	isLoaded   bool
// 	mapOwnerId *uint32
// }

// // This function parses the MapOwnerSelector Label Selector field from the
// // BpfApplication Object. The labels should map to a BpfApplication Object that
// // this BpfApplication wants to share maps with. If found, this function returns
// // the ID of the BpfApplication that owns the map on this node. Found or not,
// // this function also returns some flags (isSet, isFound, isLoaded) to help with
// // the processing and setting of the proper condition on the BpfApplication
// // Object.
// func (r *ReconcilerCommon) processMapOwnerParam(ctx context.Context, rec ProgramReconciler) (*MapOwnerParamStatus, error) {
// 	mapOwnerStatus := &MapOwnerParamStatus{
// 		isSet:      false,
// 		isFound:    false,
// 		isLoaded:   false,
// 		mapOwnerId: nil,
// 	}

// 	r.Logger.V(1).Info("processMapOwnerParam()", "ctx", ctx, "rec.progId", rec.getProgId(), "MapOwnerStatus", mapOwnerStatus)

// 	return mapOwnerStatus, nil

// 	// Parse the MapOwnerSelector label selector.
// 	mapOwnerSelectorMap, err := metav1.LabelSelectorAsMap(rec.appCommon.MapOwnerSelector)
// 	if err != nil {
// 		mapOwnerStatus.isSet = true
// 		return mapOwnerStatus, fmt.Errorf("failed to parse MapOwnerSelector: %v", err)
// 	}

// 	// If no data was entered, just return with default values, all flags set to false.
// 	if len(mapOwnerSelectorMap) == 0 {
// 		return mapOwnerStatus, nil
// 	} else {
// 		mapOwnerStatus.isSet = true

// 		// Add the labels from the MapOwnerSelector to a map and add an additional
// 		// label to filter on just this node. Call K8s to find all the eBPF programs
// 		// that match this filter.
// 		labelMap := client.MatchingLabels{internal.K8sHostLabel: r.NodeName}
// 		for key, value := range mapOwnerSelectorMap {
// 			labelMap[key] = value
// 		}
// 		opts := []client.ListOption{labelMap}
// 		r.Logger.V(1).Info("MapOwner Labels:", "opts", opts)
// 		bpfProgramList, err := rec.getBpfList(ctx, opts)
// 		if err != nil {
// 			return mapOwnerStatus, err
// 		}

// 		// If no BpfProgram Objects were found, or more than one, then return.
// 		items := (*bpfProgramList).GetItems()
// 		if len(items) == 0 {
// 			return mapOwnerStatus, nil
// 		} else if len(items) > 1 {
// 			return mapOwnerStatus, fmt.Errorf("MapOwnerSelector resolved to multiple BpfProgram Objects")
// 		} else {
// 			mapOwnerStatus.isFound = true

// 			// Get bpfProgram based on UID meta
// 			prog, err := bpfmanagentinternal.GetBpfmanProgram(ctx, r.BpfmanClient, items[0].GetUID())
// 			if err != nil {
// 				return nil, fmt.Errorf("failed to get bpfman program for BpfProgram with UID %s: %v", items[0].GetUID(), err)
// 			}

// 			kernelInfo := prog.GetKernelInfo()
// 			if kernelInfo == nil {
// 				return nil, fmt.Errorf("failed to process bpfman program for BpfProgram with UID %s: %v", items[0].GetUID(), err)
// 			}
// 			mapOwnerStatus.mapOwnerId = &kernelInfo.Id

// 			// Get most recent condition from the one eBPF Program and determine
// 			// if the BpfProgram is loaded or not.
// 			conLen := len(items[0].GetStatus().Conditions)
// 			if conLen > 0 &&
// 				items[0].GetStatus().Conditions[conLen-1].Type ==
// 					string(bpfmaniov1alpha1.BpfAppStateCondLoaded) {
// 				mapOwnerStatus.isLoaded = true
// 			}

// 			return mapOwnerStatus, nil
// 		}
// 	}
// }

func isNodeSelected(selector *metav1.LabelSelector, nodeLabels map[string]string) (bool, error) {
	// Logic to check if this node is selected by the BpfApplication object
	selectorTool, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("failed to parse nodeSelector: %v",
			err)
	}

	nodeLabelSet, err := labels.ConvertSelectorToLabelsMap(labels.FormatLabels(nodeLabels))
	if err != nil {
		return false, fmt.Errorf("failed to parse node labels : %v",
			err)
	}

	return selectorTool.Matches(nodeLabelSet), nil
}

// reconcileBpfLink reconciles the bpfman state for a single link.  It may
// update attachInfo.AttachInfoCommon.Status.  It returns a boolean value
// indicating whether the link is no longer needed and should be removed from
// the list.
func (r *ReconcilerCommon) reconcileBpfLink(ctx context.Context, rec ProgramReconciler) (bool, error) {
	isAttached := rec.isAttached(ctx)
	shouldAttach := rec.shouldAttach()

	r.Logger.V(1).Info("reconcileBpfLink()", "shouldAttached", shouldAttach, "isAttached", isAttached, "Attach Status", rec.getCurrentLinkStatus())

	switch shouldAttach {
	case true:
		switch isAttached {
		case true:
			// The link is attached and it should be attached.
			// Link exists and bpfProgram K8s Object is up to date
			r.Logger.V(1).Info("Program link is in correct state.  Nothing to do in bpfman")
			rec.setCurrentLinkStatus(bpfmaniov1alpha1.ApAttachAttached)
		case false:
			// The link should be attached, but it isn't.
			r.Logger.V(1).Info("Program is not attached, calling getAttachRequest()")
			attachRequest := rec.getAttachRequest()
			r.Logger.V(1).Info("AttachRequest", "attachRequest", attachRequest)
			r.Logger.Info("Calling bpfman to attach eBPF Program on node")
			linkId, err := bpfmanagentinternal.AttachBpfmanProgram(ctx, r.BpfmanClient, attachRequest)
			if err != nil {
				r.Logger.Error(err, "Failed to attach eBPF Program")
				rec.setCurrentLinkStatus(bpfmaniov1alpha1.ApAttachError)
			} else {
				r.Logger.Info("Successfully attached eBPF Program", "Link ID", linkId)
				rec.setLinkId(linkId)
				rec.setCurrentLinkStatus(bpfmaniov1alpha1.ApAttachAttached)
			}
		}
	case false:
		switch isAttached {
		case true:
			// The program is attached but it shouldn't be attached.  Detach it.
			r.Logger.Info("Calling bpfman to detach eBPF Program", "Link ID", rec.getLinkId())
			if err := bpfmanagentinternal.DetachBpfmanProgram(ctx, r.BpfmanClient, *rec.getLinkId()); err != nil {
				r.Logger.Error(err, "Failed to detach eBPF Program")
				rec.setCurrentLinkStatus(bpfmaniov1alpha1.ApDetachError)
			} else {
				r.Logger.Info("Successfully detached eBPF Program")
				rec.setLinkId(nil)
				rec.setCurrentLinkStatus(bpfmaniov1alpha1.ApAttachNotAttached)
			}
		case false:
			// The program shouldn't be attached and it isn't.
			rec.setCurrentLinkStatus(bpfmaniov1alpha1.ApAttachNotAttached)
		}
	}

	// The BPF program was successfully reconciled.
	remove := !shouldAttach && rec.getCurrentLinkStatus() == bpfmaniov1alpha1.ApAttachNotAttached
	return remove, nil
}

func isAttachSuccess(shouldAttach bool, status bpfmaniov1alpha1.LinkStatus) bool {
	if shouldAttach && status == bpfmaniov1alpha1.ApAttachAttached {
		return true
	} else if !shouldAttach && status == bpfmaniov1alpha1.ApAttachNotAttached {
		return true
	} else {
		return false
	}
}

// removeFinalizer removes the finalizer from the object if is applied,
// returning if the action resulted in a kube API update or not along with any
// errors.
func (r *ReconcilerCommon) removeFinalizer(ctx context.Context, o client.Object, finalizer string) bool {
	changed := controllerutil.RemoveFinalizer(o, finalizer)
	if changed {
		r.Logger.Info("Calling KubeAPI to remove finalizer from BpfApplication", "object name", o.GetName())
		err := r.Update(ctx, o)
		if err != nil {
			r.Logger.Error(err, "failed to remove BpfApplication Finalizer")
			return true
		}
	}

	return changed
}

func (r *ReconcilerCommon) doesLinkExist(ctx context.Context, programId uint32, linkId uint32) bool {
	program, err := bpfmanagentinternal.GetBpfmanProgramById(ctx, r.BpfmanClient, programId)
	if err != nil {
		return false
	}
	for _, progLink := range program.Info.Links {
		if progLink == linkId {
			return true
		}
	}
	return false
}

func directionToStr(direction bpfmaniov1alpha1.TCDirectionType) string {
	switch direction {
	case bpfmaniov1alpha1.TCIngress:
		return "ingress"
	case bpfmaniov1alpha1.TCEgress:
		return "egress"
	}
	return ""
}

func netnsPathFromPID(pid int32) string {
	return fmt.Sprintf("/host/proc/%d/ns/net", pid)
}

func getInterfaceNetNsList(interfaceSelector *bpfmaniov1alpha1.InterfaceSelector, ifName string, discoveredInterfaces *sync.Map) map[string][]string {
	netnsList := make(map[string][]string)
	if isInterfacesDiscoveryEnabled(interfaceSelector) {
		discoveredInterfaces.Range(func(key, value any) bool {
			if value.(bool) {
				intf := key.(ifaces.Interface)
				if intf.Name == ifName && intf.NetNS != netns.None() {
					netnsList[ifName] = append(netnsList[ifName], internal.NetNsPath+intf.NetNS.String())
				}
			}
			return true
		})
	}
	return netnsList
}

func isInterfacesDiscoveryEnabled(interfaceSelector *bpfmaniov1alpha1.InterfaceSelector) bool {
	if interfaceSelector.InterfacesDiscoveryConfig != nil && interfaceSelector.InterfacesDiscoveryConfig.InterfaceAutoDiscovery != nil &&
		*interfaceSelector.InterfacesDiscoveryConfig.InterfaceAutoDiscovery {
		return true
	}
	return false
}
