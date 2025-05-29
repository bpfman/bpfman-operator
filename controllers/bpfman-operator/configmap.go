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
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	osv1 "github.com/openshift/api/security/v1"

	"github.com/bpfman/bpfman-operator/internal"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=configmaps/finalizers,verbs=update

type BpfmanConfigReconciler struct {
	ClusterApplicationReconciler
	BpfmanStandardDeployment string
	CsiDriverDeployment      string
	RestrictedSCC            string
	IsOpenshift              bool
}

// retry is the standard requeue result with a delay.
var retry = ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}

// SetupWithManager sets up watches for the bpfman-config ConfigMap
// and for the bpfman-daemon DaemonSet. Any DS event for the named
// DaemonSet will enqueue a reconcile for the bpfman-config.
func (r *BpfmanConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Define mapping function for DaemonSet events to ConfigMap
	// reconcile requests.
	mapDaemonSetToConfigMap := func(ctx context.Context, obj client.Object) []reconcile.Request {
		if obj.GetName() != internal.BpfmanDsName {
			return nil
		}
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      internal.BpfmanConfigName,
			},
		}}
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch only the single bpfman-config ConfigMap.
		For(&corev1.ConfigMap{}, builder.WithPredicates(bpfmanConfigPredicate())).
		// Watch the DaemonSet (create/update/delete) and map
		// back to the ConfigMap.
		Watches(
			&appsv1.DaemonSet{},
			handler.EnqueueRequestsFromMapFunc(mapDaemonSetToConfigMap),
			builder.WithPredicates(bpfmanDaemonPredicate()),
		).
		Complete(r)
}

// Reconcile ensures the bpfman-config ConfigMap and related resources
// are created, updated, or cleaned up.
func (r *BpfmanConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = log.FromContext(ctx).WithName("bpfman-config")

	if req.NamespacedName != (types.NamespacedName{
		Namespace: internal.BpfmanNamespace,
		Name:      internal.BpfmanConfigName,
	}) {
		r.Logger.Info("Ignoring ConfigMap from unexpected namespace", "expected", internal.BpfmanNamespace, "got", req.Namespace, "name", req.Name)
		return ctrl.Result{}, nil
	}

	bpfmanConfig := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, bpfmanConfig); err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap was deleted, nothing to do.
			return ctrl.Result{}, nil
		}
		r.Logger.Error(err, "Failed getting bpfman config", "ReconcileObject", req.NamespacedName)
		return ctrl.Result{}, err
	}

	if !bpfmanConfig.DeletionTimestamp.IsZero() {
		return r.handleTeardown(ctx, bpfmanConfig)
	}

	if err := r.ensureFinalizer(ctx, bpfmanConfig); err != nil {
		r.Logger.Error(err, "failed to add finalizer to ConfigMap")
		return retry, err
	}

	if err := r.ensureCSIDriver(ctx); err != nil {
		r.Logger.Error(err, "failed to ensure Bpfman CSIDriver")
		return retry, err
	}

	if r.IsOpenshift {
		if err := r.ensureRestrictedSCC(ctx); err != nil {
			r.Logger.Error(err, "failed to ensure Bpfman restricted SCC")
			return ctrl.Result{}, err
		}
	}

	result, err := r.reconcileDaemonSet(ctx, bpfmanConfig)
	if err != nil {
		r.Logger.Error(err, "failed to reconcile Bpfman DaemonSet")
	}
	return result, err
}

// Only reconcile on bpfman-daemon Daemonset events.
func bpfmanDaemonPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == internal.BpfmanDsName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == internal.BpfmanDsName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == internal.BpfmanDsName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == internal.BpfmanDsName
		},
	}
}

// Only reconcile on bpfman-config configmap events.
func bpfmanConfigPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == internal.BpfmanConfigName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == internal.BpfmanConfigName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == internal.BpfmanConfigName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == internal.BpfmanConfigName
		},
	}
}

