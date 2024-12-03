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
	"strings"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfnsapplications,verbs=get;list;watch
//+kubebuilder:rbac:groups=bpfman.io,namespace=bpfman,resources=bpfnsapplications,verbs=get;list;watch

type BpfNsApplicationReconciler struct {
	NamespaceProgramReconciler
	currentApp *bpfmaniov1alpha1.BpfNsApplication
	ourNode    *v1.Node
}

func (r *BpfNsApplicationReconciler) getRecType() string {
	return internal.ApplicationString
}

func (r *BpfNsApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentApp = &bpfmaniov1alpha1.BpfNsApplication{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("application-ns")
	r.appOwner = &bpfmaniov1alpha1.BpfNsApplication{}
	r.finalizer = internal.BpfNsApplicationControllerFinalizer
	r.recType = internal.ApplicationString

	r.Logger.Info("bpfman-agent enter: application-ns", "Namespace", req.Namespace, "Name", req.Name)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	appPrograms := &bpfmaniov1alpha1.BpfNsApplicationList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, appPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfNsApplicationPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(appPrograms.Items) == 0 {
		r.Logger.Info("BpfNsApplicationController found no application Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	var res ctrl.Result
	var err error
	var complete bool
	var lastRec bpfmanReconciler[bpfmaniov1alpha1.BpfNsProgram, bpfmaniov1alpha1.BpfNsProgramList]

	buildProgramName := func(
		app bpfmaniov1alpha1.BpfNsApplication,
		prog bpfmaniov1alpha1.BpfNsApplicationProgram) string {
		return app.Name + "-" + strings.ToLower(string(prog.Type))
	}

	for i, a := range appPrograms.Items {
		var appProgramMap = make(map[string]bool)
		for j, p := range a.Spec.Programs {
			switch p.Type {
			case bpfmaniov1alpha1.ProgTypeUprobe,
				bpfmaniov1alpha1.ProgTypeUretprobe:
				appProgramId := fmt.Sprintf("%s-%s-%s", strings.ToLower(string(p.Type)), sanitize(p.Uprobe.FunctionName), p.Uprobe.BpfFunctionName)
				uprobeProgram := bpfmaniov1alpha1.UprobeNsProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:      buildProgramName(a, p),
						Namespace: req.Namespace,
						Labels:    map[string]string{internal.AppProgramId: appProgramId}},
					Spec: bpfmaniov1alpha1.UprobeNsProgramSpec{
						UprobeNsProgramInfo: *p.Uprobe,
						BpfAppCommon:        a.Spec.BpfAppCommon,
					},
				}
				rec := &UprobeNsProgramReconciler{
					NamespaceProgramReconciler: r.NamespaceProgramReconciler,
					currentUprobeNsProgram:     &uprobeProgram,
					ourNode:                    r.ourNode,
				}
				rec.appOwner = &a
				uprobeObjects := []client.Object{&uprobeProgram}
				appProgramMap[appProgramId] = true
				// Reconcile UprobeNsProgram or UretprobeNsProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, uprobeObjects)
				lastRec = rec

			case bpfmaniov1alpha1.ProgTypeTC:
				_, ifErr := getInterfaces(&p.TC.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					r.Logger.Error(ifErr, "failed to get interfaces for TC NS Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				appProgramId := fmt.Sprintf("%s-%s-%s", strings.ToLower(string(p.Type)), p.TC.Direction, p.TC.BpfFunctionName)
				tcProgram := bpfmaniov1alpha1.TcNsProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:      buildProgramName(a, p),
						Namespace: req.Namespace,
						Labels:    map[string]string{internal.AppProgramId: appProgramId}},
					Spec: bpfmaniov1alpha1.TcNsProgramSpec{
						TcNsProgramInfo: *p.TC,
						BpfAppCommon:    a.Spec.BpfAppCommon,
					},
				}
				rec := &TcNsProgramReconciler{
					NamespaceProgramReconciler: r.NamespaceProgramReconciler,
					currentTcNsProgram:         &tcProgram,
					ourNode:                    r.ourNode,
				}
				rec.appOwner = &a
				tcObjects := []client.Object{&tcProgram}
				appProgramMap[appProgramId] = true
				// Reconcile TcNsProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, tcObjects)
				lastRec = rec

			case bpfmaniov1alpha1.ProgTypeTCX:
				_, ifErr := getInterfaces(&p.TCX.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					r.Logger.Error(ifErr, "failed to get interfaces for TCX Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				appProgramId := fmt.Sprintf("%s-%s-%s", strings.ToLower(string(p.Type)), p.TCX.Direction, p.TCX.BpfFunctionName)
				tcxProgram := bpfmaniov1alpha1.TcxNsProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:      buildProgramName(a, p),
						Namespace: req.Namespace,
						Labels:    map[string]string{internal.AppProgramId: appProgramId}},
					Spec: bpfmaniov1alpha1.TcxNsProgramSpec{
						TcxNsProgramInfo: *p.TCX,
						BpfAppCommon:     a.Spec.BpfAppCommon,
					},
				}
				rec := &TcxNsProgramReconciler{
					NamespaceProgramReconciler: r.NamespaceProgramReconciler,
					currentTcxNsProgram:        &tcxProgram,
					ourNode:                    r.ourNode,
				}
				rec.appOwner = &a
				tcxObjects := []client.Object{&tcxProgram}
				appProgramMap[appProgramId] = true
				// Reconcile TcxNsProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, tcxObjects)
				lastRec = rec

			case bpfmaniov1alpha1.ProgTypeXDP:
				_, ifErr := getInterfaces(&p.XDP.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					r.Logger.Error(ifErr, "failed to get interfaces for XDP Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				appProgramId := fmt.Sprintf("%s-%s", strings.ToLower(string(p.Type)), p.XDP.BpfFunctionName)
				xdpProgram := bpfmaniov1alpha1.XdpNsProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:      buildProgramName(a, p),
						Namespace: req.Namespace,
						Labels:    map[string]string{internal.AppProgramId: appProgramId}},
					Spec: bpfmaniov1alpha1.XdpNsProgramSpec{
						XdpNsProgramInfo: *p.XDP,
						BpfAppCommon:     a.Spec.BpfAppCommon,
					},
				}
				rec := &XdpNsProgramReconciler{
					NamespaceProgramReconciler: r.NamespaceProgramReconciler,
					currentXdpNsProgram:        &xdpProgram,
					ourNode:                    r.ourNode,
				}
				rec.appOwner = &a
				xdpObjects := []client.Object{&xdpProgram}
				appProgramMap[appProgramId] = true
				// Reconcile XdpNsProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, xdpObjects)
				lastRec = rec

			default:
				r.Logger.Error(fmt.Errorf("unsupported bpf namespaced program type"), "unsupported bpf namespaced program type", "ProgType", p.Type)
				// Skip this program and continue to the next one
				continue
			}

			r.Logger.V(1).Info("Reconcile Application", "Application", i, "Program", j, "Name", a.Name,
				"type", p.Type, "Complete", complete, "Result", res, "Error", err)

			if complete {
				// We've completed reconciling this program, continue to the next one
				continue
			} else {
				return res, err
			}
		}

		if complete {
			bpfPrograms := &bpfmaniov1alpha1.BpfNsProgramList{}
			bpfDeletedPrograms := &bpfmaniov1alpha1.BpfNsProgramList{}
			// find programs that need to be deleted and delete them
			opts := []client.ListOption{client.MatchingLabels{internal.BpfProgramOwner: a.Name}}
			if err := r.List(ctx, bpfPrograms, opts...); err != nil {
				r.Logger.Error(err, "failed to get freshPrograms for full reconcile")
				return ctrl.Result{}, err
			}
			for _, bpfProgram := range bpfPrograms.Items {
				id := bpfProgram.Labels[internal.AppProgramId]
				if _, ok := appProgramMap[id]; !ok {
					r.Logger.Info("Deleting BpfNsProgram", "AppProgramId", id, "BpfNsProgram", bpfProgram.Name)
					bpfDeletedPrograms.Items = append(bpfDeletedPrograms.Items, bpfProgram)
				}
			}
			// Delete BpfNsPrograms that are no longer needed
			res, err = r.unLoadAndDeleteBpfProgramsList(ctx, lastRec, bpfDeletedPrograms, internal.BpfNsApplicationControllerFinalizer)
			if err != nil {
				r.Logger.Error(err, "failed to delete programs")
				return ctrl.Result{}, err
			}
			// We've completed reconciling all programs for this application, continue to the next one
			continue
		} else {
			return res, err
		}
	}

	return res, err
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a BpfNsApplication object is updated,
// load the programs to the node via bpfman, and then create a BpfNsProgram object
// to reflect per node state information.
func (r *BpfNsApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.BpfNsApplication{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfNsProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfNsProgramTypePredicate(internal.ApplicationString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the BpfNsApplication no longer select the Node. Additionally only
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
