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

package appagent

import (
	"context"
	"fmt"
	"reflect"
	"time"

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
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/app-agent/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates/finalizers,verbs=update
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get

const (
	retryDurationAgent = 5 * time.Second
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
}

type ProgramReconcilerCommon struct {
	// ANF-TODO: appCommon is needed to load the program. It won't be needed
	// after the load/attch split is ready.
	appCommon           bpfmaniov1alpha1.BpfAppCommon
	currentProgram      *bpfmaniov1alpha1.BpfApplicationProgram
	currentProgramState *bpfmaniov1alpha1.BpfApplicationProgramState
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
	updateLoadStatus(newCondition bpfmaniov1alpha1.BpfProgramConditionType)
}

// ProgramReconciler is an interface that defines the methods needed to
// reconcile a program contained in a BpfApplication.
type ProgramReconciler interface {
	getLoadRequest(mapOwnerId *uint32) (*gobpfman.LoadRequest, error)
	getProgId() *uint32
	updateAttachInfo(ctx context.Context, isBeingDeleted bool) error
	processAttachInfo(ctx context.Context, mapOwnerStatus *MapOwnerParamStatus) error
	shouldAttach() bool
	getUUID() string
	getAttachId() *uint32
	setAttachId(id *uint32)
	setAttachStatus(status bpfmaniov1alpha1.BpfProgramConditionType)
	getAttachStatus() bpfmaniov1alpha1.BpfProgramConditionType
}

// ANF-TODO: When we have the load/attach split, this is the function that will
// load or unload the program defined in the BpfApplication.  The load will
// return a list of program IDs which will be saved in the node program list.
// For now, just do the following: Update flag saying program should be loaded
// and is loaded. Also, create node program list on the first load.
func (r *ReconcilerCommon) reconcileLoad(rec ApplicationReconciler) error {
	isNodeSelected, err := isNodeSelected(rec.getNodeSelector(), rec.getNode().Labels)
	if err != nil {
		return fmt.Errorf("failed to check if node is selected: %v", err)
	}

	// ANF-TODO: When we have the load/attach split, this is where we
	// will load/unload the program as necessary.
	if !isNodeSelected {
		// The program should not be loaded
		rec.updateLoadStatus(bpfmaniov1alpha1.BpfProgCondNotSelected)
	} else if rec.isBeingDeleted() {
		// The program should be unloaded if necessary
		rec.updateLoadStatus(bpfmaniov1alpha1.BpfProgCondUnloaded)
	} else {
		// The program should be loaded, but for now, just set the condition
		rec.updateLoadStatus(bpfmaniov1alpha1.BpfProgCondLoaded)
	}

	return nil
}

// ANF-TODO: need better names for differentiating between just updating a
// simple status value and updating a proper Condition.  Or, maybe we can just
// set the values direclty.
//
// updateSimpleStatus updates the value of a BpfProgramConditionType. It returns
// a boolean indicating whether the condition was changed.
func updateSimpleStatus(existingCondition *bpfmaniov1alpha1.BpfProgramConditionType,
	newCondition bpfmaniov1alpha1.BpfProgramConditionType) bool {
	changed := false
	if *existingCondition != newCondition {
		*existingCondition = newCondition
		changed = true
	}
	return changed
}

// updateStatus updates the status of a BpfApplication object if needed,
// returning true if the status was changed, and false if the status was not
// changed.
func (r *ReconcilerCommon) updateStatus(
	ctx context.Context,
	rec ApplicationReconciler,
	condition bpfmaniov1alpha1.ProgramConditionType,
) bool {
	status := rec.GetStatus()
	r.Logger.V(1).Info("updateStatus()", "existing conds", status.Conditions, "new cond", condition)

	if status.Conditions != nil {
		numConditions := len(status.Conditions)

		if numConditions == 1 {
			if status.Conditions[0].Type == string(condition) {
				// No change, so just return false -- not updated
				return false
			} else {
				// We're changing the condition, so delete this one.  The
				// new condition will be added below.
				status.Conditions = nil
			}
		} else if numConditions > 1 {
			// We should only ever have one condition, so we shouldn't hit this
			// case.  However, if we do, log a message, delete the existing
			// conditions, and add the new one below.
			r.Logger.Info("more than one BpfProgramCondition", "numConditions", numConditions)
			status.Conditions = nil
		}
		// if numConditions == 0, just add the new condition below.
	}

	r.Logger.Info("Calling KubeAPI to update BpfAppState condition", "Name", rec.getAppStateName, "condition", condition.Condition("").Type)
	if err := rec.updateBpfAppStatus(ctx, condition.Condition("")); err != nil {
		r.Logger.Error(err, "failed to set BpfProgram object status")
	}

	r.Logger.V(1).Info("condition updated", "new condition", condition, "existing conds", status.Conditions)
	return true
}

// reconcileProgram is a common function for reconciling programs contained in a
// BpfApplication. It is called by the BpfApplication reconciler for each
// program.  It returns a boolean indicating whether anything was changed.
// ANF-TODO: For now, it just supports XDP, but that will be generalized in the
// future.
func (r *ReconcilerCommon) reconcileProgram(ctx context.Context, program ProgramReconciler, isBeingDeleted bool) error {

	mapOwnerStatus, err := r.processMapOwnerParam(ctx, program)
	if err != nil {
		return err
	}

	err = program.updateAttachInfo(ctx, isBeingDeleted)
	if err != nil {
		return err
	}

	err = program.processAttachInfo(ctx, mapOwnerStatus)
	if err != nil {
		return err
	}

	return nil
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

func getInterfaces(interfaceSelector *bpfmaniov1alpha1.InterfaceSelector, ourNode *v1.Node) ([]string, error) {
	var interfaces []string

	if interfaceSelector.Interfaces != nil {
		return *interfaceSelector.Interfaces, nil
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

// MapOwnerParamStatus provides the output from a MapOwerSelector being parsed.
type MapOwnerParamStatus struct {
	isSet      bool
	isFound    bool
	isLoaded   bool
	mapOwnerId *uint32
}

// This function parses the MapOwnerSelector Labor Selector field from the
// BpfProgramCommon struct in the *Program Objects. The labels should map to
// a BpfProgram Object that this *Program wants to share maps with. If found, this
// function returns the ID of the BpfProgram that owns the map on this node.
// Found or not, this function also returns some flags (isSet, isFound, isLoaded)
// to help with the processing and setting of the proper condition on the BpfProgram Object.
func (r *ReconcilerCommon) processMapOwnerParam(ctx context.Context, rec ProgramReconciler) (*MapOwnerParamStatus, error) {
	mapOwnerStatus := &MapOwnerParamStatus{
		isSet:      false,
		isFound:    false,
		isLoaded:   false,
		mapOwnerId: nil,
	}

	r.Logger.V(1).Info("processMapOwnerParam()", "ctx", ctx, "rec.progId", rec.getProgId(), "MapOwnerStatus", mapOwnerStatus)

	// ANF-TODO: Need to update this to support BpfApplicationState.
	return mapOwnerStatus, nil

	// // Parse the MapOwnerSelector label selector.
	// mapOwnerSelectorMap, err := metav1.LabelSelectorAsMap(rec.appCommon.MapOwnerSelector)
	// if err != nil {
	// 	mapOwnerStatus.isSet = true
	// 	return mapOwnerStatus, fmt.Errorf("failed to parse MapOwnerSelector: %v", err)
	// }

	// // If no data was entered, just return with default values, all flags set to false.
	// if len(mapOwnerSelectorMap) == 0 {
	// 	return mapOwnerStatus, nil
	// } else {
	// 	mapOwnerStatus.isSet = true

	// 	// Add the labels from the MapOwnerSelector to a map and add an additional
	// 	// label to filter on just this node. Call K8s to find all the eBPF programs
	// 	// that match this filter.
	// 	labelMap := client.MatchingLabels{internal.K8sHostLabel: r.NodeName}
	// 	for key, value := range mapOwnerSelectorMap {
	// 		labelMap[key] = value
	// 	}
	// 	opts := []client.ListOption{labelMap}
	// 	r.Logger.V(1).Info("MapOwner Labels:", "opts", opts)
	// 	bpfProgramList, err := rec.getBpfList(ctx, opts)
	// 	if err != nil {
	// 		return mapOwnerStatus, err
	// 	}

	// 	// If no BpfProgram Objects were found, or more than one, then return.
	// 	items := (*bpfProgramList).GetItems()
	// 	if len(items) == 0 {
	// 		return mapOwnerStatus, nil
	// 	} else if len(items) > 1 {
	// 		return mapOwnerStatus, fmt.Errorf("MapOwnerSelector resolved to multiple BpfProgram Objects")
	// 	} else {
	// 		mapOwnerStatus.isFound = true

	// 		// Get bpfProgram based on UID meta
	// 		prog, err := bpfmanagentinternal.GetBpfmanProgram(ctx, r.BpfmanClient, items[0].GetUID())
	// 		if err != nil {
	// 			return nil, fmt.Errorf("failed to get bpfman program for BpfProgram with UID %s: %v", items[0].GetUID(), err)
	// 		}

	// 		kernelInfo := prog.GetKernelInfo()
	// 		if kernelInfo == nil {
	// 			return nil, fmt.Errorf("failed to process bpfman program for BpfProgram with UID %s: %v", items[0].GetUID(), err)
	// 		}
	// 		mapOwnerStatus.mapOwnerId = &kernelInfo.Id

	// 		// Get most recent condition from the one eBPF Program and determine
	// 		// if the BpfProgram is loaded or not.
	// 		conLen := len(items[0].GetStatus().Conditions)
	// 		if conLen > 0 &&
	// 			items[0].GetStatus().Conditions[conLen-1].Type ==
	// 				string(bpfmaniov1alpha1.BpfProgCondLoaded) {
	// 			mapOwnerStatus.isLoaded = true
	// 		}

	// 		return mapOwnerStatus, nil
	// 	}
	// }
}

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

// reconcileBpfmanAttachment reconciles the bpfman state for a single
// attachment.  It may update attachInfo.AttachInfoCommon.Status and/or
// attachInfo.AttachId.  It returns a boolean value indicating whether the
// attachment is no longer needed and should be removed from the list.
//
// ANF-TODO: This function needs to be generalized to handle all program types.
//
// ANF-TODO: When we have load/attach split, this function will just do the
// attach. However, for now, it does the load and attach.
func (r *ReconcilerCommon) reconcileBpfAttachment(
	ctx context.Context,
	rec ProgramReconciler,
	// attachInfo *bpfmaniov1alpha1.XdpAttachInfoState,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	// bpfFunctionName string,
	mapOwnerStatus *MapOwnerParamStatus) (bool, error) {

	loadedBpfProgram, isAttached := loadedBpfPrograms[string(rec.getUUID())]

	r.Logger.V(1).Info("reconcileBpfAttachment()", "shouldAttached", rec.shouldAttach(), "isAttached", isAttached)

	switch rec.shouldAttach() {
	case true:
		switch isAttached {
		case true:
			// The program is attached and it should be attached.
			// prog ID should already have been set if program is attached
			if !reflect.DeepEqual(rec.getAttachId(), &loadedBpfProgram.KernelInfo.Id) {
				// This shouldn't happen, but if it does, log a message and update AttachId.
				r.Logger.V(1).Info("ID mismatch. Updating", "Saved ID", rec.getAttachId(),
					"Kernel ID", loadedBpfProgram.KernelInfo.Id)
				rec.setAttachId(&loadedBpfProgram.KernelInfo.Id)
			}
			// Confirm it's in the correct state.
			loadRequest, err := rec.getLoadRequest(mapOwnerStatus.mapOwnerId)
			if err != nil {
				rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError)
				return false, err
			}
			isSame, reasons := bpfmanagentinternal.DoesProgExist(loadedBpfProgram, loadRequest)
			if !isSame {
				r.Logger.Info("Attachment is in wrong state, detach and re-attach", "reason", reasons, "Attach ID", rec.getAttachId())
				if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *rec.getAttachId()); err != nil {
					rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondNotUnloaded)
					r.Logger.Error(err, "Failed to detach eBPF Program")
					return false, err
				}

				r.Logger.Info("Calling bpfman to attach eBPF Program")
				attachId, err := bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
				if err != nil {
					rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondNotLoaded)
					r.Logger.Error(err, "Failed to attach eBPF Program")
					return false, err
				}
				rec.setAttachId(attachId)
				rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondAttached)
			} else {
				// Attachment exists and bpfProgram K8s Object is up to date
				r.Logger.V(1).Info("Attachment is in correct state.  Nothing to do in bpfman")
				rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondAttached)
			}
		case false:
			// The program should be attached, but it isn't.
			r.Logger.Info("Program is not attached, calling getLoadRequest()")
			loadRequest, err := rec.getLoadRequest(mapOwnerStatus.mapOwnerId)
			if err != nil {
				rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError)
				return false, err
			}
			r.Logger.Info("Calling bpfman to attach eBPF Program on node")
			attachId, err := bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
			if err != nil {
				rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondNotLoaded)
				r.Logger.Error(err, "Failed to attach eBPF Program")
				return false, err
			}
			rec.setAttachId(attachId)
			rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondAttached)
		}
	case false:
		switch isAttached {
		case true:
			// The program is attached but it shouldn't be attached.  Unload it.
			r.Logger.Info("Calling bpfman to detach eBPF Program", "Attach ID", rec.getAttachId())
			if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *rec.getAttachId()); err != nil {
				rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondNotUnloaded)
				r.Logger.Error(err, "Failed to detach eBPF Program")
				return false, err
			}
			rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondNotAttached)
		case false:
			// The program shouldn't be attached and it isn't.
			rec.setAttachStatus(bpfmaniov1alpha1.BpfProgCondNotAttached)
		}
	}
	// The BPF program was successfully reconciled.

	remove := !rec.shouldAttach() && rec.getAttachStatus() == bpfmaniov1alpha1.BpfProgCondNotAttached
	return remove, nil
}

// removeFinalizer removes the finalizer from the object if is applied,
// returning if the action resulted in a kube API update or not along with any
// errors.
func (r *ReconcilerCommon) removeFinalizer(ctx context.Context, o client.Object, finalizer string) bool {
	changed := controllerutil.RemoveFinalizer(o, finalizer)
	if changed {
		r.Logger.Info("Calling KubeAPI to remove finalizer from BpfProgram", "object name", o.GetName())
		err := r.Update(ctx, o)
		if err != nil {
			r.Logger.Error(err, "failed to remove BpfProgram Finalizer")
			return true
		}
	}

	return changed
}
