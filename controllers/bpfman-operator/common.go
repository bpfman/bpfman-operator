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

package bpfmanoperator

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
	bpfmanHelpers "github.com/bpfman/bpfman-operator/pkg/helpers"
	"github.com/go-logr/logr"
)

// +kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplicationstates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bpfman.io,resources=bpfapplicationstates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=bpfapplicationstates,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

const (
	retryDurationOperator = 5 * time.Second
)

type BpfProgOper interface {
	GetName() string

	GetLabels() map[string]string
	GetStatus() *bpfmaniov1alpha1.BpfAppStatus
}

type BpfProgListOper[T any] interface {
	// bpfmniov1alpha1.BpfApplicationStateList | bpfmaniov1alpha1.ClusterBpfApplicationStateList
	GetItems() []T
}

type ReconcilerCommon[T BpfProgOper, TL BpfProgListOper[T]] struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// ApplicationReconciler defines a k8s reconciler which can program bpfman.
type ApplicationReconciler[T BpfProgOper, TL BpfProgListOper[T]] interface {
	// BPF Cluster of Namespaced Reconciler
	getAppStateList(ctx context.Context,
		appName string,
		appNamespace string,
	) (*TL, error)
	containsFinalizer(bpfApplication *T, finalizer string) bool

	// *Program Reconciler
	getRecCommon() *ReconcilerCommon[T, TL]
	updateStatus(ctx context.Context,
		namespace string,
		name string,
		cond bpfmaniov1alpha1.BpfApplicationConditionType,
		message string) (ctrl.Result, error)
	getFinalizer() string
}

func reconcileBpfApplication[T BpfProgOper, TL BpfProgListOper[T]](
	ctx context.Context,
	rec ApplicationReconciler[T, TL],
	app client.Object,
) (ctrl.Result, error) {
	r := rec.getRecCommon()
	appName := app.GetName()
	appNamespace := app.GetNamespace()

	r.Logger.V(1).Info("Reconciling Bpf Application", "Namespace", appNamespace, "Name", appName)

	if !controllerutil.ContainsFinalizer(app, internal.BpfmanOperatorFinalizer) {
		r.Logger.V(1).Info("Add Finalizer", "Namespace", appNamespace, "ProgramName", appName)
		return r.addFinalizer(ctx, app, internal.BpfmanOperatorFinalizer)
	}

	// reconcile BpfApplication Objects on all other events
	// list all existing BpfApplication state for the given Program
	bpfAppStateObjs, err := rec.getAppStateList(ctx, appName, appNamespace)
	if err != nil {
		r.Logger.Error(err, "failed to get freshPrograms for full reconcile")
		return ctrl.Result{}, nil
	}

	// List all nodes since a BpfApplicationState object will always be created for each
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, &client.ListOptions{}); err != nil {
		r.Logger.Error(err, "failed getting nodes for full reconcile")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	// If the application isn't being deleted, make sure that each node has at
	// least one BpfApplicationState object.  If not, Return Pending Status.
	if app.GetDeletionTimestamp().IsZero() {
		for _, node := range nodes.Items {
			nodeFound := false
			for _, appState := range (*bpfAppStateObjs).GetItems() {
				bpfProgramState := appState.GetLabels()[internal.K8sHostLabel]
				if node.Name == bpfProgramState {
					nodeFound = true
					break
				}
			}
			if !nodeFound {
				return rec.updateStatus(ctx, appNamespace, appName, bpfmaniov1alpha1.BpfAppCondPending, "")
			}
		}
	}

	failedBpfApplications := []string{}
	finalApplied := []string{}
	// Make sure no BpfApplications had any issues in the loading or unloading process
	for _, bpfAppState := range (*bpfAppStateObjs).GetItems() {

		if rec.containsFinalizer(&bpfAppState, rec.getFinalizer()) {
			finalApplied = append(finalApplied, bpfAppState.GetName())
		}

		status := bpfAppState.GetStatus()
		if bpfmanHelpers.IsBpfAppStateConditionFailure(&status.Conditions) {
			failedBpfApplications = append(failedBpfApplications, bpfAppState.GetName())
		}
	}

	if !app.GetDeletionTimestamp().IsZero() {
		// Only remove bpfman-operator finalizer if all BpfApplicationState objects are ready to be pruned  (i.e there are no
		// bpfPrograms with a finalizer)
		if len(finalApplied) == 0 {
			// Causes Requeue
			return r.removeFinalizer(ctx, app, internal.BpfmanOperatorFinalizer)
		}

		// Causes Requeue
		return rec.updateStatus(ctx, appNamespace, appName, bpfmaniov1alpha1.BpfAppCondDeleteError,
			fmt.Sprintf("Program Deletion failed on the following BpfApplicationState objects: %v", finalApplied))
	}

	if len(failedBpfApplications) != 0 {
		// Causes Requeue
		return rec.updateStatus(ctx, appNamespace, appName, bpfmaniov1alpha1.BpfAppCondError,
			fmt.Sprintf("bpfProgramReconciliation failed on the following BpfApplicationState objects: %v", failedBpfApplications))
	}

	// Causes Requeue
	return rec.updateStatus(ctx, appNamespace, appName, bpfmaniov1alpha1.BpfAppCondSuccess, "")
}

