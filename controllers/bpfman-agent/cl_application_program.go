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
	"reflect"
	"time"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	"github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplications,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates/finalizers,verbs=update
// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get

type ClBpfApplicationReconciler struct {
	ReconcilerCommon
	currentApp      *bpfmaniov1alpha1.ClusterBpfApplication
	currentAppState *bpfmaniov1alpha1.ClusterBpfApplicationState
}

type ClProgramReconcilerCommon struct {
	currentProgram      *bpfmaniov1alpha1.ClBpfApplicationProgram
	currentProgramState *bpfmaniov1alpha1.ClBpfApplicationProgramState
}

func (r *ClBpfApplicationReconciler) getAppStateName() string {
	return r.currentAppState.Name
}

func (r *ClBpfApplicationReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *ClBpfApplicationReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentApp.Spec.NodeSelector
}

func (r *ClBpfApplicationReconciler) GetStatus() *bpfmaniov1alpha1.BpfAppStatus {
	return &r.currentAppState.Status
}

func (r *ClBpfApplicationReconciler) isBeingDeleted() bool {
	return !r.currentApp.GetDeletionTimestamp().IsZero()
}

func (r *ClBpfApplicationReconciler) updateBpfAppStatus(ctx context.Context, condition metav1.Condition) error {
	r.currentAppState.Status.Conditions = nil
	meta.SetStatusCondition(&r.currentAppState.Status.Conditions, condition)
	err := r.Status().Update(ctx, r.currentAppState)
	if err != nil {
		return err
	} else {
		return r.waitForBpfAppStateStatusUpdate(ctx)
	}
}

func (r *ClBpfApplicationReconciler) updateLoadStatus(status bpfmaniov1alpha1.AppLoadStatus) {
	r.currentAppState.Spec.AppLoadStatus = status
}

