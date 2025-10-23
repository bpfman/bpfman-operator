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
	"maps"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	osv1 "github.com/openshift/api/security/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=configs,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bpfman.io,resources=configs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

type BpfmanConfigReconciler struct {
	ClusterApplicationReconciler
	BpfmanStandardDS     string
	BpfmanMetricsProxyDS string
	CsiDriverDS          string
	RestrictedSCC        string
	IsOpenshift          bool
	Recorder             record.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfmanConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	setup := ctrl.NewControllerManagedBy(mgr).
		// Watch the bpfman-daemon configmap to configure the bpfman deployment across the whole cluster
		For(&v1alpha1.Config{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanConfigName))).
		Owns(&corev1.ConfigMap{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanCmName))).
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanDsName))).
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanMetricsProxyDsName))).
		Owns(
			&storagev1.CSIDriver{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanCsiDriverName)))

	if r.IsOpenshift {
		setup = setup.Owns(
			&osv1.SecurityContextConstraints{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanRestrictedSccName)))
	}

	return setup.Complete(r)
}

func (r *BpfmanConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("Config")
	r.Logger.Info("Running the reconciler")

	bpfmanConfig := &v1alpha1.Config{}
	// We must use types.NamespacedName{Name: req.NamespacedName.Name} instead of req.NamespacedName. Otherwise, the
	// mocks in config_test.go fail as they populate the namespace in the reconciler.
	if err := r.Get(ctx, types.NamespacedName{Name: req.NamespacedName.Name}, bpfmanConfig); err != nil {
		// If the resource is not found then it usually means that it was deleted or not created
		// In this way, we will stop the reconciliation
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Logger.Error(err, "Failed to get bpfman config", "ReconcileObject", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// If we haven't added any conditions, yet, set them to unknown.
	if len(bpfmanConfig.Status.Conditions) == 0 {
		r.Logger.Info("Adding initial status conditions", "name", bpfmanConfig.Name)
		if err := r.setStatusConditions(ctx, bpfmanConfig); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Check if Config is being deleted first to prevent race
	// conditions.
	if !bpfmanConfig.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, bpfmanConfig)
	}

	// Ensure finalizer exists to prevent race conditions during
	// deletion.
	if !controllerutil.ContainsFinalizer(bpfmanConfig, internal.BpfmanConfigFinalizer) {
		r.Logger.Info("Adding finalizer to Config", "name", bpfmanConfig.Name)
		controllerutil.AddFinalizer(bpfmanConfig, internal.BpfmanConfigFinalizer)
		if err := r.Update(ctx, bpfmanConfig); err != nil {
			r.Logger.Error(err, "Failed to add finalizer to Config")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.setStatusConditions(ctx, bpfmanConfig)
	}

	// Normal reconciliation - safe to create/update resources.
	if err := r.reconcileCM(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileCSIDriver(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileSCC(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileStandardDS(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileMetricsProxyDS(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.setStatusConditions(ctx, bpfmanConfig)
}

func (r *BpfmanConfigReconciler) reconcileCM(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      internal.BpfmanCmName,
		Namespace: bpfmanConfig.Spec.Namespace},
		Data: map[string]string{
			internal.BpfmanTOML:          bpfmanConfig.Spec.Configuration,
			internal.BpfmanAgentLogLevel: bpfmanConfig.Spec.Agent.LogLevel,
			internal.BpfmanLogLevel:      bpfmanConfig.Spec.LogLevel,
		},
	}
	return assureResource(ctx, r, bpfmanConfig, cm, func(existing, desired *corev1.ConfigMap) bool {
		return !equality.Semantic.DeepEqual(existing.Data, desired.Data)
	})
}

func (r *BpfmanConfigReconciler) reconcileCSIDriver(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	csiDriver := &storagev1.CSIDriver{ObjectMeta: metav1.ObjectMeta{Name: internal.BpfmanCsiDriverName}}
	r.Logger.Info("Loading object", "object", csiDriver.Name, "path", r.CsiDriverDS)
	csiDriver, err := load(csiDriver, r.CsiDriverDS, csiDriver.Name)
	if err != nil {
		return err
	}
	return assureResource(ctx, r, bpfmanConfig, csiDriver, func(existing, desired *storagev1.CSIDriver) bool {
		return !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)
	})
}

func (r *BpfmanConfigReconciler) reconcileSCC(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	if r.IsOpenshift {
		bpfmanRestrictedSCC := &osv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
		}
		r.Logger.Info("Loading object", "object", bpfmanRestrictedSCC.Name, "path", r.RestrictedSCC)
		bpfmanRestrictedSCC, err := load(bpfmanRestrictedSCC, r.RestrictedSCC, bpfmanRestrictedSCC.Name)
		if err != nil {
			return err
		}
		return assureResource(ctx, r, bpfmanConfig, bpfmanRestrictedSCC, func(existing, desired *osv1.SecurityContextConstraints) bool {
			existingCopy := existing.DeepCopy()
			desiredCopy := desired.DeepCopy()
			existingCopy.ObjectMeta = metav1.ObjectMeta{}
			desiredCopy.ObjectMeta = metav1.ObjectMeta{}
			return !equality.Semantic.DeepEqual(existingCopy, desiredCopy)
		})
	}
	return nil
}

func (r *BpfmanConfigReconciler) reconcileStandardDS(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	bpfmanDS := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: internal.BpfmanDsName}}
	r.Logger.Info("Loading object", "object", bpfmanDS.Name, "path", r.BpfmanStandardDS)
	bpfmanDS, err := load(bpfmanDS, r.BpfmanStandardDS, bpfmanDS.Name)
	if err != nil {
		return err
	}
	configureBpfmanDs(bpfmanDS, bpfmanConfig)
	return assureResource(ctx, r, bpfmanConfig, bpfmanDS, func(existing, desired *appsv1.DaemonSet) bool {
		return !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)
	})
}

