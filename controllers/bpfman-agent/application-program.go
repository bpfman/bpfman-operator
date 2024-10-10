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

	buildProgramName := func(
		app bpfmaniov1alpha1.BpfApplication,
		prog bpfmaniov1alpha1.BpfApplicationProgram) string {
		return app.Name + "-" + strings.ToLower(string(prog.Type))
	}

	for i, a := range appPrograms.Items {
		var appProgramMap = make(map[string]bool)
		for j, p := range a.Spec.Programs {
			switch p.Type {
			case bpfmaniov1alpha1.ProgTypeFentry:
				appProgramId := fmt.Sprintf("%s-%s", strings.ToLower(string(p.Type)), sanitize(p.Fentry.FunctionName))
				fentryProgram := bpfmaniov1alpha1.FentryProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
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
				appProgramMap[appProgramId] = true
				// Reconcile FentryProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, fentryObjects)

			case bpfmaniov1alpha1.ProgTypeFexit:
				appProgramId := fmt.Sprintf("%s-%s", strings.ToLower(string(p.Type)), sanitize(p.Fexit.FunctionName))
				fexitProgram := bpfmaniov1alpha1.FexitProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
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
				appProgramMap[appProgramId] = true
				// Reconcile FexitProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, fexitObjects)

			case bpfmaniov1alpha1.ProgTypeKprobe,
				bpfmaniov1alpha1.ProgTypeKretprobe:
				appProgramId := fmt.Sprintf("%s-%s", strings.ToLower(string(p.Type)), sanitize(p.Kprobe.FunctionName))
				kprobeProgram := bpfmaniov1alpha1.KprobeProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
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
				appProgramMap[appProgramId] = true
				// Reconcile KprobeProgram or KpretprobeProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, kprobeObjects)

			case bpfmaniov1alpha1.ProgTypeUprobe,
				bpfmaniov1alpha1.ProgTypeUretprobe:
				appProgramId := fmt.Sprintf("%s-%s", strings.ToLower(string(p.Type)), sanitize(p.Uprobe.FunctionName))
				uprobeProgram := bpfmaniov1alpha1.UprobeProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
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
				appProgramMap[appProgramId] = true
				// Reconcile UprobeProgram or UpretprobeProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, uprobeObjects)

			case bpfmaniov1alpha1.ProgTypeTracepoint:
				appProgramId := fmt.Sprintf("%s-%s", strings.ToLower(string(p.Type)), sanitize(p.Tracepoint.Names[0]))
				tracepointProgram := bpfmaniov1alpha1.TracepointProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
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
				appProgramMap[appProgramId] = true
				// Reconcile TracepointProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, tracepointObjects)

			case bpfmaniov1alpha1.ProgTypeTC:
				interfaces, ifErr := getInterfaces(&p.TC.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					ctxLogger.Error(ifErr, "failed to get interfaces for TC Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				appProgramId := fmt.Sprintf("%s-%s-%s", strings.ToLower(string(p.Type)), p.TC.Direction, interfaces[0])
				tcProgram := bpfmaniov1alpha1.TcProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
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
				appProgramMap[appProgramId] = true
				// Reconcile TcProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, tcObjects)

			case bpfmaniov1alpha1.ProgTypeTCX:
				interfaces, ifErr := getInterfaces(&p.TCX.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					ctxLogger.Error(ifErr, "failed to get interfaces for TCX Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				appProgramId := fmt.Sprintf("%s-%s-%s", strings.ToLower(string(p.Type)), p.TCX.Direction, interfaces[0])
				tcxProgram := bpfmaniov1alpha1.TcxProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
					Spec: bpfmaniov1alpha1.TcxProgramSpec{
						TcxProgramInfo: *p.TCX,
						BpfAppCommon:   a.Spec.BpfAppCommon,
					},
				}
				rec := &TcxProgramReconciler{
					ReconcilerCommon:  r.ReconcilerCommon,
					currentTcxProgram: &tcxProgram,
					ourNode:           r.ourNode,
				}
				rec.appOwner = &a
				tcxObjects := []client.Object{&tcxProgram}
				appProgramMap[appProgramId] = true
				// Reconcile TcxProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, tcxObjects)

			case bpfmaniov1alpha1.ProgTypeXDP:
				interfaces, ifErr := getInterfaces(&p.XDP.InterfaceSelector, r.ourNode)
				if ifErr != nil {
					ctxLogger.Error(ifErr, "failed to get interfaces for XDP Program",
						"app program name", a.Name, "program index", j)
					continue
				}
				appProgramId := fmt.Sprintf("%s-%s", strings.ToLower(string(p.Type)), interfaces[0])
				xdpProgram := bpfmaniov1alpha1.XdpProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:   buildProgramName(a, p),
						Labels: map[string]string{internal.AppProgramId: appProgramId}},
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
				appProgramMap[appProgramId] = true
				// Reconcile XdpProgram.
				complete, res, err = r.reconcileCommon(ctx, rec, xdpObjects)

			default:
				ctxLogger.Error(fmt.Errorf("unsupported bpf program type"), "unsupported bpf program type", "ProgType", p.Type)
				// Skip this program and continue to the next one
				continue
			}

			ctxLogger.V(1).Info("Reconcile Application", "Application", i, "Program", j, "Name", a.Name,
				"type", p.Type, "Complete", complete, "Result", res, "Error", err)

			if complete {
				// We've completed reconciling this program, continue to the next one
				continue
			} else {
				return res, err
			}
		}

		if complete {
			bpfPrograms := &bpfmaniov1alpha1.BpfProgramList{}
			bpfDeletedPrograms := &bpfmaniov1alpha1.BpfProgramList{}
			// find programs that need to be deleted and delete them
			opts := []client.ListOption{client.MatchingLabels{internal.BpfProgramOwner: a.Name}}
			if err := r.List(ctx, bpfPrograms, opts...); err != nil {
				ctxLogger.Error(err, "failed to get freshPrograms for full reconcile")
				return ctrl.Result{}, err
			}
			for _, bpfProgram := range bpfPrograms.Items {
				id := bpfProgram.Labels[internal.AppProgramId]
				if _, ok := appProgramMap[id]; !ok {
					ctxLogger.Info("Deleting BpfProgram", "AppProgramId", id, "BpfProgram", bpfProgram.Name)
					bpfDeletedPrograms.Items = append(bpfDeletedPrograms.Items, bpfProgram)
				}
			}
			// Delete BpfPrograms that are no longer needed
			res, err = r.unLoadAndDeleteBpfProgramsList(ctx, bpfDeletedPrograms, internal.BpfApplicationControllerFinalizer)
			if err != nil {
				ctxLogger.Error(err, "failed to delete programs")
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
