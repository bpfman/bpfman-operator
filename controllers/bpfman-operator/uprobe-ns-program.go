/*
Copyright 2024.
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

//lint:file-ignore U1000 Linter claims functions unused, but are required for generic

package bpfmanoperator

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=uprobensprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=uprobensprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=uprobensprograms/finalizers,verbs=update
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=uprobensprograms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=uprobensprograms/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=uprobensprograms/finalizers,verbs=update

type UprobeNsProgramReconciler struct {
	NamespaceProgramReconciler
}

func (r *UprobeNsProgramReconciler) getRecCommon() *ReconcilerCommon[bpfmaniov1alpha1.BpfNsProgram, bpfmaniov1alpha1.BpfNsProgramList] {
	return &r.NamespaceProgramReconciler.ReconcilerCommon
}

func (r *UprobeNsProgramReconciler) getFinalizer() string {
	return internal.UprobeNsProgramControllerFinalizer
}

// SetupWithManager sets up the controller with the Manager.
func (r *UprobeNsProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.UprobeNsProgram{}).
		// Watch BpfNsPrograms which are owned by UprobeNsPrograms
		Watches(
			&bpfmaniov1alpha1.BpfNsProgram{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(
				predicate.And(statusChangedPredicateNamespace(),
					internal.BpfNsProgramTypePredicate(internal.UprobeString)),
			),
		).
		Complete(r)
}

func (r *UprobeNsProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("uprobe-ns")
	r.Logger.Info("bpfman-operator enter: uprobe-ns",
		"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)

	uprobeProgram := &bpfmaniov1alpha1.UprobeNsProgram{}
	if err := r.Get(ctx, req.NamespacedName, uprobeProgram); err != nil {
		// Reconcile was triggered by BpfNsProgram event, get parent UprobeNsProgram Object.
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

			// Get owning UprobeNsProgram object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfNsProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: req.NamespacedName.Namespace, Name: ownerRef.Name}, uprobeProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("Uprobe Program from ownerRef not found stale reconcile exiting",
						"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
				} else {
					r.Logger.Error(err, "failed getting UprobeNsProgram Object from ownerRef",
						"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
				}
				return ctrl.Result{}, nil
			}

		} else {
			r.Logger.Error(err, "failed getting UprobeNsProgram Object",
				"Namespace", req.NamespacedName.Namespace, "Name", req.NamespacedName.Name)
			return ctrl.Result{}, nil
		}
	}

	return reconcileBpfProgram(ctx, r, uprobeProgram)
}

func (r *UprobeNsProgramReconciler) updateStatus(
	ctx context.Context,
	namespace string,
	name string,
	cond bpfmaniov1alpha1.ProgramConditionType,
	message string,
) (ctrl.Result, error) {
	// Sometimes we end up with a stale UprobeNsProgram due to races, do this
	// get to ensure we're up to date before attempting a status update.
	prog := &bpfmaniov1alpha1.UprobeNsProgram{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, prog); err != nil {
		r.Logger.V(1).Info("failed to get fresh UprobeNsProgram object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return r.updateCondition(ctx, prog, &prog.Status.Conditions, cond, message)
}
