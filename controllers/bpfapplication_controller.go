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

package controllers

import (
	"context"
	"fmt"
	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// BpfApplicationReconciler reconciles a BpfApplication object
type BpfApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the BpfApplication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *BpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx)

	application := &bpfmaniov1alpha1.BpfApplication{}
	if err := r.Get(ctx, req.NamespacedName, application); err != nil {
		if errors.IsNotFound(err) {
			bpfProgram := &bpfmaniov1alpha1.BpfApplication{}
			if err := r.Get(ctx, req.NamespacedName, bpfProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.V(1).Info("bpfProgram not found stale reconcile, exiting", "Name", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting bpfProgram Object", "Name", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

			// Get owning application Programs object from ownerRef
			ownerRef := metav1.GetControllerOf(bpfProgram)
			if ownerRef == nil {
				return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfProgram Object owner")
			}

			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: ownerRef.Name}, bpfProgram); err != nil {
				if errors.IsNotFound(err) {
					r.Logger.Info("Application Program from ownerRef not found stale reconcile exiting", "Name", req.NamespacedName)
				} else {
					r.Logger.Error(err, "failed getting Application Program Object from ownerRef", "Name", req.NamespacedName)
				}
				return ctrl.Result{}, nil
			}

		} else {
			r.Logger.Error(err, "failed getting Application Program Object", "Name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	}

	return r.reconcileApplication(ctx, r, application)
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.BpfApplication{}).
		Complete(r)
}

func (r *BpfApplicationReconciler) reconcileApplication(ctx context.Context, reconciler *BpfApplicationReconciler, application *bpfmaniov1alpha1.BpfApplication) (ctrl.Result, error) {
	r.Logger.Info("Reconciling Application", "Name", application.Name)
	// reconcile Program Object on all other events
	// list all existing bpfProgram state for the given Program
	apps := &bpfmaniov1alpha1.BpfApplicationList{}

	for _, app := range apps.Items {
		if _, err := r.reconcileAppPrograms(ctx, &app); err != nil {
			r.Logger.Error(err, "failed reconciling Application Programs", "Name", application.Name)
			return ctrl.Result{}, err
		}

	}
	return ctrl.Result{}, nil
}

func (r *BpfApplicationReconciler) reconcileAppPrograms(ctx context.Context, application *bpfmaniov1alpha1.BpfApplication) (ctrl.Result, error) {
	for _, prog := range application.Spec.Programs {
		switch prog.Type {
		case bpfmaniov1alpha1.ProgTypeXDP:
			r.Logger.Info("Reconciling Application XDP Programs")
		case bpfmaniov1alpha1.ProgTypeTC:
			r.Logger.Info("Reconciling Application TC Programs")
		case bpfmaniov1alpha1.ProgTypeTCX:
			r.Logger.Info("Reconciling Application TCX Programs")
		case bpfmaniov1alpha1.ProgTypeFentry, bpfmaniov1alpha1.ProgTypeFexit:
			r.Logger.Info("Reconciling Application Fentry/Fexit Programs")
		case bpfmaniov1alpha1.ProgTypeKprobe, bpfmaniov1alpha1.ProgTypeKretprobe:
			r.Logger.Info("Reconciling Application Kprobe/Kretprobe Programs")
		case bpfmaniov1alpha1.ProgTypeUprobe, bpfmaniov1alpha1.ProgTypeUretprobe:
			r.Logger.Info("Reconciling Application Uprobe/Uretprobe Programs")
		case bpfmaniov1alpha1.ProgTypeTracepoint:
			r.Logger.Info("Reconciling Application Tracepoint Programs")
		default:
			err := fmt.Errorf("invalid program type: %s", prog.Type)
			r.Logger.Error(err, "invalid program type")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}