func (r *BpfmanConfigReconciler) reconcileMetricsProxyDS(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	// Reconcile metrics-proxy daemonset
	metricsProxyDS := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: internal.BpfmanMetricsProxyDsName}}
	r.Logger.Info("Loading object", "object", metricsProxyDS.Name, "path", r.BpfmanMetricsProxyDS)
	metricsProxyDS, err := load(metricsProxyDS, r.BpfmanMetricsProxyDS, metricsProxyDS.Name)
	if err != nil {
		return err
	}
	configureMetricsProxyDs(metricsProxyDS, bpfmanConfig, r.IsOpenshift)
	return assureResource(ctx, r, bpfmanConfig, metricsProxyDS, func(existing, desired *appsv1.DaemonSet) bool {
		return !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)
	})
}

// setStatusConditions checks the status of all Config components (ConfigMap, DaemonSets, CSIDriver, SCC)
// and updates the Config's status.componentStatuses and status.conditions accordingly.
// It emits Kubernetes events for status changes. After updating the status subresource, it re-fetches
// the Config object to ensure the in-memory representation reflects the latest server state, including
// any changes made by the API server (e.g., resource version updates).
func (r *BpfmanConfigReconciler) setStatusConditions(ctx context.Context, config *v1alpha1.Config) error {
	if config == nil {
		return fmt.Errorf("object Config config is nil")
	}
	if r.Recorder == nil {
		return fmt.Errorf("object Recorder is nil")
	}

	// Check each resource and set appropriate status.
	statuses := make(map[string]v1alpha1.ConfigComponentStatus)
	var err error
	var status *v1alpha1.ConfigComponentStatus

	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: internal.BpfmanCmName, Namespace: config.Spec.Namespace}
	if status, err = r.checkResourceStatus(ctx, cm, key, nil); err != nil {
		return err
	}
	if status != nil {
		statuses["ConfigMap"] = *status
	}

	ds := &appsv1.DaemonSet{}
	key = types.NamespacedName{Name: internal.BpfmanDsName, Namespace: config.Spec.Namespace}
	if status, err = r.checkResourceStatus(ctx, ds, key, isDaemonSetReady); err != nil {
		return err
	}
	if status != nil {
		statuses["DaemonSet"] = *status
	}

	metricsDS := &appsv1.DaemonSet{}
	key = types.NamespacedName{Name: internal.BpfmanMetricsProxyDsName, Namespace: config.Spec.Namespace}
	if status, err = r.checkResourceStatus(ctx, metricsDS, key, isDaemonSetReady); err != nil {
		return err
	}
	if status != nil {
		statuses["MetricsProxyDaemonSet"] = *status
	}

	csiDriver := &storagev1.CSIDriver{}
	key = types.NamespacedName{Name: internal.BpfmanCsiDriverName}
	if status, err = r.checkResourceStatus(ctx, csiDriver, key, nil); err != nil {
		return err
	}
	if status != nil {
		statuses["CsiDriver"] = *status
	}

	if r.IsOpenshift {
		scc := &osv1.SecurityContextConstraints{}
		key = types.NamespacedName{Name: internal.BpfmanRestrictedSccName}
		if status, err = r.checkResourceStatus(ctx, scc, key, nil); err != nil {
			return err
		}
		if status != nil {
			statuses["Scc"] = *status
		}
	}

	// If none of the components changed, do not update anything for the status.
	if config.Status.Components != nil && internal.CCSEquals(statuses, config.Status.Components) {
		return nil
	}

	// Set component statuses, first.
	config.Status.Components = statuses

	// Set conditions, next.
	switch {
	case internal.CCSAnyComponentProgressing(statuses, r.IsOpenshift):
		meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
			Type:    internal.ConfigConditionProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  internal.ConfigReasonProgressing,
			Message: internal.ConfigMessageProgressing,
		})
		meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
			Type:    internal.ConfigConditionAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  internal.ConfigReasonProgressing,
			Message: internal.ConfigMessageProgressing,
		})
		r.Recorder.Event(config, "Normal", internal.ConfigReasonProgressing, internal.ConfigMessageProgressing)
	case internal.CCSAllComponentsReady(statuses, r.IsOpenshift):
		meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
			Type:    internal.ConfigConditionProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  internal.ConfigReasonAvailable,
			Message: internal.ConfigMessageAvailable,
		})
		meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
			Type:    internal.ConfigConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  internal.ConfigReasonAvailable,
			Message: internal.ConfigMessageAvailable,
		})
		r.Recorder.Event(config, "Normal", internal.ConfigReasonAvailable, internal.ConfigMessageAvailable)
	default:
		meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
			Type:    internal.ConfigConditionProgressing,
			Status:  metav1.ConditionUnknown,
			Reason:  internal.ConfigReasonUnknown,
			Message: internal.ConfigMessageUnknown,
		})
		meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
			Type:    internal.ConfigConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  internal.ConfigReasonUnknown,
			Message: internal.ConfigMessageUnknown,
		})
		r.Recorder.Event(config, "Normal", internal.ConfigReasonUnknown, internal.ConfigMessageUnknown)
	}

	// Update the status.
	if err := r.Status().Update(ctx, config); err != nil {
		return fmt.Errorf("cannot update status for config %q, err: %w", config.Name, err)
	}

	// Update the object again (config is a pointer).
	return r.Get(ctx, types.NamespacedName{Name: config.Name}, config)
}

