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
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=fexitprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfProgram object
type FexitProgramReconciler struct {
	ReconcilerCommon
	currentFexitProgram *bpfmaniov1alpha1.FexitProgram
	ourNode             *v1.Node
}

func (r *FexitProgramReconciler) getFinalizer() string {
	return r.finalizer
}

func (r *FexitProgramReconciler) getOwner() metav1.Object {
	if r.appOwner == nil {
		return r.currentFexitProgram
	} else {
		return r.appOwner
	}
}

func (r *FexitProgramReconciler) getRecType() string {
	return r.recType
}

func (r *FexitProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tracing
}

func (r *FexitProgramReconciler) getName() string {
	return r.currentFexitProgram.Name
}

func (r *FexitProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *FexitProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentFexitProgram.Spec.BpfProgramCommon
}

func (r *FexitProgramReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentFexitProgram.Spec.NodeSelector
}

func (r *FexitProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.currentFexitProgram.Spec.GlobalData
}

func (r *FexitProgramReconciler) getAppProgramId() string {
	return appProgramId(r.currentFexitProgram.GetLabels())
}

func (r *FexitProgramReconciler) setCurrentProgram(program client.Object) error {
	var ok bool

	r.currentFexitProgram, ok = program.(*bpfmaniov1alpha1.FexitProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to FexitProgram")
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a FexitProgram is updated,
// load the program to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *FexitProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.FexitProgram{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.FexitString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the FexitProgram no longer select the Node. Additionally only
		// care about node events specific to our node
		Watches(
			&v1.Node{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.And(predicate.LabelChangedPredicate{}, nodePredicate(r.NodeName))),
		).
		Complete(r)
}

func (r *FexitProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfProgramList{}

	attachPoint := sanitize(r.currentFexitProgram.Spec.FunctionName)

	annotations := map[string]string{internal.FexitProgramFunction: r.currentFexitProgram.Spec.FunctionName}

	prog, err := r.createBpfProgram(attachPoint, r, annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to create BpfProgram %s: %v", attachPoint, err)
	}

	progs.Items = append(progs.Items, *prog)

	return progs, nil
}

func (r *FexitProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentFexitProgram = &bpfmaniov1alpha1.FexitProgram{}
	r.finalizer = internal.FexitProgramControllerFinalizer
	r.recType = internal.FexitString
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("fexit")

	r.Logger.Info("bpfman-agent enter: fexit", "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	fexitPrograms := &bpfmaniov1alpha1.FexitProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, fexitPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting FexitPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(fexitPrograms.Items) == 0 {
		r.Logger.Info("FexitProgramController found no Fexit Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of fexit programs to pass into reconcileCommon()
	var fexitObjects []client.Object = make([]client.Object, len(fexitPrograms.Items))
	for i := range fexitPrograms.Items {
		fexitObjects[i] = &fexitPrograms.Items[i]
	}

	// Reconcile each FexitProgram.
	_, result, err := r.reconcileCommon(ctx, r, fexitObjects)
	return result, err
}

func (r *FexitProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentFexitProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentFexitProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Tracing),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FexitAttachInfo{
				FexitAttachInfo: &gobpfman.FexitAttachInfo{
					FnName: bpfProgram.Annotations[internal.FexitProgramFunction],
				},
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.getOwner().GetName()},
		GlobalData: r.currentFexitProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}
