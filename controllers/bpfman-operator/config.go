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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
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
// +kubebuilder:rbac:groups=bpfman.io,resources=configs/finalizers,verbs=update

type BpfmanConfigReconciler struct {
	ClusterApplicationReconciler
	BpfmanStandardDS     string
	BpfmanMetricsProxyDS string
	CsiDriverDS          string
	RestrictedSCC        string
	IsOpenshift          bool
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
	if err := r.Get(ctx, types.NamespacedName{Name: req.NamespacedName.Name}, bpfmanConfig); err != nil {
		// If the resource is not found then it usually means that it was deleted or not created
		// In this way, we will stop the reconciliation
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Logger.Error(err, "Failed to get bpfman config", "ReconcileObject", req.NamespacedName)
		return ctrl.Result{}, err
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
		return ctrl.Result{}, nil
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

	return ctrl.Result{}, nil
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
			return !equality.Semantic.DeepEqual(existing, desired)
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
						SecretName: "agent-metrics-tls",
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
		return t, err
	}
	defer file.Close()

	b, err := io.ReadAll(file)
	if err != nil {
		return t, err
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(b, nil, t)
	if err != nil {
		return t, err
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

func healthProbeAddress(healthProbePort int) string {
	if healthProbePort <= 0 || healthProbePort > 65535 {
		return ""
	}
	return fmt.Sprintf(":%d", healthProbePort)
}