// checkResourceStatus is a helper for setStatusConditions. It checks whether a given object (= resource) can be found
// and if it's ready. It'll return a status (Unknown, Progressing, Ready) for the object or an error.
func (r *BpfmanConfigReconciler) checkResourceStatus(ctx context.Context, obj client.Object, key types.NamespacedName,
	readyCheck func(client.Object) bool) (*v1alpha1.ConfigComponentStatus, error) {
	if err := r.Get(ctx, key, obj); err != nil {
		if errors.IsNotFound(err) {
			return ptr.To(v1alpha1.ConfigStatusUnknown), nil
		} else {
			return nil, err
		}
	}
	if readyCheck == nil || readyCheck(obj) {
		return ptr.To(v1alpha1.ConfigStatusReady), nil
	}
	return ptr.To(v1alpha1.ConfigStatusProgressing), nil
}

// isDaemonSetReady returns true if we consider the DaemonSet to be ready.
// We consider a DaemonSet to be ready when:
// a) the DesiredNumberScheduled is > 0. For example, if a DaemonSet's node selector matches 0 nodes, we consider that
// it isn't ready.
// b) We check UpdatedNumberScheduled and NumberAvailable, the same as in
// https://github.com/kubernetes/kubernetes/blob/ad82c3d39f5e9f21e173ffeb8aa57953a0da4601/staging/src/k8s.io/kubectl/pkg/polymorphichelpers/rollout_status.go#L95
// c) In addition, we check that NumberReady matches DesiredNumberScheduled.
func isDaemonSetReady(obj client.Object) bool {
	daemon := obj.(*appsv1.DaemonSet)
	if daemon.Status.DesiredNumberScheduled <= 0 {
		return false
	}
	if daemon.Status.UpdatedNumberScheduled < daemon.Status.DesiredNumberScheduled {
		return false
	}
	if daemon.Status.NumberAvailable < daemon.Status.DesiredNumberScheduled {
		return false
	}
	if daemon.Status.NumberReady < daemon.Status.DesiredNumberScheduled {
		return false
	}
	return true
}

