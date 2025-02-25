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
	"github.com/google/uuid"

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

type BpfApplicationReconciler struct {
	ReconcilerCommon
	currentApp      *bpfmaniov1alpha1.BpfApplication
	currentAppState *bpfmaniov1alpha1.BpfApplicationState
}

type ProgramReconcilerCommon struct {
	// ANF-TODO: appCommon is needed to load the program. It won't be needed
	// after the load/attch split is ready.
	appCommon           bpfmaniov1alpha1.BpfAppCommon
	currentProgram      *bpfmaniov1alpha1.BpfApplicationProgram
	currentProgramState *bpfmaniov1alpha1.BpfApplicationProgramState
}

func (r *BpfApplicationReconciler) getAppStateName() string {
	return r.currentAppState.Name
}

func (r *BpfApplicationReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *BpfApplicationReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentApp.Spec.NodeSelector
}

func (r *BpfApplicationReconciler) GetStatus() *bpfmaniov1alpha1.BpfAppStatus {
	return &r.currentAppState.Status
}

func (r *BpfApplicationReconciler) isBeingDeleted() bool {
	return !r.currentApp.GetDeletionTimestamp().IsZero()
}

func (r *BpfApplicationReconciler) updateBpfAppStatus(ctx context.Context, condition metav1.Condition) error {
	r.currentAppState.Status.Conditions = nil
	meta.SetStatusCondition(&r.currentAppState.Status.Conditions, condition)
	err := r.Status().Update(ctx, r.currentAppState)
	if err != nil {
		return err
	} else {
		return r.waitForBpfAppStateStatusUpdate(ctx)
	}
}

func (r *BpfApplicationReconciler) updateLoadStatus(status bpfmaniov1alpha1.AppLoadStatus) {
	r.currentAppState.Spec.AppLoadStatus = status
}

