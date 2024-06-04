package bpfmanagent

import (
	"context"
	"fmt"

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

type BpfApplicationReconciler struct {
	ReconcilerCommon
	currentApp *bpfmaniov1alpha1.BpfApplication
	ourNode    *v1.Node
}

func (r *BpfApplicationReconciler) getFinalizer() string {
	return internal.BpfApplicationControllerFinalizer
}

func (r *BpfApplicationReconciler) getName() string {
	return r.currentApp.Name
}

func (r *BpfApplicationReconciler) getNode() *v1.Node {
	return r.ourNode
}

func (r *BpfApplicationReconciler) getNodeSelector() *metav1.LabelSelector {
	return &r.currentApp.Spec.NodeSelector
}

func (r *BpfApplicationReconciler) getBpfGlobalData() map[string][]byte {
	return r.currentApp.Spec.GlobalData
}

func (r *BpfApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Initialize node and current program
	r.currentApp = &bpfmaniov1alpha1.BpfApplication{}
	r.ourNode = &v1.Node{}
	r.Logger = ctrl.Log.WithName("application")

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

	for _, a := range appPrograms.Items {
		for _, p := range a.Spec.Programs {
			switch p.Type {
			case bpfmaniov1alpha1.ProgTypeFentry:
				fentryProgram := bpfmaniov1alpha1.FentryProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: a.Name + "fentry",
					},
					Spec: bpfmaniov1alpha1.FentryProgramSpec{
						FentryProgramInfo: *p.Fentry,
						BpfAppCommon:      a.Spec.BpfAppCommon,
					},
				}
				rec := &FentryProgramReconciler{
					ReconcilerCommon:     r.ReconcilerCommon,
					currentFentryProgram: &fentryProgram,
				}
				fentryObjects := []client.Object{&fentryProgram}
				// Reconcile FentryProgram.
				res, err = r.reconcileCommon(ctx, rec, fentryObjects)

			case bpfmaniov1alpha1.ProgTypeFexit:
				fexitProgram := bpfmaniov1alpha1.FexitProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: a.Name + "fexit",
					},
					Spec: bpfmaniov1alpha1.FexitProgramSpec{
						FexitProgramInfo: *p.Fexit,
						BpfAppCommon:     a.Spec.BpfAppCommon,
					},
				}
				rec := &FexitProgramReconciler{
					ReconcilerCommon:    r.ReconcilerCommon,
					currentFexitProgram: &fexitProgram,
				}
				fexitObjects := []client.Object{&fexitProgram}
				// Reconcile FexitProgram.
				res, err = r.reconcileCommon(ctx, rec, fexitObjects)

			case bpfmaniov1alpha1.ProgTypeKprobe,
				bpfmaniov1alpha1.ProgTypeKretprobe:
				kprobeProgram := bpfmaniov1alpha1.KprobeProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: a.Name + "kprobe",
					},
					Spec: bpfmaniov1alpha1.KprobeProgramSpec{
						KprobeProgramInfo: *p.Kprobe,
						BpfAppCommon:      a.Spec.BpfAppCommon,
					},
				}
				rec := &KprobeProgramReconciler{
					ReconcilerCommon:     r.ReconcilerCommon,
					currentKprobeProgram: &kprobeProgram,
				}
				kprobeObjects := []client.Object{&kprobeProgram}
				// Reconcile KprobeProgram or KpretprobeProgram.
				res, err = r.reconcileCommon(ctx, rec, kprobeObjects)

			case bpfmaniov1alpha1.ProgTypeUprobe,
				bpfmaniov1alpha1.ProgTypeUretprobe:
				uprobeProgram := bpfmaniov1alpha1.UprobeProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: a.Name + "uprobe",
					},
					Spec: bpfmaniov1alpha1.UprobeProgramSpec{
						UprobeProgramInfo: *p.Uprobe,
						BpfAppCommon:      a.Spec.BpfAppCommon,
					},
				}
				rec := &UprobeProgramReconciler{
					ReconcilerCommon:     r.ReconcilerCommon,
					currentUprobeProgram: &uprobeProgram,
				}
				uprobeObjects := []client.Object{&uprobeProgram}
				// Reconcile UprobeProgram or UpretprobeProgram.
				res, err = r.reconcileCommon(ctx, rec, uprobeObjects)

			case bpfmaniov1alpha1.ProgTypeTracepoint:
				tracepointProgram := bpfmaniov1alpha1.TracepointProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: a.Name + "tracepoint",
					},
					Spec: bpfmaniov1alpha1.TracepointProgramSpec{
						TracepointProgramInfo: *p.Tracepoint,
						BpfAppCommon:          a.Spec.BpfAppCommon,
					},
				}
				rec := &TracepointProgramReconciler{
					ReconcilerCommon:         r.ReconcilerCommon,
					currentTracepointProgram: &tracepointProgram,
				}
				tracepointObjects := []client.Object{&tracepointProgram}
				// Reconcile TracepointProgram.
				res, err = r.reconcileCommon(ctx, rec, tracepointObjects)

			case bpfmaniov1alpha1.ProgTypeTC,
				bpfmaniov1alpha1.ProgTypeTCX:
				tcProgram := bpfmaniov1alpha1.TcProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: a.Name + "tc",
					},
					Spec: bpfmaniov1alpha1.TcProgramSpec{
						TcProgramInfo: *p.TC,
						BpfAppCommon:  a.Spec.BpfAppCommon,
					},
				}
				rec := &TcProgramReconciler{
					ReconcilerCommon: r.ReconcilerCommon,
					currentTcProgram: &tcProgram,
				}
				tcObjects := []client.Object{&tcProgram}
				// Reconcile TcProgram.
				res, err = r.reconcileCommon(ctx, rec, tcObjects)

			case bpfmaniov1alpha1.ProgTypeXDP:
				xdpProgram := bpfmaniov1alpha1.XdpProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name: a.Name + "xdp",
					},
					Spec: bpfmaniov1alpha1.XdpProgramSpec{
						XdpProgramInfo: *p.XDP,
						BpfAppCommon:   a.Spec.BpfAppCommon,
					},
				}
				rec := &XdpProgramReconciler{
					ReconcilerCommon:  r.ReconcilerCommon,
					currentXdpProgram: &xdpProgram,
				}
				xdpObjects := []client.Object{&xdpProgram}
				// Reconcile XdpProgram.
				res, err = r.reconcileCommon(ctx, rec, xdpObjects)

			default:
				r.Logger.Info("Unsupported Bpf program type", "ProgType", p.Type)
				return ctrl.Result{Requeue: false}, nil
			}
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
		Complete(r)
}
