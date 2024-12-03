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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsapplications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsapplications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsapplications/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=bpfnsapplications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=bpfnsapplications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=bpfnsapplications/finalizers,verbs=update

// BpfNsApplicationReconciler reconciles a BpfNsApplication object
type BpfNsApplicationReconciler struct {
	NamespaceProgramReconciler
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *BpfNsApplicationReconciler) getRecCommon() *ReconcilerCommon[bpfmaniov1alpha1.BpfNsProgram, bpfmaniov1alpha1.BpfNsProgramList] {
	return &r.NamespaceProgramReconciler.ReconcilerCommon
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *BpfNsApplicationReconciler) getFinalizer() string {
	return internal.BpfNsApplicationControllerFinalizer
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfNsApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.BpfNsApplication{}).
		// Watch BpfNsPrograms which are owned by BpfNsApplications
		Watches(
			&bpfmaniov1alpha1.BpfNsProgram{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(
				predicate.And(
					statusChangedPredicateNamespace(),
					internal.BpfNsProgramTypePredicate(internal.ApplicationString),
				),
			),
		).
		Complete(r)
}

func (r *BpfNsApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("application")
	r.Logger.Info("bpfman-operator enter: application-ns",
		"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)

	appProgram := &bpfmaniov1alpha1.BpfNsApplication{}
	if err := r.Get(ctx, req.NamespacedName, appProgram); err != nil {
		// Reconcile was triggered by BpfNsProgram event, get parent appProgram Object.
		if errors.IsNotFound(err) {
			bpfProgram := &bpfmaniov1alpha1.BpfNsProgram{}
			if err := r.Get(ctx, req.NamespacedName, bpfProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.V(1).Info("BpfNsProgram not found stale reconcile, exiting",
						"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
				} else {
					r.Logger.Error(err, "failed getting BpfNsProgram Object",
						"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
				}
				return ctrl.Result{}, nil
			}

			// Get owning appProgram object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfNsProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: req.NamespacedName.Namespace, Name: ownerRef.Name}, appProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("Application Programs from ownerRef not found stale reconcile exiting",
						"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
				} else {
					r.Logger.Error(err, "failed getting Application Programs Object from ownerRef",
						"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
				}
				return ctrl.Result{}, nil
			}

		} else {
			r.Logger.Error(err, "failed getting Application Programs Object",
				"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
			return ctrl.Result{}, nil
		}
	}

	return reconcileBpfProgram(ctx, r, appProgram)
}

//lint:ignore U1000 Linter claims function unused, but generics confusing linter
func (r *BpfNsApplicationReconciler) updateStatus(
	ctx context.Context,
	namespace string,
	name string,
	cond bpfmaniov1alpha1.ProgramConditionType,
	message string,
) (ctrl.Result, error) {
	// Sometimes we end up with a stale FentryProgram due to races, do this
	// get to ensure we're up to date before attempting a status update.
	app := &bpfmaniov1alpha1.BpfNsApplication{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, app); err != nil {
		r.Logger.V(1).Info("failed to get fresh Application Programs object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return r.updateCondition(ctx, app, &app.Status.Conditions, cond, message)
}
