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

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates/finalizers,verbs=update
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get

type NsBpfApplicationReconciler struct {
	ReconcilerCommon
	currentApp      *bpfmaniov1alpha1.BpfApplication
	currentAppState *bpfmaniov1alpha1.BpfApplicationState
}

type NsProgramReconcilerCommon struct {
	currentProgram      *bpfmaniov1alpha1.BpfApplicationProgram
	currentProgramState *bpfmaniov1alpha1.BpfApplicationProgramState
	namespace           string
}

func (r *NsBpfApplicationReconciler) getAppStateName() string {
	return r.currentAppState.Name
}

func (r *NsBpfApplicationReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *NsBpfApplicationReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentApp.Spec.NodeSelector
}

func (r *NsBpfApplicationReconciler) GetStatus() *bpfmaniov1alpha1.BpfAppStatus {
	return &r.currentAppState.Status
}

func (r *NsBpfApplicationReconciler) isBeingDeleted() bool {
	return !r.currentApp.GetDeletionTimestamp().IsZero()
}

func (r *NsBpfApplicationReconciler) updateBpfAppStatus(ctx context.Context, condition metav1.Condition) error {
	r.currentAppState.Status.Conditions = nil
	meta.SetStatusCondition(&r.currentAppState.Status.Conditions, condition)
	err := r.Status().Update(ctx, r.currentAppState)
	if err != nil {
		return err
	} else {
		return r.waitForBpfAppStateStatusUpdate(ctx)
	}
}

func (r *NsBpfApplicationReconciler) updateLoadStatus(status bpfmaniov1alpha1.AppLoadStatus) {
	r.currentAppState.Spec.AppLoadStatus = status
}