// LoadRestrictedSecurityContext loads the bpfman-restricted SCC from disk which
// users can bind to in order to utilize bpfman in an unprivileged way.
func LoadRestrictedSecurityContext(path string) *osv1.SecurityContextConstraints {
	// Load static SCC yaml from disk
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	bpfmanRestrictedSCC := &osv1.SecurityContextConstraints{}
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, _ := decode(b, nil, bpfmanRestrictedSCC)

	return obj.(*osv1.SecurityContextConstraints)
}

func LoadCsiDriver(path string) *storagev1.CSIDriver {
	// Load static CSIDriver yaml from disk
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, _ := decode(b, nil, nil)

	return obj.(*storagev1.CSIDriver)
}

func LoadAndConfigureBpfmanDs(config *corev1.ConfigMap, path string, isOpenshift bool) *appsv1.DaemonSet {
	// Load static bpfman deployment from disk
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}

	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, _ := decode(b, nil, nil)

	staticBpfmanDeployment := obj.(*appsv1.DaemonSet)

	// Runtime Configurable fields
	bpfmanImage := config.Data["bpfman.image"]
	bpfmanAgentImage := config.Data["bpfman.agent.image"]
	bpfmanLogLevel := config.Data["bpfman.log.level"]
	bpfmanAgentLogLevel := config.Data["bpfman.agent.log.level"]
	bpfmanHealthProbeAddr := config.Data["bpfman.agent.healthprobe.addr"]
	bpfmanMetricAddr := config.Data["bpfman.agent.metric.addr"]
	bpfmanConfigs := config.Data["bpfman.toml"]

	// Annotate the log level on the ds so we get automatic restarts on changes.
	if staticBpfmanDeployment.Spec.Template.Annotations == nil {
		staticBpfmanDeployment.Spec.Template.Annotations = make(map[string]string)
	}

	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.loglevel"] = bpfmanLogLevel
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.agent.loglevel"] = bpfmanAgentLogLevel
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.agent.healthprobeaddr"] = bpfmanHealthProbeAddr
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.agent.metricaddr"] = bpfmanMetricAddr
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.toml"] = bpfmanConfigs
	staticBpfmanDeployment.Name = internal.BpfmanDsName
	staticBpfmanDeployment.Namespace = config.Namespace
	staticBpfmanDeployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)
	for cindex, container := range staticBpfmanDeployment.Spec.Template.Spec.Containers {
		if container.Name == internal.BpfmanContainerName {
			staticBpfmanDeployment.Spec.Template.Spec.Containers[cindex].Image = bpfmanImage
		} else if container.Name == internal.BpfmanAgentContainerName {
			staticBpfmanDeployment.Spec.Template.Spec.Containers[cindex].Image = bpfmanAgentImage

			for aindex, arg := range container.Args {
				if bpfmanHealthProbeAddr != "" {
					if strings.Contains(arg, "health-probe-bind-address") {
						staticBpfmanDeployment.Spec.Template.Spec.Containers[cindex].Args[aindex] = "--health-probe-bind-address=" + bpfmanHealthProbeAddr
					}
				}
				if bpfmanMetricAddr != "" {
					if strings.Contains(arg, "metrics-bind-address") {
						staticBpfmanDeployment.Spec.Template.Spec.Containers[cindex].Args[aindex] = "--metrics-bind-address=" + bpfmanMetricAddr
					}
				}
			}
		}
	}

	return staticBpfmanDeployment
}

