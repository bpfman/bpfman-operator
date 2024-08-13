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

package bpfmanagent

import (
	"context"
	"fmt"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
	"github.com/bpfman/bpfman-operator/internal"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=tcxprograms,verbs=get;list;watch

// TcxProgramReconciler reconciles a tcxProgram object by creating multiple
// bpfProgram objects and managing bpfman for each one.
type TcxProgramReconciler struct {
	ReconcilerCommon
	currentTcxProgram *bpfmaniov1alpha1.TcxProgram
	interfaces        []string
	ourNode           *v1.Node
}

func (r *TcxProgramReconciler) getFinalizer() string {
	return r.finalizer
}

func (r *TcxProgramReconciler) getOwner() metav1.Object {
	if r.appOwner == nil {
		return r.currentTcxProgram
	} else {
		return r.appOwner
	}
}

func (r *TcxProgramReconciler) getRecType() string {
	return r.recType
}

func (r *TcxProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tc
}

func (r *TcxProgramReconciler) getName() string {
	return r.currentTcxProgram.Name
}

func (r *TcxProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *TcxProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentTcxProgram.Spec.BpfProgramCommon
}

func (r *TcxProgramReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentTcxProgram.Spec.NodeSelector
}

func (r *TcxProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.currentTcxProgram.Spec.GlobalData
}

func (r *TcxProgramReconciler) getAppProgramId() string {
	return appProgramId(r.currentTcxProgram.GetLabels())
}

func (r *TcxProgramReconciler) setCurrentProgram(program client.Object) error {
	var err error
	var ok bool

	r.currentTcxProgram, ok = program.(*bpfmaniov1alpha1.TcxProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to TcxProgram")
	}

	r.interfaces, err = getInterfaces(&r.currentTcxProgram.Spec.InterfaceSelector, r.ourNode)
	if err != nil {
		return fmt.Errorf("failed to get interfaces for TcxProgram: %v", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a TcxProgram is updated,
// load the program to the node via bpfman, and then create bpfProgram object(s)
// to reflect per node state information.
func (r *TcxProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.TcxProgram{}, builder.WithPredicates(predicate.And(
			predicate.GenerationChangedPredicate{},
			predicate.ResourceVersionChangedPredicate{}),
		),
		).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.TcxString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the TcxProgram no longer select the Node. Additionally only
		// care about events specific to our node
		Watches(
			&v1.Node{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *TcxProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}
	for _, iface := range r.interfaces {
		attachPoint := iface + "-" + r.currentTcxProgram.Spec.Direction
		annotations := map[string]string{internal.TcxProgramInterface: iface}

		prog, err := r.createBpfProgram(attachPoint, r, annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to create BpfProgram %s: %v", attachPoint, err)
		}

		progs.Items = append(progs.Items, *prog)
	}

	return progs, nil
}

func (r *TcxProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentTcxProgram = &bpfmaniov1alpha1.TcxProgram{}
	r.finalizer = internal.TcxProgramControllerFinalizer
	r.recType = internal.TcxString
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("tcx")

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile TCX: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	tcxPrograms := &bpfmaniov1alpha1.TcxProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, tcxPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting TcxPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(tcxPrograms.Items) == 0 {
		r.Logger.Info("TcxProgramController found no TCX Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of tcx programs to pass into reconcileCommon()
	var tcxObjects []client.Object = make([]client.Object, len(tcxPrograms.Items))
	for i := range tcxPrograms.Items {
		tcxObjects[i] = &tcxPrograms.Items[i]
	}

	// Reconcile each TcxProgram.
	_, result, err := r.reconcileCommon(ctx, r, tcxObjects)
	return result, err
}

func (r *TcxProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentTcxProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentTcxProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Tc),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcAttachInfo{
				TcAttachInfo: &gobpfman.TCAttachInfo{
					Priority:  r.currentTcxProgram.Spec.Priority,
					Iface:     bpfProgram.Annotations[internal.TcxProgramInterface],
					Direction: r.currentTcxProgram.Spec.Direction,
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.getOwner().GetName()},
		GlobalData: r.currentTcxProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}