// resourcePredicate creates a predicate that filters events based on resource name.
// Only processes events for resources matching the specified resourceName.
func resourcePredicate(resourceName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == resourceName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == resourceName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == resourceName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == resourceName
		},
	}
}

// configureBpfmanDs configures the bpfman DaemonSet with runtime-configurable values from the Config.
// Updates container images, log levels, health probe addresses.
func configureBpfmanDs(staticBpfmanDS *appsv1.DaemonSet, config *v1alpha1.Config) {
	// Runtime Configurable fields
	bpfmanHealthProbeAddr := healthProbeAddress(config.Spec.Agent.HealthProbePort)

	newAnnotations := map[string]string{
		fmt.Sprintf("%s.%s", internal.APIPrefix, internal.BpfmanLogLevel):      config.Spec.LogLevel,
		fmt.Sprintf("%s.%s", internal.APIPrefix, internal.BpfmanAgentLogLevel): config.Spec.Agent.LogLevel,
		fmt.Sprintf("%s.%s", internal.APIPrefix, internal.BpfmanTOML):          config.Spec.Configuration,
	}

	// Annotate configuration values on the DaemonSet to trigger automatic pod restarts on changes
	if staticBpfmanDS.Spec.Template.ObjectMeta.Annotations == nil {
		staticBpfmanDS.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}

	maps.Copy(staticBpfmanDS.Spec.Template.ObjectMeta.Annotations, newAnnotations)

	staticBpfmanDS.Name = internal.BpfmanDsName
	staticBpfmanDS.Namespace = config.Spec.Namespace
	staticBpfmanDS.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)
	for cindex, container := range staticBpfmanDS.Spec.Template.Spec.Containers {
		switch container.Name {
		case internal.BpfmanContainerName:
			staticBpfmanDS.Spec.Template.Spec.Containers[cindex].Image = config.Spec.Image
		case internal.BpfmanAgentContainerName:
			staticBpfmanDS.Spec.Template.Spec.Containers[cindex].Image = config.Spec.Agent.Image
			for aindex, arg := range container.Args {
				if bpfmanHealthProbeAddr != "" {
					if strings.Contains(arg, "health-probe-bind-address") {
						staticBpfmanDS.Spec.Template.Spec.Containers[cindex].Args[aindex] =
							"--health-probe-bind-address=" + bpfmanHealthProbeAddr
					}
				}
			}
		default:
			// Do nothing
		}
	}
}

// configureMetricsProxyDs configures the metrics-proxy DaemonSet with runtime values.
// Sets up container images and adds OpenShift-specific TLS configuration when applicable.
func configureMetricsProxyDs(staticMetricsProxyDS *appsv1.DaemonSet, config *v1alpha1.Config, isOpenshift bool) {
	// Runtime Configurable fields
	bpfmanAgentImage := config.Spec.Agent.Image

	// Set the name and namespace from the config
	staticMetricsProxyDS.Name = internal.BpfmanMetricsProxyDsName
	staticMetricsProxyDS.Namespace = config.Spec.Namespace
	staticMetricsProxyDS.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)

	// Configure the metrics-proxy container image
	for cindex, container := range staticMetricsProxyDS.Spec.Template.Spec.Containers {
		if container.Name == internal.BpfmanMetricsProxyContainer {
			staticMetricsProxyDS.Spec.Template.Spec.Containers[cindex].Image = bpfmanAgentImage
		}
	}

	if staticMetricsProxyDS.Spec.Template.ObjectMeta.Annotations == nil {
		staticMetricsProxyDS.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}

	// Add OpenShift-specific TLS configuration
	if isOpenshift {
		// Add serving certificate annotation
		staticMetricsProxyDS.Spec.Template.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = "agent-metrics-tls"

		// Add TLS volume mount to container
		for cindex, container := range staticMetricsProxyDS.Spec.Template.Spec.Containers {
			if container.Name == internal.BpfmanMetricsProxyContainer {
				staticMetricsProxyDS.Spec.Template.Spec.Containers[cindex].VolumeMounts = append(
					staticMetricsProxyDS.Spec.Template.Spec.Containers[cindex].VolumeMounts,
					corev1.VolumeMount{
						Name:      "agent-metrics-tls",
						MountPath: "/tmp/k8s-webhook-server/serving-certs",
						ReadOnly:  true,
					},
				)
			}
		}

		// Add TLS volume
		staticMetricsProxyDS.Spec.Template.Spec.Volumes = append(
			staticMetricsProxyDS.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: "agent-metrics-tls",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  "agent-metrics-tls",
						DefaultMode: ptr.To(int32(420)),
					},
				},
			},
		)
	}
}

