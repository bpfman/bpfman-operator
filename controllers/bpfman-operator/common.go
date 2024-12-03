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

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfprograms,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsprograms,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=bpfnsprograms,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

const (
	retryDurationOperator = 5 * time.Second
)

type BpfProgOper interface {
	GetName() string

	GetLabels() map[string]string
	GetStatus() *bpfmaniov1alpha1.BpfProgramStatus
}

type BpfProgListOper[T any] interface {
	// bpfmniov1alpha1.BpfProgramList | bpfmaniov1alpha1.BpfNsProgramList

	GetItems() []T
}

// ReconcilerCommon reconciles a BpfProgram object
type ReconcilerCommon[T BpfProgOper, TL BpfProgListOper[T]] struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// bpfmanReconciler defines a k8s reconciler which can program bpfman.
type ProgramReconciler[T BpfProgOper, TL BpfProgListOper[T]] interface {
	// BPF Cluster of Namespaced Reconciler
	getBpfList(ctx context.Context,
		progName string,
		progNamespace string,
	) (*TL, error)
	containsFinalizer(bpfProgram *T, finalizer string) bool

	// *Program Reconciler
	getRecCommon() *ReconcilerCommon[T, TL]
	updateStatus(ctx context.Context,
		namespace string,
		name string,
		cond bpfmaniov1alpha1.ProgramConditionType,
		message string) (ctrl.Result, error)
	getFinalizer() string
}

func reconcileBpfProgram[T BpfProgOper, TL BpfProgListOper[T]](
	ctx context.Context,
	rec ProgramReconciler[T, TL],
	prog client.Object,
) (ctrl.Result, error) {
	r := rec.getRecCommon()
	progName := prog.GetName()
	progNamespace := prog.GetNamespace()

	r.Logger.V(1).Info("Reconciling Program", "Namespace", progNamespace, "Name", progName)

	if !controllerutil.ContainsFinalizer(prog, internal.BpfmanOperatorFinalizer) {
		r.Logger.V(1).Info("Add Finalizer", "Namespace", progNamespace, "ProgramName", progName)
		return r.addFinalizer(ctx, prog, internal.BpfmanOperatorFinalizer)
	}

	// reconcile Program Object on all other events
	// list all existing bpfProgram state for the given Program
	bpfPrograms, err := rec.getBpfList(ctx, progName, progNamespace)
	if err != nil {
		r.Logger.Error(err, "failed to get freshPrograms for full reconcile")
		return ctrl.Result{}, nil
	}

	// List all nodes since a bpfprogram object will always be created for each
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, &client.ListOptions{}); err != nil {
		r.Logger.Error(err, "failed getting nodes for full reconcile")
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	// If the program isn't being deleted, make sure that each node has at
	// least one bpfprogram object.  If not, Return NotYetLoaded Status.
	if prog.GetDeletionTimestamp().IsZero() {
		for _, node := range nodes.Items {
			nodeFound := false
			for _, program := range (*bpfPrograms).GetItems() {
				bpfProgramNode := program.GetLabels()[internal.K8sHostLabel]
				if node.Name == bpfProgramNode {
					nodeFound = true
					break
				}
			}
			if !nodeFound {
				return rec.updateStatus(ctx, progNamespace, progName, bpfmaniov1alpha1.ProgramNotYetLoaded, "")
			}
		}
	}

	failedBpfPrograms := []string{}
	finalApplied := []string{}
	// Make sure no bpfPrograms had any issues in the loading or unloading process
	for _, bpfProgram := range (*bpfPrograms).GetItems() {

		if rec.containsFinalizer(&bpfProgram, rec.getFinalizer()) {
			finalApplied = append(finalApplied, bpfProgram.GetName())
		}

		status := bpfProgram.GetStatus()
		if bpfmanHelpers.IsBpfProgramConditionFailure(&status.Conditions) {
			failedBpfPrograms = append(failedBpfPrograms, bpfProgram.GetName())
		}
	}

	if !prog.GetDeletionTimestamp().IsZero() {
		// Only remove bpfman-operator finalizer if all bpfProgram Objects are ready to be pruned  (i.e there are no
		// bpfPrograms with a finalizer)
		if len(finalApplied) == 0 {
			// Causes Requeue
			return r.removeFinalizer(ctx, prog, internal.BpfmanOperatorFinalizer)
		}

		// Causes Requeue
		return rec.updateStatus(ctx, progNamespace, progName, bpfmaniov1alpha1.ProgramDeleteError,
			fmt.Sprintf("Program Deletion failed on the following bpfProgram Objects: %v", finalApplied))
	}

	if len(failedBpfPrograms) != 0 {
		// Causes Requeue
		return rec.updateStatus(ctx, progNamespace, progName, bpfmaniov1alpha1.ProgramReconcileError,
			fmt.Sprintf("bpfProgramReconciliation failed on the following bpfProgram Objects: %v", failedBpfPrograms))
	}

	// Causes Requeue
	return rec.updateStatus(ctx, progNamespace, progName, bpfmaniov1alpha1.ProgramReconcileSuccess, "")
}

func (r *ReconcilerCommon[T, TL]) removeFinalizer(ctx context.Context, prog client.Object, finalizer string) (ctrl.Result, error) {
	r.Logger.Info("Calling KubeAPI to delete Program Finalizer", "Type", prog.GetObjectKind().GroupVersionKind().Kind, "Name", prog.GetName())

	if changed := controllerutil.RemoveFinalizer(prog, finalizer); changed {
		err := r.Update(ctx, prog)
		if err != nil {
			r.Logger.Error(err, "failed to remove bpfProgram Finalizer")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *ReconcilerCommon[T, TL]) addFinalizer(ctx context.Context, prog client.Object, finalizer string) (ctrl.Result, error) {
	controllerutil.AddFinalizer(prog, finalizer)

	r.Logger.Info("Calling KubeAPI to add Program Finalizer", "Type", prog.GetObjectKind().GroupVersionKind().Kind, "Name", prog.GetName())
	err := r.Update(ctx, prog)
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
	cond bpfmaniov1alpha1.ProgramConditionType,
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
			r.Logger.Info("more than one BpfProgramCondition", "numConditions", numConditions)
			*conditions = nil
		}
		// if numConditions == 0, just add the new condition below.
	}

	meta.SetStatusCondition(conditions, cond.Condition(message))

	r.Logger.Info("Calling KubeAPI to update Program condition", "Type", obj.GetObjectKind().GroupVersionKind().Kind,
		"Name", obj.GetName(), "condition", cond.Condition(message).Type)
	if err := r.Status().Update(ctx, obj); err != nil {
		r.Logger.V(1).Info("failed to set *Program object status...requeuing", "error", err)
		return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
	}

	r.Logger.V(1).Info("condition updated", "new condition", cond)
	return ctrl.Result{}, nil
}