// handleTeardown ensures that all resources created by the presence
// of the bpfman-config ConfigMap are deleted when the ConfigMap is
// marked for deletion (i.e., DeletionTimestamp is set).
//
// It performs the following steps:
//
//  1. Logs which managed resources (DaemonSet, CSIDriver, SCC) still
//     exist.
//  2. Attempts to delete any that remain, returning `retry` if any
//     are still present or if any deletion attempt fails.
//  3. If all resources have been deleted, removes the operator's
//     finalizer from the ConfigMap.
//
// The operator uses a finalizer to block Kubernetes from deleting the
// ConfigMap until cleanup is complete. The Reconcile loop continues
// to call handleTeardown as long as:
//
//   - The ConfigMap exists, and
//   - The DeletionTimestamp is set, and
//   - The finalizer is still present.
//
// Once all resources are gone and the finalizer is successfully
// removed, Kubernetes deletes the ConfigMap. On the next reconcile,
// `Get(...)` will return IsNotFound, and the controller will stop
// calling `handleTeardown`.
//
// This pattern ensures that teardown is:
//   - Explicit
//   - Idempotent (safe to run repeatedly)
//   - Driven by observed state
//   - Finalises cleanly without race conditions from async deletes
//
// Any failed or pending delete operation causes the controller to
// requeue, giving time for propagation to complete or transient
// failures to resolve.
func (r *BpfmanConfigReconciler) handleTeardown(ctx context.Context, bpfmanConfig *corev1.ConfigMap) (ctrl.Result, error) {
	var toDelete []string

	dsKey := types.NamespacedName{Namespace: bpfmanConfig.Namespace, Name: internal.BpfmanDsName}
	if err := r.Get(ctx, dsKey, &appsv1.DaemonSet{}); err == nil {
		toDelete = append(toDelete, "DaemonSet")
	}

	csiKey := types.NamespacedName{Name: internal.BpfmanCsiDriverName}
	if err := r.Get(ctx, csiKey, &storagev1.CSIDriver{}); err == nil {
		toDelete = append(toDelete, "CSIDriver")
	}

	if r.IsOpenshift {
		sccKey := types.NamespacedName{Name: internal.BpfmanRestrictedSccName}
		if err := r.Get(ctx, sccKey, &osv1.SecurityContextConstraints{}); err == nil {
			toDelete = append(toDelete, "RestrictedSCC")
		}
	}

	if len(toDelete) > 0 {
		r.Logger.Info("Tearing down resources managed by ConfigMap", "resourcesRemaining", toDelete)

		if result, err := r.tryDeleteDaemonSet(ctx, bpfmanConfig.Namespace); result.Requeue || err != nil {
			if err != nil {
				r.Logger.Error(err, "failed to delete Bpfman DaemonSet")
			}
			return result, err
		}

		if result, err := r.tryDeleteCSIDriver(ctx); result.Requeue || err != nil {
			if err != nil {
				r.Logger.Error(err, "failed to delete Bpfman CSIDriver")
			}
			return result, err
		}

		if r.IsOpenshift {
			if result, err := r.tryDeleteSCC(ctx); result.Requeue || err != nil {
				if err != nil {
					r.Logger.Error(err, "failed to delete Bpfman SCC")
				}
				return result, err
			}
		}
	}

	result, err := r.removeFinalizer(ctx, bpfmanConfig)
	if err != nil {
		r.Logger.Error(err, "failed to remove finalizer from ConfigMap")
	}
	return result, err
}

// Try to delete the DaemonSet, return a requeue result if it exists
// or an error.
func (r *BpfmanConfigReconciler) tryDeleteDaemonSet(ctx context.Context, namespace string) (ctrl.Result, error) {
	key := types.NamespacedName{Namespace: namespace, Name: internal.BpfmanDsName}
	daemonSet := &appsv1.DaemonSet{}

	switch err := r.Get(ctx, key, daemonSet); {
	case errors.IsNotFound(err):
		// Already deleted
		return ctrl.Result{}, nil
	case err != nil:
		return retry, fmt.Errorf("getting DaemonSet %q in namespace %q: %w", key.Name, key.Namespace, err)
	}

	if err := r.Delete(ctx, daemonSet); err != nil {
		return retry, fmt.Errorf("deleting DaemonSet %q in namespace %q: %w", key.Name, key.Namespace, err)
	}

	return retry, nil
}

