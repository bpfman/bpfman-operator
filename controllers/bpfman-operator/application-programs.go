/*
Copyright 2023 The bpfman Authors.

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
	"sigs.k8s.io/controller-runtime/pkg/log"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
)

// BpfApplicationReconciler reconciles a BpfApplication object
type BpfApplicationReconciler struct {
	ReconcilerCommon
}

func (r *BpfApplicationReconciler) getRecCommon() *ReconcilerCommon {
	return &r.ReconcilerCommon
}

func (r *BpfApplicationReconciler) getFinalizer() string {
	return internal.BpfApplicationControllerFinalizer
}

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications/finalizers,verbs=update

func (r *BpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	appProgram := &bpfmaniov1alpha1.BpfApplication{}
	if err := r.Get(ctx, req.NamespacedName, appProgram); err != nil {
		// Reconcile was triggered by bpfProgram event, get parent appProgram Object.
		if errors.IsNotFound(err) {
			bpfProgram := &bpfmaniov1alpha1.BpfProgram{}
			if err := r.Get(ctx, req.NamespacedName, bpfProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.V(1).Info("bpfProgram not found stale reconcile, exiting", "Name", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting bpfProgram Object", "Name", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

			// Get owning appProgram object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, appProgram); err != nil {
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

	return r.reconcileAppPrograms(ctx, appProgram)
}

func (r *BpfApplicationReconciler) reconcileAppPrograms(ctx context.Context, application *bpfmaniov1alpha1.BpfApplication) (ctrl.Result, error) {
	var result ctrl.Result
	var err error

	for _, prog := range application.Spec.Programs {
		switch prog.Type {
		case bpfmaniov1alpha1.ProgTypeXDP:
			r.Logger.Info("Reconciling Application XDP Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.XDP)

		case bpfmaniov1alpha1.ProgTypeTC:
			r.Logger.Info("Reconciling Application TC Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.TC)

		case bpfmaniov1alpha1.ProgTypeFentry:
			r.Logger.Info("Reconciling Application Fentry/Fexit Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.Fentry)

		case bpfmaniov1alpha1.ProgTypeFexit:
			r.Logger.Info("Reconciling Application Fexit Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.Fexit)

		case bpfmaniov1alpha1.ProgTypeKprobe:
			r.Logger.Info("Reconciling Application Kprobe Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.Kprobe)

		case bpfmaniov1alpha1.ProgTypeKretprobe:
			r.Logger.Info("Reconciling Application Kretprobe Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.Kretprobe)

		case bpfmaniov1alpha1.ProgTypeUprobe:
			r.Logger.Info("Reconciling Application Uprobe Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.Uprobe)

		case bpfmaniov1alpha1.ProgTypeUretprobe:
			r.Logger.Info("Reconciling Application Uretprobe Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.Uretprobe)

		case bpfmaniov1alpha1.ProgTypeTracepoint:
			r.Logger.Info("Reconciling Application Tracepoint Programs")
			result, err = reconcileBpfProgram(ctx, r, prog.Tracepoint)

		default:
			err := fmt.Errorf("invalid program type: %s", prog.Type)
			r.Logger.Error(err, "invalid program type")
			return ctrl.Result{}, err
		}
		if err != nil {
			r.Logger.Error(err, "failed reconciling Application Programs")
			return ctrl.Result{}, err
		}
	}
	return result, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.BpfApplication{}).
		Complete(r)
}

func (r *BpfApplicationReconciler) updateStatus(ctx context.Context, name string, cond bpfmaniov1alpha1.ProgramConditionType, message string) (ctrl.Result, error) {
	app := &bpfmaniov1alpha1.BpfApplication{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: name}, app); err != nil {
		r.Logger.V(1).Info("failed to get fresh Application Programs object...requeuing")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	return r.updateCondition(ctx, app, &app.Status.Conditions, cond, message)
}