// SetupWithManager sets up the controller with the Manager. The Bpfman-Agent
// should reconcile whenever a BpfApplication object is updated, load/unload bpf
// programs on the node via bpfman, and create or update a BpfApplicationState
// object to reflect per node state information.
func (r *ClBpfApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.ClusterBpfApplication{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Owns(&bpfmaniov1alpha1.ClusterBpfApplicationState{},
			builder.WithPredicates(internal.BpfNodePredicate(r.NodeName)),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the BpfApplication no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&v1.Node{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		// Watch for changes in Pod resources in case we are using a container selector.
		Watches(
			&v1.Pod{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(podOnNodePredicate(r.NodeName)),
		).
		Complete(r)
}

func (r *ClBpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("cluster-app")
	r.finalizer = internal.ClBpfApplicationControllerFinalizer
	r.recType = internal.ApplicationString

	r.Logger.Info("Enter BpfApplication Reconcile", "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	// Get the list of existing BpfApplication objects
	appPrograms := &bpfmaniov1alpha1.ClusterBpfApplicationList{}
	opts := []client.ListOption{}
	if err := r.List(ctx, appPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfApplicationPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}
	if len(appPrograms.Items) == 0 {
		r.Logger.Info("BpfApplicationController found no application Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	for appProgramIndex := range appPrograms.Items {
		r.currentApp = &appPrograms.Items[appProgramIndex]

		// Get the corresponding BpfApplicationState object, and if it doesn't
		// exist, instantiate a copy. If bpfAppStateNew is true, then we need to
		// create a new BpfApplicationState object instead of just updating the
		// existing one.
		appState, bpfAppStateNew, err := r.getBpfAppState(ctx, true)
		if err != nil {
			r.Logger.Error(err, "failed to get BpfApplicationState")
			return ctrl.Result{}, err
		}
		r.currentAppState = appState

		// Save a copy of the original BpfApplicationState to check for changes
		// at the end of the reconcile process. This approach simplifies the
		// code and reduces the risk of errors by avoiding the need to track
		// changes throughout.  We don't need to do this for new
		// BpfApplicationStates because they don't exist yet and will need to be
		// created anyway.
		var bpfAppStateOriginal *bpfmaniov1alpha1.ClusterBpfApplicationState
		if !bpfAppStateNew {
			bpfAppStateOriginal = r.currentAppState.DeepCopy()
		}

		r.Logger.Info("BpfApplicationState status", "new", bpfAppStateNew)

		if bpfAppStateNew {
			// Create the object and return. We'll get the updated object in the
			// next reconcile.
			_, err := r.updateBpfAppStateSpec(ctx, bpfAppStateOriginal, bpfAppStateNew)
			if err != nil {
				r.Logger.Error(err, "failed to update BpfApplicationState", "Name", r.currentAppState.Name)
				_, _ = r.updateStatus(ctx, r, bpfmaniov1alpha1.BpfAppStateCondError)
				// If there was an error updating the object, request a requeue
				// because we can't be sure what was updated and whether the manager
				// will requeue us without the request.
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
			} else {
				_, _ = r.updateStatus(ctx, r, bpfmaniov1alpha1.BpfAppStateCondPending)
				return ctrl.Result{}, nil
			}
		}

		// Make sure the BpfApplication code is loaded on the node.
		r.Logger.Info("Calling reconcileLoad()")
		err = r.reconcileLoad(ctx, r)
		if err != nil {
			// There's no point continuing to reconcile the links if we
			// can't load the code.
			r.Logger.Error(err, "failed to reconcileLoad")
			objectChanged, _ := r.updateBpfAppStateSpec(ctx, bpfAppStateOriginal, bpfAppStateNew)
			statusChanged, _ := r.updateStatus(ctx, r, bpfmaniov1alpha1.BpfAppStateCondError)
			if statusChanged || objectChanged {
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
			} else {
				// If nothing changed, continue with the next BpfApplication.
				// Otherwise, one bad BpfApplication can block the rest.
				continue
			}
		}

		// Initialize the BpfApplicationState status to Success.  It will be set
		// to Error if any of the programs have an error.
		bpfApplicationStatus := bpfmaniov1alpha1.BpfAppStateCondSuccess

		// If the BpfApplication is being deleted, all of the links would have
		// been detached when the programs were unloaded in the reconcileLoad()
		// operation, so we don't need to reconcile each program here.
		if !r.isBeingDeleted() {
			// Reconcile each program in the BpfApplication
			for progIndex := range r.currentApp.Spec.Programs {
				prog := &r.currentApp.Spec.Programs[progIndex]
				progState, err := r.getProgState(prog, r.currentAppState.Spec.Programs)
				if err != nil {
					// TODO: This entry should have been created when the
					// BpfApplication was loaded.  If it's not here, then we
					// need to do another load, and we'll need to work out how
					// to do that. For now, we're going to log an error
					// and continue.
					//
					// See: https://github.com/bpfman/bpfman-operator/issues/391
					r.Logger.Error(fmt.Errorf("ProgramState not found"),
						"ProgramState not found", "App Name", r.currentApp.Name, "BpfFunctionName", prog.Name)
					bpfApplicationStatus = bpfmaniov1alpha1.BpfAppStateCondProgramListChangedError
					continue
				}

				rec, err := r.getProgramReconciler(prog, progState)
				if err != nil {
					bpfApplicationStatus = bpfmaniov1alpha1.BpfAppStateCondError
					r.Logger.Error(err, "error getting program reconciler", "Name", prog.Name)
					// Skip this program and continue to the next one
					continue
				}

				err = rec.reconcileProgram(ctx, rec, r.isBeingDeleted())
				if err != nil {
					r.Logger.Info("Error reconciling program", "Name", rec.getProgName())
				} else {
					r.Logger.Info("Successfully reconciled program", "Name", rec.getProgName())
				}
			}

			// If the bpfApplicationStatus didn't get changed to an error already,
			// check the status of the programs.
			if bpfApplicationStatus == bpfmaniov1alpha1.BpfAppStateCondSuccess {
				bpfApplicationStatus = r.checkProgramStatus()
			}
		}

		// We've completed reconciling all programs and if something has
		// changed, we need to create or update the BpfApplicationState.
		specChanged, err := r.updateBpfAppStateSpec(ctx, bpfAppStateOriginal, bpfAppStateNew)
		if err != nil {
			r.Logger.Error(err, "failed to update BpfApplicationState", "Name", r.currentAppState.Name)
			_, _ = r.updateStatus(ctx, r, bpfmaniov1alpha1.BpfAppStateCondError)
			// If there was an error updating the object, request a requeue
			// because we can't be sure what was updated and whether the manager
			// will requeue us without the request.
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}

		statusChanged, err := r.updateStatus(ctx, r, bpfApplicationStatus)
		if err != nil {
			// This can happen if the object hasn't been updated in the API
			// server yet, so we'll requeue.
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}

		if specChanged || statusChanged {
			r.Logger.Info("BpfApplicationState updated", "Name", r.currentAppState.Name, "Spec Changed",
				specChanged, "Status Changed", statusChanged)
			return ctrl.Result{}, nil
		}

		if r.isBeingDeleted() {
			r.Logger.Info("BpfApplication is being deleted", "Name", r.currentApp.Name)
			if r.removeFinalizer(ctx, r.currentAppState, r.finalizer) {
				return ctrl.Result{}, nil
			}
		}

		// Nothing changed, so continue with next BpfApplication object.
		r.Logger.Info("No changes to BpfApplicationState object", "Name", r.currentAppState.Name)
	}

	// We're done with all the BpfApplication objects, so we can return.
	r.Logger.Info("All BpfApplication objects have been reconciled")
	return ctrl.Result{}, nil
}

func (r *ClBpfApplicationReconciler) getProgramReconciler(prog *bpfmaniov1alpha1.ClBpfApplicationProgram,
	progState *bpfmaniov1alpha1.ClBpfApplicationProgramState) (ProgramReconciler, error) {

	var rec ProgramReconciler

	switch prog.Type {
	case bpfmaniov1alpha1.ProgTypeFentry:
		rec = &ClFentryProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeFexit:
		rec = &ClFexitProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeKprobe, bpfmaniov1alpha1.ProgTypeKretprobe:
		rec = &ClKprobeProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeUprobe, bpfmaniov1alpha1.ProgTypeUretprobe:
		rec = &ClUprobeProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTracepoint:
		rec = &ClTracepointProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTC:
		rec = &ClTcProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTCX:
		rec = &ClTcxProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeXDP:
		rec = &ClXdpProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ClProgramReconcilerCommon: ClProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	default:
		return nil, fmt.Errorf("unsupported bpf program type")
	}

	return rec, nil
}

func (r *ClBpfApplicationReconciler) checkProgramStatus() bpfmaniov1alpha1.BpfApplicationStateConditionType {
	for _, program := range r.currentAppState.Spec.Programs {
		if program.ProgramLinkStatus != bpfmaniov1alpha1.ProgAttachSuccess {
			return bpfmaniov1alpha1.BpfAppStateCondError
		}
	}
	return bpfmaniov1alpha1.BpfAppStateCondSuccess
}

// getProgState returns the BpfApplicationProgramState object for the current node.
func (r *ClBpfApplicationReconciler) getProgState(prog *bpfmaniov1alpha1.ClBpfApplicationProgram,
	programs []bpfmaniov1alpha1.ClBpfApplicationProgramState) (*bpfmaniov1alpha1.ClBpfApplicationProgramState, error) {
	for i := range programs {
		progState := &programs[i]
		if progState.Type == prog.Type && progState.Name == prog.Name {
			switch prog.Type {
			case bpfmaniov1alpha1.ProgTypeFentry:
				if progState.FentryInfo.Function == prog.FentryInfo.Function {
					return progState, nil
				}
			case bpfmaniov1alpha1.ProgTypeFexit:
				if progState.FexitInfo.Function == prog.FexitInfo.Function {
					return progState, nil
				}
			default:
				return progState, nil
			}
		}
	}
	return nil, fmt.Errorf("BpfApplicationProgramState not found")
}

// updateBpfAppStateSpec creates or updates the BpfApplicationState object if it is
// new or has changed. It returns true if the object was created or updated, and
// an error if the API call fails. If true is returned without an error, the
// reconciler should return immediately because a new reconcile will be
// triggered.  If an error is returned, the code should return and request a
// requeue because it's uncertain whether a reconcile will be triggered.  If
// false is returned without an error, the reconciler may continue reconciling
// because nothing was changed.
func (r *ClBpfApplicationReconciler) updateBpfAppStateSpec(ctx context.Context, originalAppState *bpfmaniov1alpha1.ClusterBpfApplicationState,
	bpfAppStateNew bool) (bool, error) {

	// We've completed reconciling this program and something has
	// changed.  We need to create or update the BpfApplicationState.
	if bpfAppStateNew {
		// Create a new BpfApplicationState
		r.currentAppState.Spec.UpdateCount = 1
		r.Logger.Info("Creating new BpfApplicationState object", "Name", r.currentAppState.Name,
			"bpfAppStateNew", bpfAppStateNew, "UpdateCount", r.currentAppState.Spec.UpdateCount)
		if err := r.Create(ctx, r.currentAppState); err != nil {
			r.Logger.Error(err, "failed to create BpfApplicationState")
			return true, err
		}
		return r.waitForBpfAppStateUpdate(ctx)
	} else if !reflect.DeepEqual(originalAppState.Spec, r.currentAppState.Spec) {
		// Update the BpfApplicationState
		r.currentAppState.Spec.UpdateCount = r.currentAppState.Spec.UpdateCount + 1
		r.Logger.Info("Updating BpfApplicationState object", "Name", r.currentAppState.Name, "bpfAppStateNew", bpfAppStateNew, "UpdateCount", r.currentAppState.Spec.UpdateCount)
		if err := r.Update(ctx, r.currentAppState); err != nil {
			r.Logger.Error(err, "failed to update BpfApplicationState")
			return true, err
		}
		return r.waitForBpfAppStateUpdate(ctx)
	}
	return false, nil
}

// waitForBpfAppStateUpdate waits for the new BpfApplicationState object to be ready.
// bpfman saves state in the BpfApplicationState object that controls what needs
// to be done, so it is critical for each reconcile attempt to have the updated
// information. However, it takes time for objects to be created or updated, and
// for the API server to be able to return the update.  I've seen cases where
// the new object isn't ready when a reconcile is launched too soon after an
// update. A field called "UpdateCount" is used to ensure we get the updated
// object.  Kubernetes maintains a similar value called "Generation" which we
// might be able to use instead, but I'm not 100% sure I can trust it yet. When
// waitForBpfAppStateUpdate gets the updated object, it also updates r.currentAppState
// so the object can be used for subsequent operations (like a status update).
// From observations so far on kind, the updated object is sometimes ready on
// the first try, and sometimes it takes one more try.  I've not seen it take
// more than one retry.  waitForBpfAppStateUpdate currently waits for up to 10 seconds
// (100 * 100ms).
func (r *ClBpfApplicationReconciler) waitForBpfAppStateUpdate(ctx context.Context) (bool, error) {
	const maxRetries = 100
	const retryInterval = 100 * time.Millisecond

	var bpfAppState *bpfmaniov1alpha1.ClusterBpfApplicationState
	var err error
	r.Logger.Info("waitForBpfAppStateUpdate()", "UpdateCount", r.currentAppState.Spec.UpdateCount, "currentGeneration", r.currentAppState.GetGeneration())

	for i := 0; i < maxRetries; i++ {
		bpfAppState, _, err = r.getBpfAppState(ctx, false)
		if err != nil {
			// If we get an error, we'll just log it and keep trying.
			r.Logger.Info("Error getting BpfApplicationState", "Attempt", i, "error", err)
		} else if bpfAppState != nil && bpfAppState.Spec.UpdateCount >= r.currentAppState.Spec.UpdateCount {
			r.Logger.Info("Found new bpfAppState Spec", "Attempt", i, "UpdateCount", bpfAppState.Spec.UpdateCount,
				"currentGeneration", bpfAppState.GetGeneration())
			r.currentAppState = bpfAppState
			return true, nil
		}
		time.Sleep(retryInterval)
	}

	r.Logger.Info("Didn't find new BpfApplicationState", "Attempts", maxRetries)
	return false, fmt.Errorf("failed to get new BpfApplicationState after %d retries", maxRetries)
}

// See waitForBpfAppStateUpdate() for an explanation of why this function is needed.
func (r *ClBpfApplicationReconciler) waitForBpfAppStateStatusUpdate(ctx context.Context) error {
	const maxRetries = 100
	const retryInterval = 100 * time.Millisecond

	var bpfAppState *bpfmaniov1alpha1.ClusterBpfApplicationState
	var err error
	r.Logger.Info("waitForBpfAppStateStatusUpdate()", "UpdateCount", r.currentAppState.Spec.UpdateCount,
		"currentGeneration", r.currentAppState.GetGeneration())

	for i := 0; i < maxRetries; i++ {
		bpfAppState, _, err = r.getBpfAppState(ctx, false)
		if err != nil {
			// If we get an error, we'll just log it and keep trying.
			r.Logger.Info("Error getting BpfApplicationState", "Attempt", i, "error", err)
		} else if bpfAppState != nil && len(bpfAppState.Status.Conditions) > 0 &&
			bpfAppState.Status.Conditions[0].Type == r.currentAppState.Status.Conditions[0].Type {
			r.Logger.Info("Found new bpfAppState Status", "Attempt", i, "UpdateCount", bpfAppState.Spec.UpdateCount,
				"currentGeneration", bpfAppState.GetGeneration())
			r.currentAppState = bpfAppState
			return nil
		}
		time.Sleep(retryInterval)
	}

	r.Logger.Info("Didn't find new BpfApplicationState", "Attempts", maxRetries)
	return fmt.Errorf("failed to get new BpfApplicationState after %d retries", maxRetries)
}

// getBpfAppState returns the BpfApplicationState object for the current node. If
// needed to be created, the returned bool will be true.  Otherwise, it will be false.
func (r *ClBpfApplicationReconciler) getBpfAppState(ctx context.Context, createIfNotFound bool) (*bpfmaniov1alpha1.ClusterBpfApplicationState, bool, error) {

	appProgramList := &bpfmaniov1alpha1.ClusterBpfApplicationStateList{}

	opts := []client.ListOption{
		client.MatchingLabels{
			internal.BpfAppStateOwner: r.currentApp.GetName(),
			internal.K8sHostLabel:     r.NodeName,
		},
	}

	err := r.List(ctx, appProgramList, opts...)
	if err != nil {
		return nil, false, err
	}

	if len(appProgramList.Items) == 1 {
		// We got exatly one BpfApplicationState, so return it
		return &appProgramList.Items[0], false, nil
	}
	if len(appProgramList.Items) > 1 {
		// This should never happen, but if it does, return an error
		return nil, false, fmt.Errorf("more than one BpfApplicationState found (%d)", len(appProgramList.Items))
	}
	// There are no BpfApplicationStates for this BpfApplication on this node.
	if createIfNotFound {
		return r.createBpfAppState()
	} else {
		return nil, false, nil
	}
}

func (r *ClBpfApplicationReconciler) createBpfAppState() (*bpfmaniov1alpha1.ClusterBpfApplicationState, bool, error) {
	bpfAppState := &bpfmaniov1alpha1.ClusterBpfApplicationState{
		ObjectMeta: metav1.ObjectMeta{
			Name:       generateUniqueName(r.currentApp.Name),
			Finalizers: []string{r.finalizer},
			Labels: map[string]string{
				internal.BpfAppStateOwner: r.currentApp.GetName(),
				internal.K8sHostLabel:     r.NodeName,
			},
		},
		Spec: bpfmaniov1alpha1.ClBpfApplicationStateSpec{
			Node:          r.NodeName,
			AppLoadStatus: bpfmaniov1alpha1.AppLoadNotLoaded,
			UpdateCount:   0,
			Programs:      []bpfmaniov1alpha1.ClBpfApplicationProgramState{},
		},
		Status: bpfmaniov1alpha1.BpfAppStatus{Conditions: []metav1.Condition{}},
	}

	err := r.initializeNodeProgramList(bpfAppState)
	if err != nil {
		return nil, false, fmt.Errorf("failed to initialize BpfApplicationState program list: %v", err)
	}

	// Make the corresponding BpfApplication the owner
	if err := ctrl.SetControllerReference(r.currentApp, bpfAppState, r.Scheme); err != nil {
		return nil, false, fmt.Errorf("failed to set bpfAppState object owner reference: %v", err)
	}

	return bpfAppState, true, nil
}

func (r *ClBpfApplicationReconciler) initializeNodeProgramList(bpfAppState *bpfmaniov1alpha1.ClusterBpfApplicationState) error {
	// The list should only be initialized once when the BpfApplication is first
	// created.  After that, the user can't add or remove programs.
	if len(bpfAppState.Spec.Programs) != 0 {
		return fmt.Errorf("BpfApplicationState programs list has already been initialized")
	}

	for _, prog := range r.currentApp.Spec.Programs {
		_, err := r.getProgState(&prog, bpfAppState.Spec.Programs)
		if err == nil {
			return fmt.Errorf("duplicate bpf function detected. bpfFunctionName: %s", prog.Name)
		}
		progState := bpfmaniov1alpha1.ClBpfApplicationProgramState{
			BpfProgramStateCommon: bpfmaniov1alpha1.BpfProgramStateCommon{
				Name:              prog.Name,
				ProgramLinkStatus: bpfmaniov1alpha1.ProgAttachPending,
			},
			Type: prog.Type,
		}
		switch prog.Type {
		case bpfmaniov1alpha1.ProgTypeFentry:
			progState.FentryInfo = &bpfmaniov1alpha1.ClFentryProgramInfoState{
				ClFentryLoadInfo: prog.FentryInfo.ClFentryLoadInfo,
				Links:            []bpfmaniov1alpha1.ClFentryAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeFexit:
			progState.FexitInfo = &bpfmaniov1alpha1.ClFexitProgramInfoState{
				ClFexitLoadInfo: prog.FexitInfo.ClFexitLoadInfo,
				Links:           []bpfmaniov1alpha1.ClFexitAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeKprobe:
			progState.KprobeInfo = &bpfmaniov1alpha1.ClKprobeProgramInfoState{
				Links: []bpfmaniov1alpha1.ClKprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeKretprobe:
			progState.KretprobeInfo = &bpfmaniov1alpha1.ClKprobeProgramInfoState{
				Links: []bpfmaniov1alpha1.ClKprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeTC:
			progState.TCInfo = &bpfmaniov1alpha1.ClTcProgramInfoState{
				Links: []bpfmaniov1alpha1.ClTcAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeTCX:
			progState.TCXInfo = &bpfmaniov1alpha1.ClTcxProgramInfoState{
				Links: []bpfmaniov1alpha1.ClTcxAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeTracepoint:
			progState.TracepointInfo = &bpfmaniov1alpha1.ClTracepointProgramInfoState{
				Links: []bpfmaniov1alpha1.ClTracepointAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeUprobe:
			progState.UprobeInfo = &bpfmaniov1alpha1.ClUprobeProgramInfoState{
				Links: []bpfmaniov1alpha1.ClUprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeUretprobe:
			progState.UretprobeInfo = &bpfmaniov1alpha1.ClUprobeProgramInfoState{
				Links: []bpfmaniov1alpha1.ClUprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeXDP:
			progState.XDPInfo = &bpfmaniov1alpha1.ClXdpProgramInfoState{
				Links: []bpfmaniov1alpha1.ClXdpAttachInfoState{},
			}

		default:
			return fmt.Errorf("unexpected EBPFProgType: %#v", prog.Type)
		}

		bpfAppState.Spec.Programs = append(bpfAppState.Spec.Programs, progState)
	}

	return nil
}

func (r *ClBpfApplicationReconciler) isLoaded(ctx context.Context) bool {
	allProgramsLoaded := true
	someProgramsLoaded := false
	for _, program := range r.currentAppState.Spec.Programs {
		if program.ProgramId == nil {
			allProgramsLoaded = false
		} else if _, err := bpfmanagentinternal.GetBpfmanProgramById(ctx, r.BpfmanClient, *program.ProgramId); err != nil {
			allProgramsLoaded = false
		} else {
			someProgramsLoaded = true
		}
	}

	if allProgramsLoaded != someProgramsLoaded {
		// This should never happen because the bpfman load is all or nothing,
		// and we aren't allowing users to add or remove programs from an
		// existing BpfApplication.  However, if it does happen, log an error.
		r.Logger.Error(fmt.Errorf("inconsistent program load state"),
			"allProgramsLoaded", allProgramsLoaded, "someProgramsLoaded", someProgramsLoaded)
	}

	return allProgramsLoaded
}

func (r *ClBpfApplicationReconciler) getLoadRequest() (*gobpfman.LoadRequest, error) {

	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentApp.Spec.BpfAppCommon.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadInfo := []*gobpfman.LoadInfo{}

	for _, program := range r.currentApp.Spec.Programs {
		progState, err := r.getProgState(&program, r.currentAppState.Spec.Programs)
		if err != nil {
			return nil, fmt.Errorf("failed to get program state: %v", err)
		}
		progRec, err := r.getProgramReconciler(&program, progState)
		if err != nil {
			return nil, fmt.Errorf("failed to get program reconciler: %v", err)
		}
		programLoadInfo := progRec.getProgramLoadInfo()
		loadInfo = append(loadInfo, programLoadInfo)
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:   bytecode,
		Metadata:   map[string]string{internal.UuidMetadataKey: string(r.currentAppState.UID), internal.ProgramNameKey: r.currentApp.Name},
		GlobalData: r.currentApp.Spec.GlobalData,
		Uuid:       new(string),
		// TODO: Get map owner ID working.  For now, just pass nil.
		// See: https://github.com/bpfman/bpfman-operator/issues/393
		MapOwnerId: nil,
		Info:       loadInfo,
	}

	return &loadRequest, nil
}

func (r *ClBpfApplicationReconciler) load(ctx context.Context) error {
	loadRequest, err := r.getLoadRequest()
	if err != nil {
		return fmt.Errorf("failed to get LoadRequest")
	}

	loadResponse, err := r.BpfmanClient.Load(ctx, loadRequest)
	if err != nil {
		return fmt.Errorf("failed to load eBPF Program: %v", err)
	} else {
		for p, program := range r.currentAppState.Spec.Programs {
			id, err := bpfmanagentinternal.GetBpfProgramId(program.Name, loadResponse.Programs)
			// This should never happen because the bpfman load is all or nothing,
			// and we aren't allowing users to add or remove programs from an
			// existing BpfApplication.  However, if it does happen, log an error.
			r.Logger.Info("Programs", "Program", program.Name, "ProgramId", id)
			if err != nil {
				return fmt.Errorf("failed to get program id: %v", err)
			}
			r.currentAppState.Spec.Programs[p].ProgramId = id
		}
	}
	return nil
}

func (r *ClBpfApplicationReconciler) unload(ctx context.Context) {
	for i, program := range r.currentAppState.Spec.Programs {
		if program.ProgramId != nil {
			err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *program.ProgramId)
			if err != nil {
				// This should never happen under normal operations.  However,
				// it is possible that someone unloaded the program manually. In
				// that case, we should log the error and continue.
				r.Logger.Error(err, "failed to unload program", "ProgramId", *program.ProgramId)
			}
		}
		r.currentAppState.Spec.Programs[i].ProgramId = nil
		// When bpfman deletes a program, it also automatically detaches all links, so,
		// we can just delete the links from the state.
		r.deleteLinks(&r.currentAppState.Spec.Programs[i])
		r.currentAppState.Spec.Programs[i].ProgramLinkStatus = bpfmaniov1alpha1.ProgAttachSuccess
	}
}

func (r *ClBpfApplicationReconciler) deleteLinks(program *bpfmaniov1alpha1.ClBpfApplicationProgramState) {
	switch program.Type {
	case bpfmaniov1alpha1.ProgTypeFentry:
		program.FentryInfo.Links = []bpfmaniov1alpha1.ClFentryAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeFexit:
		program.FexitInfo.Links = []bpfmaniov1alpha1.ClFexitAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeKprobe:
		program.KprobeInfo.Links = []bpfmaniov1alpha1.ClKprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeKretprobe:
		program.KretprobeInfo.Links = []bpfmaniov1alpha1.ClKprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeTC:
		program.TCInfo.Links = []bpfmaniov1alpha1.ClTcAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeTCX:
		program.TCXInfo.Links = []bpfmaniov1alpha1.ClTcxAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeTracepoint:
		program.TracepointInfo.Links = []bpfmaniov1alpha1.ClTracepointAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeUprobe:
		program.UprobeInfo.Links = []bpfmaniov1alpha1.ClUprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeUretprobe:
		program.UretprobeInfo.Links = []bpfmaniov1alpha1.ClUprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeXDP:
		program.XDPInfo.Links = []bpfmaniov1alpha1.ClXdpAttachInfoState{}
	default:
		r.Logger.Error(fmt.Errorf("unexpected EBPFProgType"), "unexpected EBPFProgType", "Type", program.Type)
	}
}

// validateProgramList checks the BpfApplicationPrograms to ensure that none
// have been added or deleted.
func (r *ClBpfApplicationReconciler) validateProgramList() error {
	// Create a map of the programs in r.currentAppState.Spec.Programs to make
	// the checks more efficient.
	appStateProgMap := make(map[string]bool)
	for _, program := range r.currentAppState.Spec.Programs {
		appStateProgMap[program.Name] = true
	}

	// Check that all the programs in r.currentApp.Spec.Programs are on the
	// list.  If not, that indicates that the program has been added, which is
	// not allowed.  Remove them if they are on the list so we can check if any
	// are left over which would indicate that they have been removed from the
	// list.
	addedPrograms := ""
	for _, program := range r.currentApp.Spec.Programs {
		if _, ok := appStateProgMap[program.Name]; !ok {
			addedPrograms = addedPrograms + program.Name + " "
		} else {
			delete(appStateProgMap, program.Name)
		}
	}

	if addedPrograms != "" {
		return fmt.Errorf("programs have been added: %s", addedPrograms)
	}

	// Now, see if there are any programs left on the list, which would indicate that
	// they have been removed from the list.
	if len(appStateProgMap) > 0 {
		// create a string containing the names of the programs that have been removed
		removedPrograms := ""
		for program := range appStateProgMap {
			removedPrograms = removedPrograms + program + " "
		}
		return fmt.Errorf("programs have been removed: %s", removedPrograms)
	}

	return nil
}