// Ensure the finalizer is added to the ConfigMap.
func (r *BpfmanConfigReconciler) ensureFinalizer(ctx context.Context, bpfmanConfig *corev1.ConfigMap) error {
	if updated := controllerutil.AddFinalizer(bpfmanConfig, internal.BpfmanOperatorFinalizer); updated {
		if err := r.Update(ctx, bpfmanConfig); err != nil {
			return fmt.Errorf("adding finalizer to ConfigMap %q/%q: %w", bpfmanConfig.Namespace, bpfmanConfig.Name, err)
		}
	}
	return nil
}

// Ensure CSIDriver exists, create if necessary.
func (r *BpfmanConfigReconciler) ensureCSIDriver(ctx context.Context) error {
	key := types.NamespacedName{
		Namespace: corev1.NamespaceAll,
		Name:      internal.BpfmanCsiDriverName,
	}
	csiDriver := &storagev1.CSIDriver{}

	switch err := r.Get(ctx, key, csiDriver); {
	case errors.IsNotFound(err):
		*csiDriver = *LoadCsiDriver(r.CsiDriverDeployment)
		if err := r.Create(ctx, csiDriver); err != nil {
			return fmt.Errorf("creating Bpfman CSIDriver %q: %w", key.Name, err)
		}
	case err != nil:
		return fmt.Errorf("getting Bpfman CSIDriver %q: %w", key.Name, err)
	}

	return nil
}

// Ensure the restricted SCC exists (for OpenShift).
func (r *BpfmanConfigReconciler) ensureRestrictedSCC(ctx context.Context) error {
	key := types.NamespacedName{
		Namespace: corev1.NamespaceAll,
		Name:      internal.BpfmanRestrictedSccName,
	}
	scc := &osv1.SecurityContextConstraints{}

	switch err := r.Get(ctx, key, scc); {
	case errors.IsNotFound(err):
		*scc = *LoadRestrictedSecurityContext(r.RestrictedSCC)
		if err := r.Create(ctx, scc); err != nil {
			return fmt.Errorf("creating restricted SCC %q: %w", key.Name, err)
		}
	case err != nil:
		return fmt.Errorf("getting restricted SCC %q: %w", key.Name, err)
	}

	return nil
}

// Ensure the restricted SCC exists (for OpenShift).
func (r *BpfmanConfigReconciler) reconcileDaemonSet(ctx context.Context, bpfmanConfig *corev1.ConfigMap) (ctrl.Result, error) {
	key := types.NamespacedName{
		Namespace: bpfmanConfig.Namespace,
		Name:      internal.BpfmanDsName,
	}

	currentDS := &appsv1.DaemonSet{}
	staticDaemonSet := LoadAndConfigureBpfmanDs(bpfmanConfig, r.BpfmanStandardDeployment, r.IsOpenshift)

	switch err := r.Get(ctx, key, currentDS); {
	case errors.IsNotFound(err):
		// Check if there's a terminating DaemonSet.
		terminating, terminatingDS, err := r.anyTerminatingDaemonSets(ctx, key.Namespace, key.Name)
		if err != nil {
			r.Logger.Error(err, "Failed to check for terminating DaemonSets")
			return ctrl.Result{}, err
		}
		if terminating {
			r.Logger.Info("Found terminating DaemonSet, requeuing", "deletionTimestamp", terminatingDS.DeletionTimestamp, "resourceVersion", terminatingDS.ResourceVersion, "uid", terminatingDS.UID)
			return retry, nil
		}

		if err := r.Create(ctx, staticDaemonSet); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating Bpfman DaemonSet %q/%q: %w", key.Namespace, key.Name, err)
		}

		return ctrl.Result{}, nil
	case err != nil:
		r.Logger.Error(err, "Failed to get DaemonSet during reconciliation")
		return ctrl.Result{}, fmt.Errorf("getting Bpfman DaemonSet %q/%q: %w", key.Namespace, key.Name, err)
	}

	r.Logger.Info("Found existing DaemonSet", "resourceVersion", currentDS.ResourceVersion, "uid", currentDS.UID, "generation", currentDS.Generation)

	return r.updateDaemonSetIfNeeded(ctx, currentDS, staticDaemonSet)
}

