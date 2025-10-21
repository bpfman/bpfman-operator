/*
Copyright 2024 The bpfman Authors.

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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=clusterbpfapplications/finalizers,verbs=update

// BpfApplicationReconciler reconciles a BpfApplication object
type BpfApplicationReconciler struct {
	ClusterApplicationReconciler
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *BpfApplicationReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ClusterApplicationReconciler.ReconcilerCommon
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *BpfApplicationReconciler) getFinalizer() string {
	return internal.ClBpfApplicationControllerFinalizer
}

// SetupWithManager sets up the controller with the Manager.

func (r *BpfApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.ClusterBpfApplication{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Watches(
			&bpfmaniov1alpha1.ClusterBpfApplicationState{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(statusChangedPredicateCluster()),
		).
		Complete(r)
}

func (r *BpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("application")
	r.Logger.Info("BpfApplication Reconcile enter", "Name", req.NamespacedName.Name)

	bpfApp := &bpfmaniov1alpha1.ClusterBpfApplication{}
	if err := r.Get(ctx, req.NamespacedName, bpfApp); err != nil {
		// Reconcile was triggered by BpfApplicationState event, get parent bpfApp Object.
		if errors.IsNotFound(err) {
			bpfAppState := &bpfmaniov1alpha1.ClusterBpfApplicationState{}
			if err := r.Get(ctx, req.NamespacedName, bpfAppState); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.V(1).Info("BpfApplication not found. Stale reconcile. Exiting", "Name", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting BpfApplication Object", "Name", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

			// Get owning bpfApp object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfAppState)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfApplicationState object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, bpfApp); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("Application Programs from ownerRef not found stale reconcile exiting", "Name", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting Application Programs Object from ownerRef", "Name", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

		} else {
			r.Logger.Error(err, "failed getting Application Programs Object", "Name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	}

	return reconcileBpfApplication(ctx, r, bpfApp)
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *BpfApplicationReconciler) updateStatus(ctx context.Context, _namespace string, name string, cond bpfmaniov1alpha1.BpfApplicationConditionType, message string) (ctrl.Result, error) {
	// TODO: Does this still happen?
	// Sometimes we end up with a stale FentryProgram due to races, do this
	// get to ensure we're up to date before attempting a status update.
	app := &bpfmaniov1alpha1.ClusterBpfApplication{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: name}, app); err != nil {
		r.Logger.V(1).Info("failed to get fresh Application Programs object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return r.updateCondition(ctx, app, &app.Status.Conditions, cond, message)
}