// SetupWithManager sets up the controller with the Manager. The Bpfman-Agent
// should reconcile whenever a BpfNsApplication object is updated, load/unload bpf
// programs on the node via bpfman, and create or update a BpfNsApplicationState
// object to reflect per node state information.
func (r *NsBpfApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.BpfApplication{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Owns(&bpfmaniov1alpha1.BpfApplicationState{},
			builder.WithPredicates(internal.BpfNodePredicate(r.NodeName)),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the BpfNsApplication no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&v1.Node{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		// Watch for changes in Pod resources in case we are using a container
		// or network namespace selector.
		Watches(
			&v1.Pod{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(podOnNodePredicate(r.NodeName)),
		).
		Complete(r)
}

func (r *NsBpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("namespace-app")
	r.finalizer = internal.NsBpfApplicationControllerFinalizer
	r.recType = internal.ApplicationString

	r.Logger.Info("Enter BpfNsApplication Reconcile", "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	// Get the list of existing BpfNsApplication objects
	appPrograms := &bpfmaniov1alpha1.BpfApplicationList{}
	opts := []client.ListOption{}
	if err := r.List(ctx, appPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfNsApplicationPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}
	if len(appPrograms.Items) == 0 {
		r.Logger.Info("BpfNsApplicationController found no application Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	for appProgramIndex := range appPrograms.Items {
		r.currentApp = &appPrograms.Items[appProgramIndex]

		r.Logger.Info("Reconciling BpfApplication", "Name", r.currentApp.Name)

		// Get the corresponding BpfNsApplicationState object, and if it doesn't
		// exist, instantiate a copy. If bpfAppStateNew is true, then we need to
		// create a new BpfNsApplicationState instead of just updating the
		// existing one.
		appState, bpfAppStateNew, err := r.getBpfAppState(ctx, !r.isBeingDeleted())
		if err != nil {
			r.Logger.Error(err, "failed to get BpfNsApplicationState")
			return ctrl.Result{}, err
		}

		if appState == nil && r.isBeingDeleted() {
			// If the BpfApplicationState doesn't exist and the BpfApplication
			// is being deleted, we don't need to do anything.  Just continue
			// with the next BpfApplication.
			r.Logger.Info("BpfApplicationState doesn't exist and BpfApplication is being deleted",
				"Name", r.currentApp.Name)
			continue
		}

		r.currentAppState = appState

		// Save a copy of the original BpfNsApplicationState to check for changes
		// at the end of the reconcile process. This approach simplifies the
		// code and reduces the risk of errors by avoiding the need to track
		// changes throughout.  We don't need to do this for new
		// BpfNsApplicationStates because they don't exist yet and will need to be
		// created anyway.
		var bpfAppStateOriginal *bpfmaniov1alpha1.BpfApplicationState
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
		r.Logger.Info("Calling reconcileLoad()", "isBeingDeleted", r.isBeingDeleted())
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
				// If nothing changed, continue with the next BpfNsApplication.
				// Otherwise, one bad BpfNsApplication can block the rest.
				continue
			}
		}

		// Initialize the BpfNsApplicationState status to Success.  It will be set
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
		// changed, we need to create or update the BpfNsApplicationState.
		specChanged, err := r.updateBpfAppStateSpec(ctx, bpfAppStateOriginal, bpfAppStateNew)
		if err != nil {
			r.Logger.Error(err, "failed to update BpfNsApplicationState", "Name", r.currentAppState.Name)
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
			r.Logger.Info("BpfNsApplicationState updated", "Name", r.currentAppState.Name, "Spec Changed",
				specChanged, "Status Changed", statusChanged)
			return ctrl.Result{}, nil
		}

		if r.isBeingDeleted() {
			r.Logger.Info("BpfNsApplication is being deleted", "Name", r.currentApp.Name)
			if r.removeFinalizer(ctx, r.currentAppState, r.finalizer) {
				return ctrl.Result{}, nil
			}
		}

		// Nothing changed, so continue with next BpfNsApplication object.
		r.Logger.Info("No changes to BpfNsApplicationState object", "Name", r.currentAppState.Name)
	}

	// We're done with all the BpfNsApplication objects, so we can return.
	r.Logger.Info("All BpfNsApplication objects have been reconciled")
	return ctrl.Result{}, nil
}

func (r *NsBpfApplicationReconciler) getProgramReconciler(prog *bpfmaniov1alpha1.BpfApplicationProgram,
	progState *bpfmaniov1alpha1.BpfApplicationProgramState) (ProgramReconciler, error) {

	var rec ProgramReconciler

	switch prog.Type {
	case bpfmaniov1alpha1.ProgTypeUprobe, bpfmaniov1alpha1.ProgTypeUretprobe:
		rec = &NsUprobeProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			NsProgramReconcilerCommon: NsProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTC:
		rec = &NsTcProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			NsProgramReconcilerCommon: NsProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTCX:
		rec = &NsTcxProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			NsProgramReconcilerCommon: NsProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeXDP:
		rec = &NsXdpProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			NsProgramReconcilerCommon: NsProgramReconcilerCommon{
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	default:
		return nil, fmt.Errorf("unsupported bpf program type")
	}

	return rec, nil
}

func (r *NsBpfApplicationReconciler) checkProgramStatus() bpfmaniov1alpha1.BpfApplicationStateConditionType {
	for _, program := range r.currentAppState.Spec.Programs {
		if program.ProgramLinkStatus != bpfmaniov1alpha1.ProgAttachSuccess {
			return bpfmaniov1alpha1.BpfAppStateCondError
		}
	}
	return bpfmaniov1alpha1.BpfAppStateCondSuccess
}

// getProgState returns the BpfNsApplicationProgramState object for the current node.
func (r *NsBpfApplicationReconciler) getProgState(prog *bpfmaniov1alpha1.BpfApplicationProgram,
	programs []bpfmaniov1alpha1.BpfApplicationProgramState) (*bpfmaniov1alpha1.BpfApplicationProgramState, error) {
	for i := range programs {
		progState := &programs[i]
		if progState.Type == prog.Type && progState.Name == prog.Name {
			return progState, nil
		}
	}
	return nil, fmt.Errorf("BpfNsApplicationProgramState not found")
}

// updateBpfAppStateSpec creates or updates the BpfNsApplicationState object if it is
// new or has changed. It returns true if the object was created or updated, and
// an error if the API call fails. If true is returned without an error, the
// reconciler should return immediately because a new reconcile will be
// triggered.  If an error is returned, the code should return and request a
// requeue because it's uncertain whether a reconcile will be triggered.  If
// false is returned without an error, the reconciler may continue reconciling
// because nothing was changed.
func (r *NsBpfApplicationReconciler) updateBpfAppStateSpec(ctx context.Context, originalAppState *bpfmaniov1alpha1.BpfApplicationState,
	bpfAppStateNew bool) (bool, error) {

	// We've completed reconciling this program and something has
	// changed.  We need to create or update the BpfNsApplicationState.
	if bpfAppStateNew {
		// Create a new BpfNsApplicationState
		r.currentAppState.Spec.UpdateCount = 1
		r.Logger.Info("Creating new BpfNsApplicationState object", "Name", r.currentAppState.Name,
			"bpfAppStateNew", bpfAppStateNew, "UpdateCount", r.currentAppState.Spec.UpdateCount)
		if err := r.Create(ctx, r.currentAppState); err != nil {
			r.Logger.Error(err, "failed to create BpfNsApplicationState")
			return true, err
		}
		return r.waitForBpfAppStateUpdate(ctx)
	} else if !reflect.DeepEqual(originalAppState.Spec, r.currentAppState.Spec) {
		// Update the BpfNsApplicationState
		r.currentAppState.Spec.UpdateCount = r.currentAppState.Spec.UpdateCount + 1
		r.Logger.Info("Updating BpfNsApplicationState object", "Name", r.currentAppState.Name, "bpfAppStateNew", bpfAppStateNew, "UpdateCount", r.currentAppState.Spec.UpdateCount)
		if err := r.Update(ctx, r.currentAppState); err != nil {
			r.Logger.Error(err, "failed to update BpfNsApplicationState")
			return true, err
		}
		return r.waitForBpfAppStateUpdate(ctx)
	}
	return false, nil
}

// waitForBpfAppStateUpdate waits for the new BpfNsApplicationState object to be ready.
// bpfman saves state in the BpfNsApplicationState object that controls what needs
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
func (r *NsBpfApplicationReconciler) waitForBpfAppStateUpdate(ctx context.Context) (bool, error) {
	const maxRetries = 100
	const retryInterval = 100 * time.Millisecond

	var bpfAppState *bpfmaniov1alpha1.BpfApplicationState
	var err error
	r.Logger.Info("waitForBpfAppStateUpdate()", "UpdateCount", r.currentAppState.Spec.UpdateCount, "currentGeneration", r.currentAppState.GetGeneration())

	for i := 0; i < maxRetries; i++ {
		bpfAppState, _, err = r.getBpfAppState(ctx, false)
		if err != nil {
			// If we get an error, we'll just log it and keep trying.
			r.Logger.Info("Error getting BpfNsApplicationState", "Attempt", i, "error", err)
		} else if bpfAppState != nil && bpfAppState.Spec.UpdateCount >= r.currentAppState.Spec.UpdateCount {
			r.Logger.Info("Found new bpfAppState Spec", "Attempt", i, "UpdateCount", bpfAppState.Spec.UpdateCount,
				"currentGeneration", bpfAppState.GetGeneration())
			r.currentAppState = bpfAppState
			return true, nil
		}
		time.Sleep(retryInterval)
	}

	r.Logger.Info("Didn't find new BpfNsApplicationState", "Attempts", maxRetries)
	return false, fmt.Errorf("failed to get new BpfNsApplicationState after %d retries", maxRetries)
}

// See waitForBpfAppStateUpdate() for an explanation of why this function is needed.
func (r *NsBpfApplicationReconciler) waitForBpfAppStateStatusUpdate(ctx context.Context) error {
	const maxRetries = 100
	const retryInterval = 100 * time.Millisecond

	var bpfAppState *bpfmaniov1alpha1.BpfApplicationState
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

// getBpfAppState returns the BpfNsApplicationState object for the current node. If
// needed to be created, the returned bool will be true.  Otherwise, it will be false.
func (r *NsBpfApplicationReconciler) getBpfAppState(ctx context.Context, createIfNotFound bool) (*bpfmaniov1alpha1.BpfApplicationState, bool, error) {

	appProgramList := &bpfmaniov1alpha1.BpfApplicationStateList{}

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
		// We got exatly one BpfNsApplicationState, so return it
		return &appProgramList.Items[0], false, nil
	}
	if len(appProgramList.Items) > 1 {
		// This should never happen, but if it does, return an error
		return nil, false, fmt.Errorf("more than one BpfNsApplicationState found (%d)", len(appProgramList.Items))
	}
	// There are no BpfNsApplicationStates for this BpfNsApplication on this node.
	if createIfNotFound {
		return r.createBpfAppState()
	} else {
		return nil, false, nil
	}
}

func (r *NsBpfApplicationReconciler) createBpfAppState() (*bpfmaniov1alpha1.BpfApplicationState, bool, error) {
	bpfAppState := &bpfmaniov1alpha1.BpfApplicationState{
		ObjectMeta: metav1.ObjectMeta{
			Name:       generateUniqueName(r.currentApp.Name),
			Namespace:  r.currentApp.Namespace,
			Finalizers: []string{r.finalizer},
			Labels: map[string]string{
				internal.BpfAppStateOwner: r.currentApp.GetName(),
				internal.K8sHostLabel:     r.NodeName,
			},
		},
		Spec: bpfmaniov1alpha1.BpfApplicationStateSpec{
			Node:          r.NodeName,
			AppLoadStatus: bpfmaniov1alpha1.AppLoadNotLoaded,
			UpdateCount:   0,
			Programs:      []bpfmaniov1alpha1.BpfApplicationProgramState{},
		},
		Status: bpfmaniov1alpha1.BpfAppStatus{Conditions: []metav1.Condition{}},
	}

	err := r.initializeNodeProgramList(bpfAppState)
	if err != nil {
		return nil, false, fmt.Errorf("failed to initialize BpfNsApplicationState program list: %v", err)
	}

	// Make the corresponding BpfApplication the owner
	if err := ctrl.SetControllerReference(r.currentApp, bpfAppState, r.Scheme); err != nil {
		return nil, false, fmt.Errorf("failed to set bpfAppState object owner reference: %v", err)
	}

	return bpfAppState, true, nil
}

func (r *NsBpfApplicationReconciler) initializeNodeProgramList(bpfAppState *bpfmaniov1alpha1.BpfApplicationState) error {
	// The list should only be initialized once when the BpfNsApplication is first
	// created.  After that, the user can't add or remove programs.
	if len(bpfAppState.Spec.Programs) != 0 {
		return fmt.Errorf("BpfNsApplicationState programs list has already been initialized")
	}

	for _, prog := range r.currentApp.Spec.Programs {
		// Check if it's already on the list.  If it is, this is an error
		// because a given bpf function can only be loaded once per
		// BpfNsApplication.
		_, err := r.getProgState(&prog, bpfAppState.Spec.Programs)
		if err == nil {
			return fmt.Errorf("duplicate bpf function detected. bpfFunctionName: %s", prog.Name)
		}
		progState := bpfmaniov1alpha1.BpfApplicationProgramState{
			BpfProgramStateCommon: bpfmaniov1alpha1.BpfProgramStateCommon{
				Name:              prog.Name,
				ProgramLinkStatus: bpfmaniov1alpha1.ProgAttachPending,
			},
			Type: prog.Type,
		}
		switch prog.Type {
		case bpfmaniov1alpha1.ProgTypeTC:
			progState.TC = &bpfmaniov1alpha1.TcProgramInfoState{
				Links: []bpfmaniov1alpha1.TcAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeTCX:
			progState.TCX = &bpfmaniov1alpha1.TcxProgramInfoState{
				Links: []bpfmaniov1alpha1.TcxAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeUprobe:
			progState.UProbe = &bpfmaniov1alpha1.UprobeProgramInfoState{
				Links: []bpfmaniov1alpha1.UprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeUretprobe:
			progState.URetProbe = &bpfmaniov1alpha1.UprobeProgramInfoState{
				Links: []bpfmaniov1alpha1.UprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeXDP:
			progState.XDP = &bpfmaniov1alpha1.XdpProgramInfoState{
				Links: []bpfmaniov1alpha1.XdpAttachInfoState{},
			}

		default:
			panic(fmt.Sprintf("unexpected EBPFProgType: %#v", prog.Type))
		}

		bpfAppState.Spec.Programs = append(bpfAppState.Spec.Programs, progState)
	}

	return nil
}

func (r *NsBpfApplicationReconciler) isLoaded(ctx context.Context) bool {
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

func (r *NsBpfApplicationReconciler) getLoadRequest() (*gobpfman.LoadRequest, error) {

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

func (r *NsBpfApplicationReconciler) load(ctx context.Context) error {
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

func (r *NsBpfApplicationReconciler) unload(ctx context.Context) {
	for i, program := range r.currentAppState.Spec.Programs {
		if program.ProgramId != nil {
			err := bpfmanagentinternal.UnloadBpfmanProgram(ctx, r.BpfmanClient, *program.ProgramId)
			if err != nil {
				// This should never happen under normal operations.  However,
				// it is possible that someone unloaded the program manually. In
				// that case, we should log the error and continue.
				r.Logger.Error(err, "failed to unload program", "ProgramId", *program.ProgramId)
			}
			r.currentAppState.Spec.Programs[i].ProgramId = nil
			// When bpfman deletes a program, it also automatically detaches all links, so,
			// we can just delete the links from the state.
			r.deleteLinks(&r.currentAppState.Spec.Programs[i])
		}
		r.currentAppState.Spec.Programs[i].ProgramLinkStatus = bpfmaniov1alpha1.ProgAttachSuccess
	}
}

func (r *NsBpfApplicationReconciler) deleteLinks(program *bpfmaniov1alpha1.BpfApplicationProgramState) {
	switch program.Type {
	case bpfmaniov1alpha1.ProgTypeTC:
		program.TC.Links = []bpfmaniov1alpha1.TcAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeTCX:
		program.TCX.Links = []bpfmaniov1alpha1.TcxAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeUprobe:
		program.UProbe.Links = []bpfmaniov1alpha1.UprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeUretprobe:
		program.URetProbe.Links = []bpfmaniov1alpha1.UprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeXDP:
		program.XDP.Links = []bpfmaniov1alpha1.XdpAttachInfoState{}
	default:
		r.Logger.Error(fmt.Errorf("unexpected EBPFProgType"), "unexpected EBPFProgType", "Type", program.Type)
	}
}

// validateProgramList checks the BpfApplicationPrograms to ensure that none
// have been added or deleted.
func (r *NsBpfApplicationReconciler) validateProgramList() error {
	if len(r.currentAppState.Spec.Programs) != len(r.currentApp.Spec.Programs) {
		return fmt.Errorf("program list has changed")
	}

	// Create a map of the programs in r.currentAppState.Spec.Programs so we can
	// quickly check if a program is in the list.
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