// Try to delete the CSIDriver, return a requeue result if it exists
// or an error.
func (r *BpfmanConfigReconciler) tryDeleteCSIDriver(ctx context.Context) (ctrl.Result, error) {
	key := types.NamespacedName{Name: internal.BpfmanCsiDriverName}
	csiDriver := &storagev1.CSIDriver{}

	switch err := r.Get(ctx, key, csiDriver); {
	case errors.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		return retry, fmt.Errorf("getting CSIDriver %q: %w", key.Name, err)
	}

	if err := r.Delete(ctx, csiDriver); err != nil {
		return retry, fmt.Errorf("deleting CSIDriver %q: %w", key.Name, err)
	}

	return retry, nil
}

// Try to delete the restricted SCC, return a requeue result if it
// exists or an error.
func (r *BpfmanConfigReconciler) tryDeleteSCC(ctx context.Context) (ctrl.Result, error) {
	key := types.NamespacedName{Name: internal.BpfmanRestrictedSccName}
	scc := &osv1.SecurityContextConstraints{}

	switch err := r.Get(ctx, key, scc); {
	case errors.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		return retry, fmt.Errorf("getting SCC %q: %w", key.Name, err)
	}

	if err := r.Delete(ctx, scc); err != nil {
		return retry, fmt.Errorf("deleting SCC %q: %w", key.Name, err)
	}

	return retry, nil
}

// Remove the finalizer from the ConfigMap if present.
func (r *BpfmanConfigReconciler) removeFinalizer(ctx context.Context, bpfmanConfig *corev1.ConfigMap) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(bpfmanConfig, internal.BpfmanOperatorFinalizer) {
		controllerutil.RemoveFinalizer(bpfmanConfig, internal.BpfmanOperatorFinalizer)
		if err := r.Update(ctx, bpfmanConfig); err != nil {
			return retry, fmt.Errorf("removing finalizer from ConfigMap %q/%q: %w", bpfmanConfig.Namespace, bpfmanConfig.Name, err)
		}
	}

	return ctrl.Result{}, nil
}

// Update DaemonSet if its spec has changed.
func (r *BpfmanConfigReconciler) updateDaemonSetIfNeeded(ctx context.Context, bpfmanDeployment, staticBpfmanDeployment *appsv1.DaemonSet) (ctrl.Result, error) {
	if !reflect.DeepEqual(staticBpfmanDeployment.Spec, bpfmanDeployment.Spec) {
		r.Logger.Info("Updating Bpfman DaemonSet due to spec change")
		if err := r.Update(ctx, staticBpfmanDeployment); err != nil {
			return retry, fmt.Errorf("reconciling Bpfman DaemonSet: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// anyTerminatingDaemonSets checks if there are any terminating
// DaemonSets with the given name. Returns whether a terminating
// DaemonSet was found, info about it if found, and any error.
func (r *BpfmanConfigReconciler) anyTerminatingDaemonSets(ctx context.Context, namespace, name string) (bool, *appsv1.DaemonSet, error) {
	dsList := &appsv1.DaemonSetList{}
	if err := r.List(ctx, dsList, client.InNamespace(namespace)); err != nil {
		return false, nil, fmt.Errorf("listing DaemonSets: %w", err)
	}

	for _, ds := range dsList.Items {
		if ds.Name == name && !ds.DeletionTimestamp.IsZero() {
			return true, ds.DeepCopy(), nil
		}
	}

	return false, nil, nil
}