// SetupWithManager sets up the controller with the Manager. The Bpfman-Agent
// should reconcile whenever a BpfApplication object is updated, load/unload bpf
// programs on the node via bpfman, and create or update a BpfApplicationState
// object to reflect per node state information.
func (r *BpfApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.BpfApplication{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Owns(&bpfmaniov1alpha1.BpfApplicationState{},
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

func (r *BpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("cluster-app")
	r.finalizer = internal.BpfApplicationControllerFinalizer
	r.recType = internal.ApplicationString

	r.Logger.Info("Enter BpfApplication Reconcile", "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	// Get the list of existing BpfApplication objects
	appPrograms := &bpfmaniov1alpha1.BpfApplicationList{}
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
		appProgram := &appPrograms.Items[appProgramIndex]
		// ANF-TODO: After load/attach split, we will need to load the code defined
		// in the BpfApplication here one time before we go through the list of
		// programs.  However, for now, we need to keep the current behavior and
		// load it for every attachment.

		r.currentApp = appProgram

		// Get the corresponding BpfApplicationState object, and if it doesn't
		// exist, instantiate a copy. If bpfAppStateNew is true, then we need to
		// create a new BpfApplicationState at the end of the reconcile
		// instead of just updating the existing one.
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
		var bpfAppStateOriginal *bpfmaniov1alpha1.BpfApplicationState
		if !bpfAppStateNew {
			bpfAppStateOriginal = r.currentAppState.DeepCopy()
		}

		r.Logger.Info("From getBpfAppState", "new", bpfAppStateNew)

		if bpfAppStateNew {
			// Create the object and return. We'll get the updated object in the
			// next reconcile.
			_, err := r.updateBpfAppStateSpec(ctx, bpfAppStateOriginal, bpfAppStateNew)
			if err != nil {
				r.Logger.Error(err, "failed to update BpfApplicationState", "Name", r.currentAppState.Name)
				_, _ = r.updateStatus(ctx, r, bpfmaniov1alpha1.ProgramReconcileError)
				// If there was an error updating the object, request a requeue
				// because we can't be sure what was updated and whether the manager
				// will requeue us without the request.
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
			} else {
				r.updateStatus(ctx, r, bpfmaniov1alpha1.ProgramNotYetLoaded)
				return ctrl.Result{}, nil
			}
		}

		// Make sure the BpfApplication code is loaded on the node.
		r.Logger.Info("Calling reconcileLoad()")
		err = r.reconcileLoad(ctx, r)
		if err != nil {
			// There's no point continuing to reconcile the attachments if we
			// can't load the code.
			r.Logger.Error(err, "failed to reconcileLoad")
			objectChanged, _ := r.updateBpfAppStateSpec(ctx, bpfAppStateOriginal, bpfAppStateNew)
			statusChanged, _ := r.updateStatus(ctx, r, bpfmaniov1alpha1.ProgramReconcileError)
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
		bpfApplicationStatus := bpfmaniov1alpha1.ProgramReconcileSuccess

		// If the BpfApplication is being deleted, all of the links would have
		// been detached when the programs are unloaded in the reconcileLoad()
		// operation, so we don't need to reconcile each program.
		if !r.isBeingDeleted() {
			// Reconcile each program in the BpfApplication
			for progIndex := range appProgram.Spec.Programs {
				prog := &appProgram.Spec.Programs[progIndex]
				progState, err := r.getProgState(prog, r.currentAppState.Spec.Programs)
				if err != nil {
					// ANF-TODO: This entry should have been created when the
					// BpfApplication was loaded.  If it's not here, then we need to
					// do another load, and we'll need to work out how to do that.
					// If we just do a load here for the new program, then it won't
					// share global data with the existing programs.  So, we need to
					// decide whether to just do an incremental load, or unload the
					// existing programs and reload everything.  In the future, we
					// may be able to add more seamless support for incremental
					// loads. However, for now, we're going to log an error and
					// continue.
					r.Logger.Error(fmt.Errorf("ProgramState not found"),
						"ProgramState not found", "App Name", r.currentApp.Name, "BpfFunctionName", prog.BpfFunctionName)
					// ANF-TODO: Make a special error for this.
					bpfApplicationStatus = bpfmaniov1alpha1.ProgramReconcileError
					continue
				}

				rec, err := r.getProgramReconciler(prog, progState)
				if err != nil {
					bpfApplicationStatus = bpfmaniov1alpha1.ProgramReconcileError
					r.Logger.Error(err, "error getting program reconciler", "Name", prog.BpfFunctionName)
					// Skip this program and continue to the next one
					continue
				}

				err = rec.reconcileProgram(ctx, rec, r.isBeingDeleted())
				if err != nil {
					r.Logger.Info("Error reconciling program", "Name", rec.getProgName(), "Index", appProgramIndex)
				} else {
					r.Logger.Info("Successfully reconciled program", "Name", rec.getProgName(), "Index", appProgramIndex)
				}
			}

			// If the bpfApplicationStatus didn't get changed to an error already,
			// check the status of the programs.
			if bpfApplicationStatus == bpfmaniov1alpha1.ProgramReconcileSuccess {
				bpfApplicationStatus = r.checkProgramStatus()
			}
		}

		// We've completed reconciling all programs and if something has
		// changed, we need to create or update the BpfApplicationState.
		specChanged, err := r.updateBpfAppStateSpec(ctx, bpfAppStateOriginal, bpfAppStateNew)
		if err != nil {
			r.Logger.Error(err, "failed to update BpfApplicationState", "Name", r.currentAppState.Name)
			_, _ = r.updateStatus(ctx, r, bpfmaniov1alpha1.ProgramReconcileError)
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

func (r *BpfApplicationReconciler) getProgramReconciler(prog *bpfmaniov1alpha1.BpfApplicationProgram,
	progState *bpfmaniov1alpha1.BpfApplicationProgramState) (ProgramReconciler, error) {

	var rec ProgramReconciler

	switch prog.Type {
	case bpfmaniov1alpha1.ProgTypeFentry:
		rec = &FentryProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeFexit:
		rec = &FexitProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeKprobe:
		rec = &KprobeProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeUprobe:
		rec = &UprobeProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTracepoint:
		rec = &TracepointProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTC:
		rec = &TcProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeTCX:
		rec = &TcxProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	case bpfmaniov1alpha1.ProgTypeXDP:
		rec = &XdpProgramReconciler{
			ReconcilerCommon: r.ReconcilerCommon,
			ProgramReconcilerCommon: ProgramReconcilerCommon{
				appCommon:           r.currentApp.Spec.BpfAppCommon,
				currentProgram:      prog,
				currentProgramState: progState,
			},
		}

	default:
		return nil, fmt.Errorf("unsupported bpf program type")
	}

	return rec, nil
}

func (r *BpfApplicationReconciler) checkProgramStatus() bpfmaniov1alpha1.ProgramConditionType {
	for _, program := range r.currentAppState.Spec.Programs {
		if program.ProgramAttachStatus != bpfmaniov1alpha1.ProgAttachSuccess {
			return bpfmaniov1alpha1.ProgramReconcileError
		}
	}
	return bpfmaniov1alpha1.ProgramReconcileSuccess
}

// getProgState returns the BpfApplicationProgramState object for the current node.
func (r *BpfApplicationReconciler) getProgState(prog *bpfmaniov1alpha1.BpfApplicationProgram,
	programs []bpfmaniov1alpha1.BpfApplicationProgramState) (*bpfmaniov1alpha1.BpfApplicationProgramState, error) {
	for i := range programs {
		progState := &programs[i]
		if progState.Type == prog.Type && progState.BpfFunctionName == prog.BpfFunctionName {
			switch prog.Type {
			case bpfmaniov1alpha1.ProgTypeFentry:
				if progState.Fentry.FunctionName == prog.Fentry.FunctionName {
					return progState, nil
				}
			case bpfmaniov1alpha1.ProgTypeFexit:
				if progState.Fexit.FunctionName == prog.Fexit.FunctionName {
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
func (r *BpfApplicationReconciler) updateBpfAppStateSpec(ctx context.Context, originalAppState *bpfmaniov1alpha1.BpfApplicationState,
	bpfAppStateNew bool) (bool, error) {

	// We've completed reconciling this program and something has
	// changed.  We need to create or update the BpfApplicationState.
	if bpfAppStateNew {
		// ANF-TODO: With the change to create new BpfApplicationState objects
		// before completing the reconcile, we should never get here.

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
func (r *BpfApplicationReconciler) waitForBpfAppStateUpdate(ctx context.Context) (bool, error) {
	const maxRetries = 100
	const retryInterval = 100 * time.Millisecond

	var bpfAppState *bpfmaniov1alpha1.BpfApplicationState
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
func (r *BpfApplicationReconciler) waitForBpfAppStateStatusUpdate(ctx context.Context) error {
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
		} else if bpfAppState != nil &&
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
func (r *BpfApplicationReconciler) getBpfAppState(ctx context.Context, createIfNotFound bool) (*bpfmaniov1alpha1.BpfApplicationState, bool, error) {

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

func (r *BpfApplicationReconciler) createBpfAppState() (*bpfmaniov1alpha1.BpfApplicationState, bool, error) {
	bpfAppState := &bpfmaniov1alpha1.BpfApplicationState{
		ObjectMeta: metav1.ObjectMeta{
			Name:       generateUniqueName(r.currentApp.Name),
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
		return nil, false, fmt.Errorf("failed to initialize BpfApplicationState program list: %v", err)
	}

	// Make the corresponding BpfProgramConfig the owner
	if err := ctrl.SetControllerReference(r.currentApp, bpfAppState, r.Scheme); err != nil {
		return nil, false, fmt.Errorf("failed to set bpfAppState object owner reference: %v", err)
	}

	return bpfAppState, true, nil
}

func (r *BpfApplicationReconciler) initializeNodeProgramList(bpfAppState *bpfmaniov1alpha1.BpfApplicationState) error {
	// The list should only be initialized once when the BpfApplication is first
	// created.  After that, the user can't add or remove programs.
	if len(bpfAppState.Spec.Programs) != 0 {
		return fmt.Errorf("BpfApplicationState programs list has already been initialized")
	}

	for _, prog := range r.currentApp.Spec.Programs {
		// ANF-TODO: Create issue to investigate doing this with CRD validation.
		// Check if it's already on the list.  If it is, this is an error
		// because a given bpf function can only be loaded once per
		// BpfApplication.
		_, err := r.getProgState(&prog, bpfAppState.Spec.Programs)
		if err == nil {
			return fmt.Errorf("duplicate bpf function detected. bpfFunctionName: %s", prog.BpfFunctionName)
		}
		progState := bpfmaniov1alpha1.BpfApplicationProgramState{
			BpfProgramStateCommon: bpfmaniov1alpha1.BpfProgramStateCommon{
				BpfFunctionName:     prog.BpfFunctionName,
				ProgramAttachStatus: bpfmaniov1alpha1.ProgAttachInit,
			},
			Type: prog.Type,
		}
		switch prog.Type {
		case bpfmaniov1alpha1.ProgTypeFentry:
			progState.Fentry = &bpfmaniov1alpha1.FentryProgramInfoState{
				FentryLoadInfo: prog.Fentry.FentryLoadInfo,
				FentryAttachInfoState: bpfmaniov1alpha1.FentryAttachInfoState{
					AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
						AttachPointStatus: bpfmaniov1alpha1.ApAttachNotAttached,
						UUID:              uuid.New().String(),
					},
					Attach: prog.Fentry.Attach,
				},
			}

		case bpfmaniov1alpha1.ProgTypeFexit:
			progState.Fexit = &bpfmaniov1alpha1.FexitProgramInfoState{
				FexitLoadInfo: prog.Fexit.FexitLoadInfo,
				FexitAttachInfoState: bpfmaniov1alpha1.FexitAttachInfoState{
					AttachInfoStateCommon: bpfmaniov1alpha1.AttachInfoStateCommon{
						AttachPointStatus: bpfmaniov1alpha1.ApAttachNotAttached,
						UUID:              uuid.New().String(),
					},
					Attach: prog.Fexit.Attach,
				},
			}

		case bpfmaniov1alpha1.ProgTypeKprobe:
			progState.Kprobe = &bpfmaniov1alpha1.KprobeProgramInfoState{
				AttachPoints: []bpfmaniov1alpha1.KprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeTC:
			progState.TC = &bpfmaniov1alpha1.TcProgramInfoState{
				AttachPoints: []bpfmaniov1alpha1.TcAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeTCX:
			progState.TCX = &bpfmaniov1alpha1.TcxProgramInfoState{
				AttachPoints: []bpfmaniov1alpha1.TcxAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeTracepoint:
			progState.Tracepoint = &bpfmaniov1alpha1.TracepointProgramInfoState{
				AttachPoints: []bpfmaniov1alpha1.TracepointAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeUprobe:
			progState.Uprobe = &bpfmaniov1alpha1.UprobeProgramInfoState{
				AttachPoints: []bpfmaniov1alpha1.UprobeAttachInfoState{},
			}

		case bpfmaniov1alpha1.ProgTypeXDP:
			progState.XDP = &bpfmaniov1alpha1.XdpProgramInfoState{
				AttachPoints: []bpfmaniov1alpha1.XdpAttachInfoState{},
			}

		default:
			panic(fmt.Sprintf("unexpected EBPFProgType: %#v", prog.Type))
		}

		bpfAppState.Spec.Programs = append(bpfAppState.Spec.Programs, progState)
	}

	return nil
}

func (r *BpfApplicationReconciler) isLoaded(ctx context.Context) bool {
	allProgramsLoaded := true
	someProgramsLoaded := false
	for _, program := range r.currentAppState.Spec.Programs {
		if program.ProgramId == nil {
			allProgramsLoaded = false
		} else if _, err := bpfmanagentinternal.GetBpfmanProgramById(ctx, r.BpfmanClient, *program.ProgramId); err != nil {
			allProgramsLoaded = false
			// ANF-TODO: Should we check program info here to make sure it's the program we expect?
		} else {
			someProgramsLoaded = true
		}
	}

	if allProgramsLoaded != someProgramsLoaded {
		// ANF-TODO: This should never happen because the bpfman load is all or
		// nothing, and we aren't allowing users to add or remove programs from
		// an existing BpfApplication.  However, we should think about how to
		// handle it if it does happen. We could just unload everything and
		// reload it, or we could try to load the missing programs.  We could
		// also just log an error.  For now, we're just going to log an error
		// and continue.
		r.Logger.Error(fmt.Errorf("inconsistent program load state"),
			"allProgramsLoaded", allProgramsLoaded, "someProgramsLoaded", someProgramsLoaded)
	}

	return allProgramsLoaded
}

func (r *BpfApplicationReconciler) getLoadRequest() (*gobpfman.LoadRequest, error) {

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
		// ANF-TODO: Get map owner ID working.  For now, just pass nil.
		MapOwnerId: nil,
		Info:       loadInfo,
	}

	return &loadRequest, nil
}

func (r *BpfApplicationReconciler) load(ctx context.Context) error {
	loadRequest, err := r.getLoadRequest()
	if err != nil {
		return fmt.Errorf("failed to get LoadRequest")
	}

	loadResponse, err := r.BpfmanClient.Load(ctx, loadRequest)
	if err != nil {
		return fmt.Errorf("failed to load eBPF Program: %v", err)
	} else {
		for p, program := range r.currentAppState.Spec.Programs {
			id, err := bpfmanagentinternal.GetBpfProgramId(program.BpfFunctionName, loadResponse.Programs)
			// ANF-TODO: This should never happen because the bpfman load is
			// all or nothing, and if a success was returned, all of the
			// programs should have IDs.
			r.Logger.Info("Programs", "Program", program.BpfFunctionName, "ProgramId", id)
			if err != nil {
				return fmt.Errorf("failed to get program id: %v", err)
			}
			r.currentAppState.Spec.Programs[p].ProgramId = id
		}
	}
	return nil
}

func (r *BpfApplicationReconciler) unload(ctx context.Context) {
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
		// we can just delete the attach points from the state.
		r.deleteAttachPoints(&r.currentAppState.Spec.Programs[i])
		r.currentAppState.Spec.Programs[i].ProgramAttachStatus = bpfmaniov1alpha1.ProgAttachSuccess
	}
}

func (r *BpfApplicationReconciler) deleteAttachPoints(program *bpfmaniov1alpha1.BpfApplicationProgramState) {
	switch program.Type {
	case bpfmaniov1alpha1.ProgTypeFentry:
		program.Fentry.Attach = false
	case bpfmaniov1alpha1.ProgTypeFexit:
		program.Fexit.Attach = false
	case bpfmaniov1alpha1.ProgTypeKprobe:
		program.Kprobe.AttachPoints = []bpfmaniov1alpha1.KprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeTC:
		program.TC.AttachPoints = []bpfmaniov1alpha1.TcAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeTCX:
		program.TCX.AttachPoints = []bpfmaniov1alpha1.TcxAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeTracepoint:
		program.Tracepoint.AttachPoints = []bpfmaniov1alpha1.TracepointAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeUprobe:
		program.Uprobe.AttachPoints = []bpfmaniov1alpha1.UprobeAttachInfoState{}
	case bpfmaniov1alpha1.ProgTypeXDP:
		program.XDP.AttachPoints = []bpfmaniov1alpha1.XdpAttachInfoState{}
	default:
		r.Logger.Error(fmt.Errorf("unexpected EBPFProgType"), "unexpected EBPFProgType", "Type", program.Type)
	}
}
