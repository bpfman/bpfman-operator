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

//+kubebuilder:rbac:groups=bpfman.io,resources=tcnsprograms,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=tcnsprograms,verbs=get;list;watch

// TcNsProgramReconciler reconciles a TcNsProgram object by creating multiple
// BpfNsProgram objects and managing bpfman for each one.
type TcNsProgramReconciler struct {
	NamespaceProgramReconciler
	currentTcNsProgram *bpfmaniov1alpha1.TcNsProgram
	interfaces         []string
	ourNode            *v1.Node
}

func (r *TcNsProgramReconciler) getFinalizer() string {
	return r.finalizer
}

func (r *TcNsProgramReconciler) getOwner() metav1.Object {
	if r.appOwner == nil {
		return r.currentTcNsProgram
	} else {
		return r.appOwner
	}
}

func (r *TcNsProgramReconciler) getRecType() string {
	return r.recType
}

func (r *TcNsProgramReconciler) getProgType() internal.ProgramType {
	return internal.Tc
}

func (r *TcNsProgramReconciler) getName() string {
	return r.currentTcNsProgram.Name
}

func (r *TcNsProgramReconciler) getNamespace() string {
	return r.currentTcNsProgram.Namespace
}

func (r *TcNsProgramReconciler) getNoContAnnotationIndex() string {
	return internal.TcNsNoContainersOnNode
}

func (r *TcNsProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *TcNsProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentTcNsProgram.Spec.BpfProgramCommon
}

func (r *TcNsProgramReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentTcNsProgram.Spec.NodeSelector
}

func (r *TcNsProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.currentTcNsProgram.Spec.GlobalData
}

func (r *TcNsProgramReconciler) getAppProgramId() string {
	return appProgramId(r.currentTcNsProgram.GetLabels())
}

func (r *TcNsProgramReconciler) setCurrentProgram(program client.Object) error {
	var err error
	var ok bool

	r.currentTcNsProgram, ok = program.(*bpfmaniov1alpha1.TcNsProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to TcNsProgram")
	}

	r.interfaces, err = getInterfaces(&r.currentTcNsProgram.Spec.InterfaceSelector, r.ourNode)
	if err != nil {
		return fmt.Errorf("failed to get interfaces for TcNsProgram: %v", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a TcNsProgram is updated,
// load the program to the node via bpfman, and then create BpfNsProgram object(s)
// to reflect per node state information.
func (r *TcNsProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.TcNsProgram{}, builder.WithPredicates(predicate.And(
			predicate.GenerationChangedPredicate{},
			predicate.ResourceVersionChangedPredicate{}),
		),
		).
		Owns(&bpfmaniov1alpha1.BpfNsProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfNsProgramTypePredicate(internal.Tc.String()),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the TcNsProgram no longer select the Node. Additionally only
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

func (r *TcNsProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfNsProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfNsProgramList{}

	// There is a container selector, so see if there are any matching
	// containers on this node.
	containerInfo, err := r.Containers.GetContainers(
		ctx,
		r.getNamespace(),
		r.currentTcNsProgram.Spec.Containers.Pods,
		r.currentTcNsProgram.Spec.Containers.ContainerNames,
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
				r.currentTcNsProgram.Spec.Direction,
				"no-containers-on-node",
			)

			annotations := map[string]string{
				internal.TcNsProgramInterface:   iface,
				internal.TcNsNoContainersOnNode: "true",
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
					r.currentTcNsProgram.Spec.Direction,
					container.podName,
					container.containerName,
				)

				annotations := map[string]string{
					internal.TcNsProgramInterface: iface,
					internal.TcNsContainerPid:     strconv.FormatInt(container.pid, 10),
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

func (r *TcNsProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentTcNsProgram = &bpfmaniov1alpha1.TcNsProgram{}
	r.finalizer = internal.TcNsProgramControllerFinalizer
	r.recType = internal.Tc.String()
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("tc-ns")

	r.Logger.Info("bpfman-agent enter: tc-ns", "Namespace", req.Namespace, "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	tcPrograms := &bpfmaniov1alpha1.TcNsProgramList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, tcPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting TcNsPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(tcPrograms.Items) == 0 {
		r.Logger.Info("TcNsProgramController found no TC Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of tc programs to pass into reconcileCommon()
	var tcObjects []client.Object = make([]client.Object, len(tcPrograms.Items))
	for i := range tcPrograms.Items {
		tcObjects[i] = &tcPrograms.Items[i]
	}

	// Reconcile each TcNsProgram.
	_, result, err := r.reconcileCommon(ctx, r, tcObjects)
	return result, err
}

func (r *TcNsProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfNsProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentTcNsProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	attachInfo := &gobpfman.TCAttachInfo{
		Priority:  r.currentTcNsProgram.Spec.Priority,
		Iface:     bpfProgram.Annotations[internal.TcNsProgramInterface],
		Direction: r.currentTcNsProgram.Spec.Direction,
		ProceedOn: tcProceedOnToInt(r.currentTcNsProgram.Spec.ProceedOn),
	}

	containerPidStr, ok := bpfProgram.Annotations[internal.TcNsContainerPid]
	if ok {
		netns := fmt.Sprintf("/host/proc/%s/ns/net", containerPidStr)
		attachInfo.Netns = &netns
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentTcNsProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Tc),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcAttachInfo{
				TcAttachInfo: attachInfo,
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.getOwner().GetName()},
		GlobalData: r.currentTcNsProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}