func (r *ReconcilerCommon[T, TL]) removeFinalizer(ctx context.Context, bpfApp client.Object, finalizer string) (ctrl.Result, error) {
	r.Logger.Info("Calling KubeAPI to delete Program Finalizer", "Type", bpfApp.GetObjectKind().GroupVersionKind().Kind, "Name", bpfApp.GetName())

	if changed := controllerutil.RemoveFinalizer(bpfApp, finalizer); changed {
		err := r.Update(ctx, bpfApp)
		if err != nil {
			r.Logger.Error(err, "failed to remove bpfApp Finalizer")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *ReconcilerCommon[T, TL]) addFinalizer(ctx context.Context, app client.Object, finalizer string) (ctrl.Result, error) {
	controllerutil.AddFinalizer(app, finalizer)

	r.Logger.Info("Calling KubeAPI to add Program Finalizer", "Type", app.GetObjectKind().GroupVersionKind().Kind, "Name", app.GetName())
	err := r.Update(ctx, app)
	if err != nil {
		r.Logger.V(1).Info("failed adding bpfman-operator finalizer to Program...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ReconcilerCommon[T, TL]) updateCondition(
	ctx context.Context,
	obj client.Object,
	conditions *[]metav1.Condition,
	cond bpfmaniov1alpha1.BpfApplicationConditionType,
	message string,
) (ctrl.Result, error) {

	r.Logger.V(1).Info("updateCondition()", "existing conds", conditions, "new cond", cond)

	if conditions != nil {
		numConditions := len(*conditions)

		if numConditions == 1 {
			if (*conditions)[0].Type == string(cond) {
				r.Logger.Info("No change in status", "existing condition", (*conditions)[0].Type)
				// No change, so just return false -- not updated
				return ctrl.Result{}, nil
			} else {
				// We're changing the condition, so delete this one.  The
				// new condition will be added below.
				*conditions = nil
			}
		} else if numConditions > 1 {
			// We should only ever have one condition, so we shouldn't hit this
			// case.  However, if we do, log a message, delete the existing
			// conditions, and add the new one below.
			r.Logger.Info("more than one condition found", "numConditions", numConditions)
			*conditions = nil
		}
		// if numConditions == 0, just add the new condition below.
	}

	meta.SetStatusCondition(conditions, cond.Condition(message))

	r.Logger.Info("Calling KubeAPI to update Program condition", "Type", obj.GetObjectKind().GroupVersionKind().Kind,
		"Name", obj.GetName(), "condition", cond.Condition(message).Type)
	if err := r.Status().Update(ctx, obj); err != nil {
		r.Logger.V(1).Info("failed to set BpfApplication object status...requeuing", "error", err)
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	r.Logger.V(1).Info("condition updated", "new condition", cond)
	return ctrl.Result{}, nil
}
