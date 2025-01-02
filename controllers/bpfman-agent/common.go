/*
Copyright 2022.

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
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	"github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=tcprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=tcxprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=xdpprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=tracepointprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=kprobeprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=uprobeprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=fentryprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=fexityprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=xdpnsprograms/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get

const (
	retryDurationAgent     = 5 * time.Second
	programDoesNotExistErr = "does not exist"
)

type BpfProg interface {

	// GetName returns the name of the current program.
	GetName() string

	// GetUID returns the UID of the current program.
	GetUID() types.UID
	GetAnnotations() map[string]string
	GetLabels() map[string]string
	GetStatus() *bpfmaniov1alpha1.BpfProgramStatus
	GetClientObject() client.Object
}

type BpfProgList[T any] interface {
	// bpfmaniov1alpha1.BpfProgramList | bpfmaniov1alpha1.BpfNsProgramList

	GetItems() []T
}

// ReconcilerCommon provides a skeleton for all *Program Reconcilers.
type ReconcilerCommon[T BpfProg, TL BpfProgList[T]] struct {
	client.Client
	Scheme       *runtime.Scheme
	GrpcConn     *grpc.ClientConn
	BpfmanClient gobpfman.BpfmanClient
	Logger       logr.Logger
	NodeName     string
	progId       *uint32
	finalizer    string
	recType      string
	appOwner     metav1.Object // Set if the owner is an application
	Containers   ContainerGetter
}

// bpfmanReconciler defines a generic bpfProgram K8s object reconciler which can
// program bpfman from user intent in the K8s CRDs.
type bpfmanReconciler[T BpfProg, TL BpfProgList[T]] interface {
	// BPF Cluster of Namespaced Reconciler
	//
	// createBpfProgram creates either a BpfProgram or BpfNsProgram instance. It is pushed
	// to the Kubernetes API server at a later time.
	createBpfProgram(
		attachPoint string,
		rec bpfmanReconciler[T, TL],
		annotations map[string]string,
	) (*T, error)
	// getBpfList calls the Kubernetes API server to retrieve a list of BpfProgram or BpfNsProgram objects.
	getBpfList(ctx context.Context, opts []client.ListOption) (*TL, error)
	// updateBpfStatus calls the Kubernetes API server to write a new condition to the Status of a
	// BpfProgram or BpfNsProgram object.
	updateBpfStatus(ctx context.Context, bpfProgram *T, condition metav1.Condition) error

	// *Program Reconciler
	//
	// SetupWithManager registers the reconciler with the manager and defines
	// which kubernetes events will trigger a reconcile.
	SetupWithManager(mgr ctrl.Manager) error
	// Reconcile is the main entry point to the reconciler. It will be called by
	// the controller runtime when something happens that the reconciler is
	// interested in. When Reconcile is invoked, it initializes some state in
	// the given bpfmanReconciler, retrieves a list of all programs of the given
	// type, and then calls reconcileCommon.
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	// getFinalizer returns the string used for the finalizer to prevent the
	// BpfProgram object from deletion until cleanup can be performed
	getFinalizer() string
	// getOwner returns the owner of the BpfProgram object.  This is either the
	// *Program or the BpfApplicationProgram that created it.
	getOwner() metav1.Object
	// getRecType returns the type of the reconciler.  This is often the string
	// representation of the ProgramType, but in cases where there are multiple
	// reconcilers for a single ProgramType, it may be different (e.g., uprobe,
	// fentry, and fexit)
	getRecType() string
	// getProgType returns the ProgramType used by bpfman for the bpfPrograms
	// the reconciler manages.
	getProgType() internal.ProgramType
	// getName returns the name of the current program being reconciled.
	getName() string
	// getNamespace returns the namespace of the current program being reconciled.
	getNamespace() string
	// getNoContAnnotationIndex returns the index into the annotations map for the "NoContainersOnNode"
	// field. Each program type has its own unique index, and common code needs to look in the annotations
	// to determine if No Containers were found. Example indexes: TcNoContainersOnNode, XdpNoContainersOnNode, ...
	getNoContAnnotationIndex() string
	// getExpectedBpfPrograms returns the list of BpfPrograms that are expected
	// to be loaded on the current node.
	getExpectedBpfPrograms(ctx context.Context) (*TL, error)
	// getLoadRequest returns the LoadRequest that should be sent to bpfman to
	// load the given BpfProgram.
	getLoadRequest(bpfProgram *T, mapOwnerId *uint32) (*gobpfman.LoadRequest, error)
	// getNode returns node object for the current node.
	getNode() *v1.Node
	// getBpfProgramCommon returns the BpfProgramCommon object for the current
	// Program being reconciled.
	getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon
	// setCurrentProgram sets the current *Program for the reconciler as well as
	// any other related state needed.
	setCurrentProgram(program client.Object) error
	// getNodeSelector returns the node selector for the nodes where bpf programs will be deployed.
	getNodeSelector() *metav1.LabelSelector
	// getBpfGlobalData returns the Bpf program global variables.
	getBpfGlobalData() map[string][]byte
	// getAppProgramId() returns the program qualifier for the current
	// program.  If there is no qualifier, it should return an empty string.
	getAppProgramId() string
}

// reconcileCommon is the common reconciler loop called by each bpfman
// reconciler.  It reconciles each program in the list.  The boolean return
// value is set to true if we've made it through all the programs in the list
// without anything being updated and a requeue has not been requested. Otherwise,
// it's set to false. reconcileCommon should not return error because it will
// trigger an infinite reconcile loop. Instead, it should report the error to
// user and retry if specified. For some errors the controller may decide not to
// retry. Note: This only results in calls to bpfman if we need to change
// something
func (r *ReconcilerCommon[T, TL]) reconcileCommon(ctx context.Context, rec bpfmanReconciler[T, TL],
	programs []client.Object) (bool, ctrl.Result, error) {

	r.Logger.V(1).Info("Start reconcileCommon()")

	// Get existing ebpf state from bpfman.
	loadedBpfPrograms, err := bpfmanagentinternal.ListBpfmanPrograms(ctx, r.BpfmanClient, rec.getProgType())
	if err != nil {
		r.Logger.Error(err, "failed to list loaded bpfman programs")
		return false, ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
	}

	requeue := false // initialize requeue to false
	for _, program := range programs {
		r.Logger.V(1).Info("Reconciling program", "Name", program.GetName())

		// Save the *Program CRD of the current program being reconciled
		err := rec.setCurrentProgram(program)
		if err != nil {
			r.Logger.Error(err, "Failed to set current program")
			return false, ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}

		result, err := r.reconcileProgram(ctx, rec, program, loadedBpfPrograms)
		if err != nil {
			r.Logger.Error(err, "Reconciling program failed", "Name", rec.getName, "ReconcileResult", result.String())
		}

		switch result {
		case internal.Unchanged:
			// continue with next program
		case internal.Updated:
			// return
			return false, ctrl.Result{Requeue: false}, nil
		case internal.Requeue:
			// remember to do a requeue when we're done and continue with next program
			requeue = true
		}
	}

	if requeue {
		// A requeue has been requested
		return false, ctrl.Result{RequeueAfter: retryDurationAgent}, nil
	} else {
		// We've made it through all the programs in the list without anything being
		// updated and a reque has not been requested.
		return true, ctrl.Result{Requeue: false}, nil
	}
}

// reconcileBpfmanPrograms ONLY reconciles the bpfman state for a single BpfProgram.
// It does not interact with the k8s API in any way.
func (r *ReconcilerCommon[T, TL]) reconcileBpfProgram(ctx context.Context,
	rec bpfmanReconciler[T, TL],
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	bpfProgram *T,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus) (bpfmaniov1alpha1.BpfProgramConditionType, error) {

	r.Logger.V(1).Info("enter reconcileBpfmanProgram()", "Name", (*bpfProgram).GetName(), "CurrentProgram", rec.getName())

	uuid := (*bpfProgram).GetUID()
	noContainersOnNode := noContainersOnNode(bpfProgram, rec.getNoContAnnotationIndex())
	loadedBpfProgram, isLoaded := loadedBpfPrograms[string(uuid)]
	shouldBeLoaded := bpfProgramShouldBeLoaded(isNodeSelected, isBeingDeleted, noContainersOnNode, mapOwnerStatus)

	r.Logger.V(1).Info("reconcileBpfmanProgram()", "shouldBeLoaded", shouldBeLoaded, "isLoaded", isLoaded)

	switch isLoaded {
	case true:
		// prog ID should already have been set if program is loaded
		id, err := GetID(bpfProgram)
		if err != nil {
			r.Logger.Error(err, "Failed to get kernel ID for BpfProgram")
			return bpfmaniov1alpha1.BpfProgCondNotLoaded, err
		}
		switch shouldBeLoaded {
		case true:
			// The program is loaded and it should be loaded.
			// Confirm it's in the correct state.
			loadRequest, err := rec.getLoadRequest(bpfProgram, mapOwnerStatus.mapOwnerId)
			if err != nil {
				return bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError, err
			}
			isSame, reasons := bpfmanagentinternal.DoesProgExist(loadedBpfProgram, loadRequest)
			if !isSame {
				r.Logger.Info("BpfProgram is in wrong state, unloading and reloading", "reason", reasons, "Name", (*bpfProgram).GetName(), "Program ID", id)
				if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
					r.Logger.Error(err, "Failed to unload eBPF Program")
					return bpfmaniov1alpha1.BpfProgCondNotUnloaded, err
				}

				r.Logger.Info("Calling bpfman to load eBPF Program on Node", "Name", (*bpfProgram).GetName())
				r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
				if err != nil {
					r.Logger.Error(err, "Failed to load eBPF Program")
					return bpfmaniov1alpha1.BpfProgCondNotLoaded, err
				}
			} else {
				// Program exists and bpfProgram K8s Object is up to date
				r.Logger.V(1).Info("BpfProgram is in correct state.  Nothing to do in bpfman")
				r.progId = id
			}
		case false:
			// The program is loaded but it shouldn't be loaded.
			r.Logger.Info("Calling bpfman to unload eBPF Program on node", "Name", (*bpfProgram).GetName(), "Program ID", id)
			if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
				r.Logger.Error(err, "Failed to unload eBPF Program")
				return bpfmaniov1alpha1.BpfProgCondNotUnloaded, err
			}
		}
	case false:
		switch shouldBeLoaded {
		case true:
			// The program isn't loaded but it should be loaded.
			loadRequest, err := rec.getLoadRequest(bpfProgram, mapOwnerStatus.mapOwnerId)
			if err != nil {
				return bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError, err
			}

			r.Logger.Info("Calling bpfman to load eBPF Program on node", "Name", (*bpfProgram).GetName())
			r.progId, err = bpfmanagentinternal.LoadBpfmanProgram(ctx, r.BpfmanClient, loadRequest)
			if err != nil {
				r.Logger.Error(err, "Failed to load eBPF Program")
				return bpfmaniov1alpha1.BpfProgCondNotLoaded, err
			}
		case false:
			// The program isn't loaded and it shouldn't be loaded.
		}
	}

	// The BPF program was successfully reconciled.
	return r.reconcileBpfProgramSuccessCondition(
		isLoaded,
		shouldBeLoaded,
		isNodeSelected,
		isBeingDeleted,
		noContainersOnNode,
		mapOwnerStatus), nil
}

// reconcileBpfProgramSuccessCondition returns the proper condition for a
// successful reconcile of a bpfProgram based on the given parameters.
func (r *ReconcilerCommon[T, TL]) reconcileBpfProgramSuccessCondition(
	isLoaded bool,
	shouldBeLoaded bool,
	isNodeSelected bool,
	isBeingDeleted bool,
	noContainersOnNode bool,
	mapOwnerStatus *MapOwnerParamStatus) bpfmaniov1alpha1.BpfProgramConditionType {

	switch isLoaded {
	case true:
		switch shouldBeLoaded {
		case true:
			// The program is loaded and it should be loaded.
			return bpfmaniov1alpha1.BpfProgCondLoaded
		case false:
			// The program is loaded but it shouldn't be loaded.
			if isBeingDeleted {
				return bpfmaniov1alpha1.BpfProgCondUnloaded
			}
			if !isNodeSelected {
				return bpfmaniov1alpha1.BpfProgCondNotSelected
			}
			if noContainersOnNode {
				return bpfmaniov1alpha1.BpfProgCondNoContainersOnNode
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded
			}
			// If we get here, there's a problem.  All of the possible reasons
			// that a program should not be loaded should have been handled
			// above.
			r.Logger.Error(nil, "unhandled case in isLoaded && !shouldBeLoaded")
			return bpfmaniov1alpha1.BpfProgCondUnloaded
		}
	case false:
		switch shouldBeLoaded {
		case true:
			// The program isn't loaded but it should be loaded.
			return bpfmaniov1alpha1.BpfProgCondLoaded
		case false:
			// The program isn't loaded and it shouldn't be loaded.
			if isBeingDeleted {
				return bpfmaniov1alpha1.BpfProgCondUnloaded
			}
			if !isNodeSelected {
				return bpfmaniov1alpha1.BpfProgCondNotSelected
			}
			if noContainersOnNode {
				return bpfmaniov1alpha1.BpfProgCondNoContainersOnNode
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isFound {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound
			}
			if mapOwnerStatus.isSet && !mapOwnerStatus.isLoaded {
				return bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded
			}
			// If we get here, there's a problem.  All of the possible reasons
			// that a program should not be loaded should have been handled
			// above.
			r.Logger.Error(nil, "unhandled case in !isLoaded && !shouldBeLoaded")
			return bpfmaniov1alpha1.BpfProgCondUnloaded
		}
	}

	// We should never get here, but need this return to satisfy the compiler.
	r.Logger.Error(nil, "unhandled case in reconcileBpfProgramSuccessCondition()")
	return bpfmaniov1alpha1.BpfProgCondNone
}

func bpfProgramShouldBeLoaded(
	isNodeSelected bool,
	isBeingDeleted bool,
	noContainersOnNode bool,
	mapOwnerStatus *MapOwnerParamStatus) bool {
	return isNodeSelected && !isBeingDeleted && !noContainersOnNode && mapOk(mapOwnerStatus)
}

func mapOk(mapOwnerStatus *MapOwnerParamStatus) bool {
	return !mapOwnerStatus.isSet || (mapOwnerStatus.isSet && mapOwnerStatus.isFound && mapOwnerStatus.isLoaded)
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

func isNodeSelected(selector *metav1.LabelSelector, nodeLabels map[string]string) (bool, error) {
	// Logic to check if this node is selected by the *Program object
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

// removeFinalizer removes the finalizer from the BpfProgram object if is applied,
// returning if the action resulted in a kube API update or not along with any
// errors.
func (r *ReconcilerCommon[T, TL]) removeFinalizer(ctx context.Context, o client.Object, finalizer string) bool {
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

// updateStatus updates the status of a BpfProgram object if needed, returning
// false if the status was already set for the given bpfProgram, meaning reconciliation
// may continue.
func (r *ReconcilerCommon[T, TL]) updateStatus(
	ctx context.Context,
	rec bpfmanReconciler[T, TL],
	bpfProgram *T,
	cond bpfmaniov1alpha1.BpfProgramConditionType,
) bool {
	status := (*bpfProgram).GetStatus()
	r.Logger.V(1).Info("updateStatus()", "existing conds", status.Conditions, "new cond", cond)

	if status.Conditions != nil {
		numConditions := len(status.Conditions)

		if numConditions == 1 {
			if status.Conditions[0].Type == string(cond) {
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

	//meta.SetStatusCondition(&status.Conditions, cond.Condition())

	r.Logger.Info("Calling KubeAPI to update BpfProgram condition", "Name", (*bpfProgram).GetName(), "condition", cond.Condition().Type)
	//if err := r.Status().Update(ctx, (*bpfProgram).GetClientObject()); err != nil {
	if err := rec.updateBpfStatus(ctx, bpfProgram, cond.Condition()); err != nil {
		r.Logger.Error(err, "failed to set BpfProgram object status")
	}

	r.Logger.V(1).Info("condition updated", "new condition", cond)
	return true
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func statusContains[T BpfProg](bpfProgram *T, cond bpfmaniov1alpha1.BpfProgramConditionType) bool {
	status := (*bpfProgram).GetStatus()
	for _, c := range status.Conditions {
		if c.Type == string(cond) {
			return true
		}
	}
	return false
}

type bpfProgKey struct {
	appProgId   string
	attachPoint string
}

func (r *ReconcilerCommon[T, TL]) getExistingBpfPrograms(ctx context.Context,
	rec bpfmanReconciler[T, TL]) (map[bpfProgKey]T, error) {

	// Only list bpfPrograms for this *Program and the controller's node
	opts := []client.ListOption{
		client.MatchingLabels{
			internal.BpfProgramOwner: rec.getOwner().GetName(),
			internal.AppProgramId:    rec.getAppProgramId(),
			internal.K8sHostLabel:    r.NodeName,
		},
	}

	bpfProgramList, err := rec.getBpfList(ctx, opts)
	if err != nil {
		return nil, err
	}

	existingBpfPrograms := map[bpfProgKey]T{}
	for _, bpfProg := range (*bpfProgramList).GetItems() {
		key := bpfProgKey{
			appProgId:   bpfProg.GetLabels()[internal.AppProgramId],
			attachPoint: bpfProg.GetAnnotations()[internal.BpfProgramAttachPoint],
		}
		existingBpfPrograms[key] = bpfProg
	}

	return existingBpfPrograms, nil
}

func generateUniqueName(baseName string) string {
	uuid := uuid.New().String()
	return fmt.Sprintf("%s-%s", baseName, uuid[:8])
}

// Programs may be deleted for one of two reasons.  The first is that the global
// *Program object is being deleted.  The second is that the something has
// changed on the node that is causing the need to remove individual
// bpfPrograms. Typically this happens when containers that used to match a
// container selector are deleted and the eBPF programs that were installed in
// them need to be removed.  This function handles both of these cases.
//
// For the first case, deletion of a *Program takes a few steps if there are
// existing bpfPrograms:
//  1. Reconcile the bpfProgram (take bpfman cleanup steps).
//  2. Remove any finalizers from the bpfProgram Object.
//  3. Update the condition on the bpfProgram to BpfProgCondUnloaded so the
//     operator knows it's safe to remove the parent Program Object, which
//     is when the bpfProgram is automatically deleted by the owner-reference.
//
// For the second case, we need to do the first 2 steps, and then explicitly
// delete the bpfPrograms that are no longer needed.
func (r *ReconcilerCommon[T, TL]) handleProgDelete(
	ctx context.Context,
	rec bpfmanReconciler[T, TL],
	existingBpfPrograms map[bpfProgKey]T,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus,
) (internal.ReconcileResult, error) {
	r.Logger.V(1).Info("handleProgDelete()", "isBeingDeleted", isBeingDeleted, "isNodeSelected",
		isNodeSelected, "mapOwnerStatus", mapOwnerStatus)
	for _, bpfProgram := range existingBpfPrograms {
		r.Logger.V(1).Info("Deleting BpfProgram", "Name", bpfProgram.GetName())
		// Reconcile the bpfProgram if error write condition and exit with
		// retry.
		cond, err := r.reconcileBpfProgram(ctx,
			rec,
			loadedBpfPrograms,
			&bpfProgram,
			isNodeSelected,
			true, // delete program
			mapOwnerStatus,
		)
		if err != nil {
			r.updateStatus(ctx, rec, &bpfProgram, cond)
			return internal.Requeue, fmt.Errorf("failed to delete program from bpfman: %v", err)
		}

		if isBeingDeleted {
			// We're deleting these programs because the *Program is being
			// deleted.

			// So update the status.
			if r.updateStatus(ctx, rec, &bpfProgram, cond) {
				return internal.Updated, nil
			}

			// Then remove the finalizer, and the program will be deleted when
			// the owner is deleted.
			if r.removeFinalizer(ctx, bpfProgram.GetClientObject(), rec.getFinalizer()) {
				return internal.Updated, nil
			}
		} else {
			// We're deleting these programs because they were not expected due
			// to changes that caused the containers to not be selected anymore.

			// So, remove the finalizer.
			if r.removeFinalizer(ctx, bpfProgram.GetClientObject(), rec.getFinalizer()) {
				return internal.Updated, nil
			}

			// Then explicitly delete them.
			opts := client.DeleteOptions{}
			r.Logger.Info("Calling KubeAPI to delete BpfProgram", "Name", bpfProgram.GetName(), "Owner", bpfProgram.GetName())
			if err := r.Delete(ctx, bpfProgram.GetClientObject(), &opts); err != nil {
				return internal.Requeue, fmt.Errorf("failed to delete BpfProgram object: %v", err)
			}
			return internal.Updated, nil
		}
	}

	// We're done reconciling.
	r.Logger.Info("No change in status", "Name", rec.getName())
	return internal.Unchanged, nil
}

// unLoadAndDeleteProgramsList unloads and deletes BbpPrograms when the owning
// *Program or BpfApplication is not being deleted itself, but something
// has changed such that the BpfPrograms are no longer needed.
func (r *ReconcilerCommon[T, TL]) unLoadAndDeleteBpfProgramsList(
	ctx context.Context,
	rec bpfmanReconciler[T, TL],
	bpfProgramsList *TL,
	finalizerString string,
) (reconcile.Result, error) {
	for _, bpfProgram := range (*bpfProgramsList).GetItems() {
		r.Logger.V(1).Info("Deleting BpfProgram", "Name", bpfProgram.GetName())
		id, err := GetID(&bpfProgram)
		if err != nil {
			r.Logger.Error(err, "Failed to get kernel ID from BpfProgram")
			return ctrl.Result{}, nil
		}

		if id != nil && !statusContains(&bpfProgram, bpfmaniov1alpha1.BpfProgCondUnloaded) {
			r.Logger.Info("Calling bpfman to unload program on node", "Name", bpfProgram.GetName(), "Program ID", id)
			if err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *id); err != nil {
				if strings.Contains(err.Error(), programDoesNotExistErr) {
					r.Logger.Info("Program not found on node", "Name", bpfProgram.GetName(), "Program ID", id)
				} else {
					r.Logger.Error(err, "Failed to unload Program from bpfman")
					return ctrl.Result{RequeueAfter: retryDurationAgent}, nil
				}
				if r.updateStatus(ctx, rec, &bpfProgram, bpfmaniov1alpha1.BpfProgCondUnloaded) {
					return ctrl.Result{}, nil
				}
			}
		}

		if r.updateStatus(ctx, rec, &bpfProgram, bpfmaniov1alpha1.BpfProgCondUnloaded) {
			return ctrl.Result{}, nil
		}

		if r.removeFinalizer(ctx, bpfProgram.GetClientObject(), finalizerString) {
			return ctrl.Result{}, nil
		}

		opts := client.DeleteOptions{}
		r.Logger.Info("Calling KubeAPI to delete BpfProgram", "Name", bpfProgram.GetName(), "Owner", bpfProgram.GetName())
		if err := r.Delete(ctx, bpfProgram.GetClientObject(), &opts); err != nil {
			return ctrl.Result{RequeueAfter: retryDurationAgent}, fmt.Errorf("failed to delete BpfProgram object: %v", err)
		} else {
			// we will deal one program at a time, so we can break out of the loop
			break
		}
	}
	return ctrl.Result{}, nil
}

// handleProgCreateOrUpdate compares the expected bpfPrograms to the existing
// bpfPrograms.  If a bpfProgram is expected but doesn't exist, it is created.
// If an expected bpfProgram exists, it is reconciled. If a bpfProgram exists
// but is not expected, it is deleted.
func (r *ReconcilerCommon[T, TL]) handleProgCreateOrUpdate(
	ctx context.Context,
	rec bpfmanReconciler[T, TL],
	existingBpfPrograms map[bpfProgKey]T,
	expectedBpfPrograms *TL,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult,
	isNodeSelected bool,
	isBeingDeleted bool,
	mapOwnerStatus *MapOwnerParamStatus,
) (internal.ReconcileResult, error) {
	r.Logger.V(1).Info("handleProgCreateOrUpdate()", "isBeingDeleted", isBeingDeleted, "isNodeSelected",
		isNodeSelected, "mapOwnerStatus", mapOwnerStatus)
	// If the *Program isn't being deleted ALWAYS create the bpfPrograms
	// even if the node isn't selected
	for _, expectedBpfProgram := range (*expectedBpfPrograms).GetItems() {
		r.Logger.V(1).Info("Creating or Updating", "Name", expectedBpfProgram.GetName())
		key := bpfProgKey{
			appProgId:   expectedBpfProgram.GetLabels()[internal.AppProgramId],
			attachPoint: expectedBpfProgram.GetAnnotations()[internal.BpfProgramAttachPoint],
		}
		existingBpfProgram, exists := existingBpfPrograms[key]
		if exists {
			// Remove the bpfProgram from the existingPrograms map so we know
			// not to delete it below.
			delete(existingBpfPrograms, key)
		} else {
			// Create a new bpfProgram Object for this program.
			opts := client.CreateOptions{}
			r.Logger.Info("Calling KubeAPI to create BpfProgram", "Name", expectedBpfProgram.GetName(), "Owner", rec.getOwner().GetName())
			if err := r.Create(ctx, expectedBpfProgram.GetClientObject(), &opts); err != nil {
				return internal.Requeue, fmt.Errorf("failed to create BpfProgram object: %v", err)
			}
			return internal.Updated, nil
		}

		// bpfProgram Object exists go ahead and reconcile it, if there is
		// an error write condition and exit with retry.
		cond, err := r.reconcileBpfProgram(ctx,
			rec,
			loadedBpfPrograms,
			&existingBpfProgram,
			isNodeSelected,
			isBeingDeleted,
			mapOwnerStatus,
		)
		if err != nil {
			if r.updateStatus(ctx, rec, &existingBpfProgram, cond) {
				// Return an error the first time.
				return internal.Updated, fmt.Errorf("failed to reconcile bpfman program: %v", err)
			}
		} else {
			// Make sure if we're not selected exit and write correct condition
			if cond == bpfmaniov1alpha1.BpfProgCondNotSelected ||
				cond == bpfmaniov1alpha1.BpfProgCondMapOwnerNotFound ||
				cond == bpfmaniov1alpha1.BpfProgCondMapOwnerNotLoaded ||
				cond == bpfmaniov1alpha1.BpfProgCondNoContainersOnNode {
				// Write NodeNodeSelected status
				if r.updateStatus(ctx, rec, &existingBpfProgram, cond) {
					r.Logger.V(1).Info("Update condition from bpfman reconcile", "condition", cond)
					return internal.Updated, nil
				} else {
					continue
				}
			}

			// GetID() will fail if ProgramId is not in the annotations, which is expected on a
			// create. In this case existingId will be nil and DeepEqual() will fail and cause
			// annotation to be set.
			existingId, _ := GetID(&existingBpfProgram)

			// If bpfProgram Maps OR the program ID annotation isn't up to date just update it and return
			if !reflect.DeepEqual(existingId, r.progId) {
				r.Logger.Info("Calling KubeAPI to update BpfProgram Object", "Id", r.progId, "Name", existingBpfProgram.GetName())
				// annotations should be populated on create
				annotations := existingBpfProgram.GetAnnotations()
				annotations[internal.IdAnnotation] = strconv.FormatUint(uint64(*r.progId), 10)

				if err := r.Update(ctx, existingBpfProgram.GetClientObject(), &client.UpdateOptions{}); err != nil {
					return internal.Requeue, fmt.Errorf("failed to update BpfProgram's Programs: %v", err)
				}
				return internal.Updated, nil
			}

			if r.updateStatus(ctx, rec, &existingBpfProgram, cond) {
				return internal.Updated, nil
			}
		}
	}

	// We're done reconciling the expected programs.  If any unexpected programs
	// exist, delete them and return the result.
	if len(existingBpfPrograms) > 0 {
		return r.handleProgDelete(ctx, rec, existingBpfPrograms, loadedBpfPrograms, isNodeSelected, isBeingDeleted, mapOwnerStatus)
	} else {
		// We're done reconciling.
		r.Logger.Info("No change in status", "Name", rec.getName())
		return internal.Unchanged, nil
	}
}

// reconcileProgram is called by ALL *Program controllers, and contains much of
// the core logic for taking *Program objects, turning them into bpfProgram
// object(s), and ultimately telling the custom controller types to load real
// bpf programs on the node via bpfman. Additionally it acts as a central point for
// interacting with the K8s API. This function will exit if any action is taken
// against the K8s API. If the function returns a retry boolean and error, the
// reconcile will be retried based on a default 5 second interval if the retry
// boolean is set to `true`.
func (r *ReconcilerCommon[T, TL]) reconcileProgram(ctx context.Context,
	rec bpfmanReconciler[T, TL],
	program client.Object,
	loadedBpfPrograms map[string]*gobpfman.ListResponse_ListResult) (internal.ReconcileResult, error) {

	r.Logger.V(1).Info("reconcileProgram", "name", program.GetName())

	// Determine which node local actions should be taken based on whether the node is selected
	// OR if the *Program is being deleted.
	isNodeSelected, err := isNodeSelected(rec.getNodeSelector(), rec.getNode().Labels)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to check if node is selected: %v", err)
	}

	isBeingDeleted := !rec.getOwner().GetDeletionTimestamp().IsZero()

	// Query the K8s API to get a list of existing bpfPrograms for this *Program
	// on this node.
	existingBpfPrograms, err := r.getExistingBpfPrograms(ctx, rec)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to get existing BpfPrograms: %v", err)
	}

	// Determine if the MapOwnerSelector was set, and if so, see if the MapOwner
	// ID can be found.
	mapOwnerStatus, err := r.processMapOwnerParam(ctx, rec, &rec.getBpfProgramCommon().MapOwnerSelector)
	if err != nil {
		return internal.Requeue, fmt.Errorf("failed to determine map owner: %v", err)
	}
	r.Logger.V(1).Info("ProcessMapOwnerParam",
		"isSet", mapOwnerStatus.isSet,
		"isFound", mapOwnerStatus.isFound,
		"isLoaded", mapOwnerStatus.isLoaded,
		"mapOwnerid", mapOwnerStatus.mapOwnerId)

	switch isBeingDeleted {
	case true:
		return r.handleProgDelete(ctx, rec, existingBpfPrograms, loadedBpfPrograms,
			isNodeSelected, isBeingDeleted, mapOwnerStatus)
	case false:
		// Generate the list of BpfPrograms for this *Program. This handles the
		// one *Program to many BpfPrograms (e.g., One *Program maps to multiple
		// interfaces because of PodSelector, or one *Program needs to be
		// installed in multiple containers because of ContainerSelector).
		expectedBpfPrograms, err := rec.getExpectedBpfPrograms(ctx)
		if err != nil {
			return internal.Requeue, fmt.Errorf("failed to get expected BpfPrograms: %v", err)
		}
		return r.handleProgCreateOrUpdate(ctx, rec, existingBpfPrograms, expectedBpfPrograms, loadedBpfPrograms,
			isNodeSelected, isBeingDeleted, mapOwnerStatus)
	}

	// This return should never be reached, but it's here to satisfy the compiler.
	return internal.Unchanged, nil
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
func (r *ReconcilerCommon[T, TL]) processMapOwnerParam(
	ctx context.Context,
	rec bpfmanReconciler[T, TL],
	selector *metav1.LabelSelector) (*MapOwnerParamStatus, error) {
	mapOwnerStatus := &MapOwnerParamStatus{}

	// Parse the MapOwnerSelector label selector.
	mapOwnerSelectorMap, err := metav1.LabelSelectorAsMap(selector)
	if err != nil {
		mapOwnerStatus.isSet = true
		return mapOwnerStatus, fmt.Errorf("failed to parse MapOwnerSelector: %v", err)
	}

	// If no data was entered, just return with default values, all flags set to false.
	if len(mapOwnerSelectorMap) == 0 {
		return mapOwnerStatus, nil
	} else {
		mapOwnerStatus.isSet = true

		// Add the labels from the MapOwnerSelector to a map and add an additional
		// label to filter on just this node. Call K8s to find all the eBPF programs
		// that match this filter.
		labelMap := client.MatchingLabels{internal.K8sHostLabel: r.NodeName}
		for key, value := range mapOwnerSelectorMap {
			labelMap[key] = value
		}
		opts := []client.ListOption{labelMap}
		r.Logger.V(1).Info("MapOwner Labels:", "opts", opts)
		bpfProgramList, err := rec.getBpfList(ctx, opts)
		if err != nil {
			return mapOwnerStatus, err
		}

		// If no BpfProgram Objects were found, or more than one, then return.
		items := (*bpfProgramList).GetItems()
		if len(items) == 0 {
			return mapOwnerStatus, nil
		} else if len(items) > 1 {
			return mapOwnerStatus, fmt.Errorf("MapOwnerSelector resolved to multiple BpfProgram Objects")
		} else {
			mapOwnerStatus.isFound = true

			// Get bpfProgram based on UID meta
			prog, err := bpfmanagentinternal.GetBpfmanProgram(ctx, r.BpfmanClient, items[0].GetUID())
			if err != nil {
				return nil, fmt.Errorf("failed to get bpfman program for BpfProgram with UID %s: %v", items[0].GetUID(), err)
			}

			kernelInfo := prog.GetKernelInfo()
			if kernelInfo == nil {
				return nil, fmt.Errorf("failed to process bpfman program for BpfProgram with UID %s: %v", items[0].GetUID(), err)
			}
			mapOwnerStatus.mapOwnerId = &kernelInfo.Id

			// Get most recent condition from the one eBPF Program and determine
			// if the BpfProgram is loaded or not.
			conLen := len(items[0].GetStatus().Conditions)
			if conLen > 0 &&
				items[0].GetStatus().Conditions[conLen-1].Type ==
					string(bpfmaniov1alpha1.BpfProgCondLoaded) {
				mapOwnerStatus.isLoaded = true
			}

			return mapOwnerStatus, nil
		}
	}
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

// sanitize a string to work as a bpfProgram name
func sanitize(name string) string {
	name = strings.TrimPrefix(name, "/")
	name = strings.Replace(strings.Replace(name, "/", "-", -1), "_", "-", -1)
	return strings.ToLower(name)
}

func appProgramId(labels map[string]string) string {
	id, ok := labels[internal.AppProgramId]
	if ok {
		return id
	}
	return ""
}

// getBpfProgram returns a BpfProgram object in the bpfProgram parameter based
// on the given owner, appProgId, and attachPoint. If the BpfProgram is not
// found, an error is returned.
//
//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *ReconcilerCommon[T, TL]) getBpfProgram(
	ctx context.Context,
	rec bpfmanReconciler[T, TL],
	owner string,
	appProgId string,
	attachPoint string,
	bpfProgram *T) error {

	// Only list bpfPrograms for this *Program and the controller's node
	opts := []client.ListOption{
		client.MatchingLabels{
			internal.BpfProgramOwner: owner,
			internal.AppProgramId:    appProgId,
			internal.K8sHostLabel:    r.NodeName,
		},
	}

	bpfProgramList, err := rec.getBpfList(ctx, opts)
	if err != nil {
		return err
	}

	for _, bpfProg := range (*bpfProgramList).GetItems() {
		if appProgId == bpfProg.GetLabels()[internal.AppProgramId] &&
			attachPoint == bpfProg.GetAnnotations()[internal.BpfProgramAttachPoint] {
			*bpfProgram = bpfProg
			return nil
		}
	}

	return fmt.Errorf("BpfProgram not found")
}

// get the program ID from a bpfProgram
func GetID[T BpfProg](p *T) (*uint32, error) {
	annotations := (*p).GetAnnotations()
	idString, ok := annotations[internal.IdAnnotation]
	if !ok {
		return nil, fmt.Errorf("failed to get program ID because no annotations")
	}

	id, err := strconv.ParseUint(idString, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse program ID: %v", err)
	}
	uid := uint32(id)
	return &uid, nil
}
