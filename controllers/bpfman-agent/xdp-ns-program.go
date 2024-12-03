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
	internal "github.com/bpfman/bpfman-operator/internal"
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

//+kubebuilder:rbac:groups=bpfman.io,resources=xdpnsprograms,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=xdpnsprograms,verbs=get;list;watch

// BpfProgramReconciler reconciles a BpfNsProgram object
type XdpNsProgramReconciler struct {
	NamespaceProgramReconciler
	currentXdpNsProgram *bpfmaniov1alpha1.XdpNsProgram
	interfaces          []string
	ourNode             *v1.Node
}

func (r *XdpNsProgramReconciler) getFinalizer() string {
	return r.finalizer
}

func (r *XdpNsProgramReconciler) getOwner() metav1.Object {
	if r.appOwner == nil {
		return r.currentXdpNsProgram
	} else {
		return r.appOwner
	}
}

func (r *XdpNsProgramReconciler) getRecType() string {
	return r.recType
}

func (r *XdpNsProgramReconciler) getProgType() internal.ProgramType {
	return internal.Xdp
}

func (r *XdpNsProgramReconciler) getName() string {
	return r.currentXdpNsProgram.Name
}

func (r *XdpNsProgramReconciler) getNamespace() string {
	return r.currentXdpNsProgram.Namespace
}

func (r *XdpNsProgramReconciler) getNoContAnnotationIndex() string {
	return internal.XdpNsNoContainersOnNode
}

func (r *XdpNsProgramReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *XdpNsProgramReconciler) getBpfProgramCommon() *bpfmaniov1alpha1.BpfProgramCommon {
	return &r.currentXdpNsProgram.Spec.BpfProgramCommon
}

func (r *XdpNsProgramReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentXdpNsProgram.Spec.NodeSelector
}

func (r *XdpNsProgramReconciler) getBpfGlobalData() map[string][]byte {
	return r.currentXdpNsProgram.Spec.GlobalData
}

func (r *XdpNsProgramReconciler) getAppProgramId() string {
	return appProgramId(r.currentXdpNsProgram.GetLabels())
}