// load reads a Kubernetes resource manifest from the specified file path and deserializes it.
// Sets the resource name and returns the loaded object.
func load[T client.Object](t T, path, name string) (T, error) {
	file, err := os.Open(path)
	if err != nil {
		return t, fmt.Errorf("load %s from %s: %w", name, path, err)
	}
	defer file.Close()

	b, err := io.ReadAll(file)
	if err != nil {
		return t, fmt.Errorf("load %s from %s: %w", name, path, err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(b, nil, t)
	if err != nil {
		return t, fmt.Errorf("load %s from %s: %w", name, path, err)
	}

	t = obj.(T)
	t.SetName(name)

	return t, nil
}

// assureResource ensures a Kubernetes resource exists and is up to date.
// Creates the resource if it doesn't exist, otherwise updates it to match the desired state.
func assureResource[T client.Object](ctx context.Context, r *BpfmanConfigReconciler,
	bpfmanConfig *v1alpha1.Config, resource T, needsUpdate func(existing T, desired T) bool) error {
	if err := ctrl.SetControllerReference(bpfmanConfig, resource, r.Scheme); err != nil {
		return err
	}

	objectKey := types.NamespacedName{Namespace: resource.GetNamespace(), Name: resource.GetName()}
	r.Logger.Info("Getting object",
		"type", resource.GetObjectKind(), "namespace", resource.GetNamespace(), "name", resource.GetName())
	existingResource := resource.DeepCopyObject().(T)
	if err := r.Get(ctx, objectKey, existingResource); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("Creating object",
				"type", resource.GetObjectKind(), "namespace", resource.GetNamespace(), "name", resource.GetName())
			if err := r.Create(ctx, resource); err != nil {
				r.Logger.Error(err, "Failed to create object",
					"type", resource.GetObjectKind(), "namespace", resource.GetNamespace(), "name", resource.GetName())
				return err
			}
			return nil
		}
		r.Logger.Error(err, "Failed to get object",
			"type", resource.GetObjectKind(), "namespace", resource.GetNamespace(), "name", resource.GetName())
		return err
	}

	if needsUpdate(existingResource, resource) {
		r.Logger.Info("Updating object",
			"type", resource.GetObjectKind(), "namespace", resource.GetNamespace(), "name", resource.GetName())
		resource.SetResourceVersion(existingResource.GetResourceVersion())
		if err := r.Update(ctx, resource); err != nil {
			r.Logger.Error(err, "Failed updating object",
				"type", resource.GetObjectKind(), "namespace", resource.GetNamespace(), "name", resource.GetName())
			return err
		}
	}

	return nil
}

// handleDeletion manages the safe deletion of Config resources by
// removing the finalizer to allow Kubernetes garbage collection to
// proceed via owner references.
func (r *BpfmanConfigReconciler) handleDeletion(ctx context.Context, config *v1alpha1.Config) (ctrl.Result, error) {
	r.Logger.Info("Config deletion requested, allowing Kubernetes garbage collection to proceed",
		"name", config.Name)

	// No custom cleanup needed - all resources are managed via
	// owner references. Remove finalizer to trigger cascading
	// deletion of owned resources.
	controllerutil.RemoveFinalizer(config, internal.BpfmanConfigFinalizer)
	if err := r.Update(ctx, config); err != nil {
		r.Logger.Error(err, "Failed to remove finalizer from Config during deletion", "name", config.Name)
		return ctrl.Result{}, err
	}

	r.Logger.Info("Finalizer removed from Config, deletion will proceed", "name", config.Name)
	return ctrl.Result{}, nil
}

func healthProbeAddress(healthProbePort int32) string {
	if healthProbePort < 0 || healthProbePort > 65535 {
		return ""
	}
	if healthProbePort == 0 {
		healthProbePort = internal.DefaultHealthProbePort
	}
	return fmt.Sprintf(":%d", healthProbePort)
}
