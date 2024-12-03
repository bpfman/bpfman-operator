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

package bpfmanagent

import (
	"context"
	"fmt"
	"strconv"

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

//+kubebuilder:rbac:groups=bpfman.io,resources=tcxnsprograms,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=tcxnsprograms,verbs=get;list;watch

// TcxNsProgramReconciler reconciles a tcxNsProgram object by creating multiple
// bpfNsProgram objects and managing bpfman for each one.
type TcxNsProgramReconciler struct {
	NamespaceProgramReconciler
	currentTcxNsProgram *bpfmaniov1alpha1.TcxNsProgram
	interfaces          []string
	ourNode             *v1.Node
}

func (r *TcxNsProgramReconciler) getFinalizer() string {
	return r.finalizer
}

func (r *TcxNsProgramReconciler) getOwner() metav1.Object {
	if r.appOwner == nil {
		return r.currentTcxNsProgram
	} else {
		return r.appOwner
	}
}

func (r *TcxNsProgramReconciler) getRecType() string {
	return r.recType
}

func (r *TcxNsProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tc
}

func (r *TcxNsProgramReconciler) getName() string {
	return r.currentTcxNsProgram.Name
}

func (r *TcxNsProgramReconciler) getNamespace() string {
	return r.currentTcxNsProgram.Namespace
}

func (r *TcxNsProgramReconciler) getNoContAnnotationIndex() string {
	return internal.TcxNsNoContainersOnNode
}

func (r *TcxNsProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *TcxNsProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentTcxNsProgram.Spec.BpfProgramCommon
}

func (r *TcxNsProgramReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentTcxNsProgram.Spec.NodeSelector
}

func (r *TcxNsProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.currentTcxNsProgram.Spec.GlobalData
}

func (r *TcxNsProgramReconciler) getAppProgramId() string {
	return appProgramId(r.currentTcxNsProgram.GetLabels())
}

func (r *TcxNsProgramReconciler) setCurrentProgram(program client.Object) error {
	var err error
	var ok bool

	r.currentTcxNsProgram, ok = program.(*bpfmaniov1alpha1.TcxNsProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to TcxNsProgram")
	}

	r.interfaces, err = getInterfaces(&r.currentTcxNsProgram.Spec.InterfaceSelector, r.ourNode)
	if err != nil {
		return fmt.Errorf("failed to get interfaces for TcxNsProgram: %v", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a TcxNsProgram is updated,
// load the program to the node via bpfman, and then create bpfNsProgram object(s)
// to reflect per node state information.
func (r *TcxNsProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.TcxNsProgram{}, builder.WithPredicates(predicate.And(
			predicate.GenerationChangedPredicate{},
			predicate.ResourceVersionChangedPredicate{}),
		),
		).
		Owns(&bpfmaniov1alpha1.BpfNsProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfNsProgramTypePredicate(internal.TcxString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the TcxNsProgram no longer select the Node. Additionally only
		// care about events specific to our node
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

func (r *TcxNsProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfNsProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfNsProgramList{}

	// There is a container selector, so see if there are any matching
	// containers on this node.
	containerInfo, err := r.Containers.GetContainers(
		ctx,
		r.getNamespace(),
		r.currentTcxNsProgram.Spec.Containers.Pods,
		r.currentTcxNsProgram.Spec.Containers.ContainerNames,
		r.Logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get container pids: %v", err)
	}

	if containerInfo == nil || len(*containerInfo) == 0 {
		// There were no errors, but the container selector didn't
		// select any containers on this node.
		for _, iface := range r.interfaces {
			attachPoint := fmt.Sprintf("%s-%s-%s",
				iface,
				r.currentTcxNsProgram.Spec.Direction,
				"no-containers-on-node",
			)

			annotations := map[string]string{
				internal.TcxNsProgramInterface:   iface,
				internal.TcxNsNoContainersOnNode: "true",
			}

			prog, err := r.createBpfProgram(attachPoint, r, annotations)
			if err != nil {
				return nil, fmt.Errorf("failed to create BpfNsProgram %s: %v", attachPoint, err)
			}

			progs.Items = append(progs.Items, *prog)
		}
	} else {
		// Containers were found, so create BpfNsPrograms.
		for i := range *containerInfo {
			container := (*containerInfo)[i]
			for _, iface := range r.interfaces {
				attachPoint := fmt.Sprintf("%s-%s-%s-%s",
					iface,
					r.currentTcxNsProgram.Spec.Direction,
					container.podName,
					container.containerName,
				)

				annotations := map[string]string{
					internal.TcxNsProgramInterface: iface,
					internal.TcxNsContainerPid:     strconv.FormatInt(container.pid, 10),
				}

				prog, err := r.createBpfProgram(attachPoint, r, annotations)
				if err != nil {
					return nil, fmt.Errorf("failed to create BpfNsProgram %s: %v", attachPoint, err)
				}

				progs.Items = append(progs.Items, *prog)
			}
		}
	}

	return progs, nil
}

func (r *TcxNsProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentTcxNsProgram = &bpfmaniov1alpha1.TcxNsProgram{}
	r.finalizer = internal.TcxNsProgramControllerFinalizer
	r.recType = internal.TcxString
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("tcx-ns")

	r.Logger.Info("bpfman-agent enter: tcx-ns", "Namespace", req.Namespace, "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	tcxPrograms := &bpfmaniov1alpha1.TcxNsProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, tcxPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting TcxNsPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(tcxPrograms.Items) == 0 {
		r.Logger.Info("TcxNsProgramController found no TCX NS Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of tcx programs to pass into reconcileCommon()
	var tcxObjects []client.Object = make([]client.Object, len(tcxPrograms.Items))
	for i := range tcxPrograms.Items {
		tcxObjects[i] = &tcxPrograms.Items[i]
	}

	// Reconcile each TcxNsProgram.
	_, result, err := r.reconcileCommon(ctx, r, tcxObjects)
	return result, err
}

func (r *TcxNsProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfNsProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentTcxNsProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	attachInfo := &gobpfman.TCXAttachInfo{
		Priority:  r.currentTcxNsProgram.Spec.Priority,
		Iface:     bpfProgram.Annotations[internal.TcxNsProgramInterface],
		Direction: r.currentTcxNsProgram.Spec.Direction,
	}

	containerPidStr, ok := bpfProgram.Annotations[internal.TcxNsContainerPid]
	if ok {
		netns := fmt.Sprintf("/host/proc/%s/ns/net", containerPidStr)
		attachInfo.Netns = &netns
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentTcxNsProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Tc),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcxAttachInfo{
				TcxAttachInfo: attachInfo,
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.getOwner().GetName()},
		GlobalData: r.currentTcxNsProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}
