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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//+kubebuilder:rbac:groups=bpfman.io,resources=bpfapplications,verbs=get;list;watch

type BpfApplicationReconciler struct {
	ReconcilerCommon
	currentApp *bpfmaniov1alpha1.BpfApplication
	ourNode    *v1.Node
}

func (r *BpfApplicationReconciler) getRecType() string {
	return internal.ApplicationString
}

func (r *BpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentApp = &bpfmaniov1alpha1.BpfApplication{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("application")
	r.appOwner = &bpfmaniov1alpha1.BpfApplication{}
	r.finalizer = internal.BpfApplicationControllerFinalizer
	r.recType = internal.ApplicationString

	ctxLogger := log.FromContext(ctx)
	ctxLogger.Info("Reconcile Application: Enter", "ReconcileKey", req)

	// Lookup K8s node object for this bpfman-agent This should always succeed
	if err := r.Get(ctx, types.NamespacedName{Namespace: v1.NamespaceAll, Name: r.NodeName}, r.ourNode); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting bpfman-agent node %s : %v",
			req.NamespacedName, err)
	}

	appPrograms := &bpfmaniov1alpha1.BpfApplicationList{}

	opts := []client.ListOption{}

	if err := r.List(ctx, appPrograms, opts...); err != nil {
		return ctrl.Result{Requeue: false}, fmt.Errorf("failed getting BpfApplicationPrograms for full reconcile %s : %v",
			req.NamespacedName, err)
	}

	if len(appPrograms.Items) == 0 {
		r.Logger.Info("BpfApplicationController found no application Programs")
		return ctrl.Result{Requeue: false}, nil
	}

	var res ctrl.Result
	var err error
	var complete bool

	namePrefix := func(
		app bpfmaniov1alpha1.BpfApplication,
		prog bpfmaniov1alpha1.BpfApplicationProgram) string {
		return app.Name + "-" + strings.ToLower(string(prog.Type)) + "-"
	}

	for i, a := range appPrograms.Items {
		for j, p := range a.Spec.Programs {
			switch p.Type {
			case bpfmaniov1alpha1.ProgTypeFentry:
				fentryProgram := bpfmaniov1alpha1.FentryProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: namePrefix(a, p) + sanitize(p.Fentry.FunctionName),
					},
					Spec: bpfmaniov1alpha1.FentryProgramSpec{
						FentryProgramInfo: *p.Fentry,
						BpfAppCommon:      a.Spec.BpfAppCommon,
					},
				}
				rec := &FentryProgramReconciler{
					ReconcilerCommon:     r.ReconcilerCommon,
					currentFentryProgram: &fentryProgram,
					ourNode:              r.ourNode,
				}
				rec.appOwner = &a
				fentryObjects := []client.Object{&fentryProgram}
				// Reconcile FentryProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, fentryObjects)

			case bpfmaniov1alpha1.ProgTypeFexit:
				fexitProgram := bpfmaniov1alpha1.FexitProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: namePrefix(a, p) + sanitize(p.Fexit.FunctionName),
					},
					Spec: bpfmaniov1alpha1.FexitProgramSpec{
						FexitProgramInfo: *p.Fexit,
						BpfAppCommon:     a.Spec.BpfAppCommon,
					},
				}
				rec := &FexitProgramReconciler{
					ReconcilerCommon:    r.ReconcilerCommon,
					currentFexitProgram: &fexitProgram,
					ourNode:             r.ourNode,
				}
				rec.appOwner = &a
				fexitObjects := []client.Object{&fexitProgram}
				// Reconcile FexitProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, fexitObjects)

			case bpfmaniov1alpha1.ProgTypeKprobe,
				bpfmaniov1alpha1.ProgTypeKretprobe:
				kprobeProgram := bpfmaniov1alpha1.KprobeProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: namePrefix(a, p) + sanitize(p.Kprobe.FunctionName),
					},
					Spec: bpfmaniov1alpha1.KprobeProgramSpec{
						KprobeProgramInfo: *p.Kprobe,
						BpfAppCommon:      a.Spec.BpfAppCommon,
					},
				}
				rec := &KprobeProgramReconciler{
					ReconcilerCommon:     r.ReconcilerCommon,
					currentKprobeProgram: &kprobeProgram,
					ourNode:              r.ourNode,
				}
				rec.appOwner = &a
				kprobeObjects := []client.Object{&kprobeProgram}
				// Reconcile KprobeProgram or KpretprobeProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, kprobeObjects)

			case bpfmaniov1alpha1.ProgTypeUprobe,
				bpfmaniov1alpha1.ProgTypeUretprobe:
				uprobeProgram := bpfmaniov1alpha1.UprobeProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: namePrefix(a, p) + sanitize(p.Uprobe.FunctionName),
					},
					Spec: bpfmaniov1alpha1.UprobeProgramSpec{
						UprobeProgramInfo: *p.Uprobe,
						BpfAppCommon:      a.Spec.BpfAppCommon,
					},
				}
				rec := &UprobeProgramReconciler{
					ReconcilerCommon:     r.ReconcilerCommon,
					currentUprobeProgram: &uprobeProgram,
					ourNode:              r.ourNode,
				}
				rec.appOwner = &a
				uprobeObjects := []client.Object{&uprobeProgram}
				// Reconcile UprobeProgram or UpretprobeProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, uprobeObjects)

			case bpfmaniov1alpha1.ProgTypeTracepoint:
				tracepointProgram := bpfmaniov1alpha1.TracepointProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: namePrefix(a, p) + sanitize(p.Tracepoint.Names[0]),
					},
					Spec: bpfmaniov1alpha1.TracepointProgramSpec{
						TracepointProgramInfo: *p.Tracepoint,
						BpfAppCommon:          a.Spec.BpfAppCommon,
					},
				}
				rec := &TracepointProgramReconciler{
					ReconcilerCommon:         r.ReconcilerCommon,
					currentTracepointProgram: &tracepointProgram,
					ourNode:                  r.ourNode,
				}
				rec.appOwner = &a
				tracepointObjects := []client.Object{&tracepointProgram}
				// Reconcile TracepointProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, tracepointObjects)

			case bpfmaniov1alpha1.ProgTypeTC,
				bpfmaniov1alpha1.ProgTypeTCX:
				interfaces, ifErr := getInterfaces(&p.TC.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					ctxLogger.Error(ifErr, "failed to get interfaces for TC Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				tcProgram := bpfmaniov1alpha1.TcProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: namePrefix(a, p) + p.TC.Direction + "-" + interfaces[0],
					},
					Spec: bpfmaniov1alpha1.TcProgramSpec{
						TcProgramInfo: *p.TC,
						BpfAppCommon:  a.Spec.BpfAppCommon,
					},
				}
				rec := &TcProgramReconciler{
					ReconcilerCommon: r.ReconcilerCommon,
					currentTcProgram: &tcProgram,
					ourNode:          r.ourNode,
				}
				rec.appOwner = &a
				tcObjects := []client.Object{&tcProgram}
				// Reconcile TcProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, tcObjects)

			case bpfmaniov1alpha1.ProgTypeXDP:
				interfaces, ifErr := getInterfaces(&p.XDP.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					ctxLogger.Error(ifErr, "failed to get interfaces for XDP Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				xdpProgram := bpfmaniov1alpha1.XdpProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: namePrefix(a, p) + interfaces[0],
					},
					Spec: bpfmaniov1alpha1.XdpProgramSpec{
						XdpProgramInfo: *p.XDP,
						BpfAppCommon:   a.Spec.BpfAppCommon,
					},
				}
				rec := &XdpProgramReconciler{
					ReconcilerCommon:  r.ReconcilerCommon,
					currentXdpProgram: &xdpProgram,
					ourNode:           r.ourNode,
				}
				rec.appOwner = &a
				xdpObjects := []client.Object{&xdpProgram}
				// Reconcile XdpProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, xdpObjects)

			default:
				r.Logger.Error(fmt.Errorf("unsupported bpf program type"), "unsupported bpf program type", "ProgType", p.Type)
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
			// We've completed reconciling all programs for this application, continue to the next one
			continue
		} else {
			return res, err
		}
	}

	return res, err
}

// SetupWithManager sets up the controller with the Manager.
// The Bpfman-Agent should reconcile whenever a BpfApplication object is updated,
// load the programs to the node via bpfman, and then create a bpfProgram object
// to reflect per node state information.
func (r *BpfApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bpfmaniov1alpha1.BpfApplication{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, predicate.ResourceVersionChangedPredicate{}))).
		Owns(&bpfmaniov1alpha1.BpfProgram{},
			builder.WithPredicates(predicate.And(
				internal.BpfProgramTypePredicate(internal.ApplicationString),
				internal.BpfProgramNodePredicate(r.NodeName)),
			),
		).
		// Only trigger reconciliation if node labels change since that could
		// make the BpfApplication no longer select the Node. Additionally only
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