func (r *XdpNsProgramReconciler) setCurrentProgram(program client.Object) error {
	var err error
	var ok bool

	r.currentXdpNsProgram, ok = program.(*bpfmaniov1alpha1.XdpNsProgram)
	if !ok {
		return fmt.Errorf("failed to cast program to XdpNsProgram")
	}

	r.interfaces, err = getInterfaces(&r.currentXdpNsProgram.Spec.InterfaceSelector, r.ourNode)
	if err != nil {
		return fmt.Errorf("failed to get interfaces for XdpNsProgram: %v", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a XdpNsProgram is updated,
// load the program to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *XdpNsProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.XdpNsProgram{},
			builder.WithPredicates(predicate.And(
				predicate.GenerationChangedPredicate{},
				predicate.ResourceVersionChangedPredicate{}),
			),
		).
		Owns(&bpfmaniov1alpha1.BpfNsProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfNsProgramTypePredicate(internal.Xdp.String()),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the XdpNsProgram no longer select the Node. Additionally only
		// care about node events specific to our node
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

func (r *XdpNsProgramReconciler) getExpectedBpfPrograms(ctx context.Context) (*bpfmaniov1alpha1.BpfNsProgramList, error) {
	progs := &bpfmaniov1alpha1.BpfNsProgramList{}

	// There is a container selector, so see if there are any matching
	// containers on this node.
	containerInfo, err := r.Containers.GetContainers(
		ctx,
		r.getNamespace(),
		r.currentXdpNsProgram.Spec.Containers.Pods,
		r.currentXdpNsProgram.Spec.Containers.ContainerNames,
		r.Logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get container pids: %v", err)
	}

	if containerInfo == nil || len(*containerInfo) == 0 {
		// There were no errors, but the container selector didn't
		// select any containers on this node.
		for _, iface := range r.interfaces {
			attachPoint := fmt.Sprintf("%s-%s",
				iface,
				"no-containers-on-node",
			)

			annotations := map[string]string{
				internal.XdpNsProgramInterface:   iface,
				internal.XdpNsNoContainersOnNode: "true",
			}

			prog, err := r.createBpfProgram(attachPoint, r, annotations)
			if err != nil {
				return nil, fmt.Errorf("failed to create BpfProgram %s: %v", attachPoint, err)
			}

			progs.Items = append(progs.Items, *prog)
		}
	} else {
		// Containers were found, so create bpfPrograms.
		for i := range *containerInfo {
			container := (*containerInfo)[i]
			for _, iface := range r.interfaces {
				attachPoint := fmt.Sprintf("%s-%s-%s",
					iface,
					container.podName,
					container.containerName,
				)

				annotations := map[string]string{
					internal.XdpNsProgramInterface: iface,
					internal.XdpNsContainerPid:     strconv.FormatInt(container.pid, 10),
				}

				prog, err := r.createBpfProgram(attachPoint, r, annotations)
				if err != nil {
					return nil, fmt.Errorf("failed to create BpfProgram %s: %v", attachPoint, err)
				}

				progs.Items = append(progs.Items, *prog)
			}
		}
	}

	return progs, nil
}

func (r *XdpNsProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentXdpNsProgram = &bpfmaniov1alpha1.XdpNsProgram{}
	r.finalizer = internal.XdpNsProgramControllerFinalizer
	r.recType = internal.Xdp.String()
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("xdp-ns")

	r.Logger.Info("bpfman-agent enter: xdp-ns", "Namespace", req.Namespace, "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	xdpPrograms := &bpfmaniov1alpha1.XdpNsProgramList{}
	opts := []client.ListOption{}

	if err := r.List(ctx, xdpPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting XdpNsPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(xdpPrograms.Items) == 0 {
		r.Logger.Info("XdpNsProgramController found no XDP Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	// Create a list of Xdp programs to pass into reconcileCommon()
	var xdpObjects []client.Object = make([]client.Object, len(xdpPrograms.Items))
	for i := range xdpPrograms.Items {
		xdpObjects[i] = &xdpPrograms.Items[i]
	}

	// Reconcile each TcProgram.
	_, result, err := r.reconcileCommon(ctx, r, xdpObjects)
	return result, err
}

func (r *XdpNsProgramReconciler) getLoadRequest(bpfProgram *bpfmaniov1alpha1.BpfNsProgram, mapOwnerId *uint32) (*gobpfman.LoadRequest, error) {
	bytecode, err := bpfmanagentinternal.GetBytecode(r.Client, &r.currentXdpNsProgram.Spec.ByteCode)
	if err != nil {
		return nil, fmt.Errorf("failed to process bytecode selector: %v", err)
	}

	attachInfo := &gobpfman.XDPAttachInfo{
		Priority:  r.currentXdpNsProgram.Spec.Priority,
		Iface:     bpfProgram.Annotations[internal.XdpNsProgramInterface],
		ProceedOn: xdpProceedOnToInt(r.currentXdpNsProgram.Spec.ProceedOn),
	}

	containerPidStr, ok := bpfProgram.Annotations[internal.XdpNsContainerPid]
	if ok {
		netns := fmt.Sprintf("/host/proc/%s/ns/net", containerPidStr)
		attachInfo.Netns = &netns
	}

	loadRequest := gobpfman.LoadRequest{
		Bytecode:    bytecode,
		Name:        r.currentXdpNsProgram.Spec.BpfFunctionName,
		ProgramType: uint32(internal.Xdp),
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_XdpAttachInfo{
				XdpAttachInfo: attachInfo,
			},
		},
		Metadata:   map[string]string{internal.UuidMetadataKey: string(bpfProgram.UID), internal.ProgramNameKey: r.getOwner().GetName()},
		GlobalData: r.currentXdpNsProgram.Spec.GlobalData,
		MapOwnerId: mapOwnerId,
	}

	return &loadRequest, nil
}
