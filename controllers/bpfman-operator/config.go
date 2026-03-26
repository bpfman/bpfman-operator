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
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	osv1 "github.com/openshift/api/security/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=privileged;bpfman-restricted,verbs=use
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods;endpoints;secrets,verbs=get;list;watch
// +kubebuilder:rbac:urls=/metrics;/agent-metrics,verbs=get
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=configs/finalizers,verbs=update

type BpfmanConfigReconciler struct {
	ClusterApplicationReconciler
	BpfmanStandardDS     string
	BpfmanMetricsProxyDS string
	CsiDriverDS          string
	IsOpenshift          bool
	HasMonitoring        bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfmanConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	setup := ctrl.NewControllerManagedBy(mgr).
		// Watch the Config CR to configure the bpfman deployment across the whole cluster
		For(&v1alpha1.Config{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanConfigName))).
		Owns(&corev1.ConfigMap{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanCmName))).
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanDsName))).
		Owns(
			&storagev1.CSIDriver{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanCsiDriverName)))

	if r.IsOpenshift {
		setup = setup.Owns(
			&osv1.SecurityContextConstraints{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanRestrictedSccName))).
			Owns(
				&rbacv1.ClusterRoleBinding{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanPrivilegedSccClusterRoleBindingName))).
			Owns(
				&rbacv1.ClusterRole{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanUserClusterRoleName))).
			Owns(
				&rbacv1.ClusterRoleBinding{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanPrometheusClusterRoleBindingName))).
			Owns(
				&rbacv1.Role{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanPrometheusRoleName))).
			Owns(
				&rbacv1.RoleBinding{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanPrometheusRoleBindingName)))

		if r.HasMonitoring {
			// Watch the operator namespace so we can re-apply the
			// cluster-monitoring label if it is removed.
			setup = setup.Watches(
				&corev1.Namespace{},
				handler.EnqueueRequestsFromMapFunc(
					func(_ context.Context, obj client.Object) []ctrl.Request {
						if obj.GetName() != internal.BpfmanNamespace {
							return nil
						}
						return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: internal.BpfmanConfigName}}}
					}),
				builder.WithPredicates(resourcePredicate(internal.BpfmanNamespace)))
		}
	}

	if r.HasMonitoring {
		setup = setup.Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(resourcePredicate(internal.BpfmanMetricsProxyDsName))).
			Owns(
				&corev1.Service{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanAgentMetricsServiceName))).
			Owns(
				&corev1.Service{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanControllerMetricsServiceName))).
			Owns(
				&monitoringv1.ServiceMonitor{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanAgentServiceMonitorName))).
			Owns(
				&monitoringv1.ServiceMonitor{},
				builder.WithPredicates(resourcePredicate(internal.BpfmanControllerServiceMonitorName)))
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

	if err := r.reconcileMetricsServices(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileServiceMonitors(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileAgentMetricsServiceAnnotation(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileOpenShiftRBAC(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcilePrometheusRBAC(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileNamespace(ctx, bpfmanConfig); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BpfmanConfigReconciler) reconcileCM(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanCmName,
			Namespace: bpfmanConfig.Spec.Namespace,
		},
		Data: map[string]string{
			internal.BpfmanTOML:          bpfmanConfig.Spec.Configuration,
			internal.BpfmanAgentLogLevel: bpfmanConfig.Spec.Agent.LogLevel,
			internal.BpfmanLogLevel:      bpfmanConfig.Spec.Daemon.LogLevel,
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
		allowPrivilegeEscalation := false
		bpfmanRestrictedSCC := &osv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
			AllowHostDirVolumePlugin: false,
			AllowHostIPC:             false,
			AllowHostNetwork:         false,
			AllowHostPID:             false,
			AllowHostPorts:           false,
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			AllowPrivilegedContainer: false,
			RequiredDropCapabilities: []corev1.Capability{"ALL"},
			RunAsUser: osv1.RunAsUserStrategyOptions{
				Type: osv1.RunAsUserStrategyMustRunAsNonRoot,
			},
			SELinuxContext: osv1.SELinuxContextStrategyOptions{
				Type: osv1.SELinuxStrategyRunAsAny,
			},
			SupplementalGroups: osv1.SupplementalGroupsStrategyOptions{
				Type: osv1.SupplementalGroupsStrategyMustRunAs,
			},
			FSGroup: osv1.FSGroupStrategyOptions{
				Type: osv1.FSGroupStrategyRunAsAny,
			},
			Volumes: []osv1.FSType{
				osv1.FSTypeConfigMap,
				osv1.FSTypeCSI,
				osv1.FSTypeDownwardAPI,
				osv1.FSTypeEmptyDir,
				osv1.FSTypeEphemeral,
				osv1.FSTypePersistentVolumeClaim,
				osv1.FSProjected,
				osv1.FSTypeSecret,
			},
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
		copyImagePullPolicy(&existing.Spec.Template.Spec, &desired.Spec.Template.Spec)
		return !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)
	})
}

func (r *BpfmanConfigReconciler) reconcileMetricsProxyDS(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	if !r.HasMonitoring {
		return nil
	}

	// Reconcile metrics-proxy daemonset
	metricsProxyDS := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: internal.BpfmanMetricsProxyDsName}}
	r.Logger.Info("Loading object", "object", metricsProxyDS.Name, "path", r.BpfmanMetricsProxyDS)
	metricsProxyDS, err := load(metricsProxyDS, r.BpfmanMetricsProxyDS, metricsProxyDS.Name)
	if err != nil {
		return err
	}
	configureMetricsProxyDs(metricsProxyDS, bpfmanConfig, r.IsOpenshift)
	return assureResource(ctx, r, bpfmanConfig, metricsProxyDS, func(existing, desired *appsv1.DaemonSet) bool {
		copyImagePullPolicy(&existing.Spec.Template.Spec, &desired.Spec.Template.Spec)
		return !equality.Semantic.DeepEqual(existing.Spec, desired.Spec)
	})
}

// reconcileMetricsServices ensures the metrics Services exist for
// Prometheus to discover scrape targets. The agent metrics service is
// headless so that Prometheus discovers each DaemonSet pod
// individually rather than routing through a single virtual IP.
func (r *BpfmanConfigReconciler) reconcileMetricsServices(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	if !r.HasMonitoring {
		return nil
	}

	ns := bpfmanConfig.Spec.Namespace

	agentSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanAgentMetricsServiceName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "agent-metrics-service",
				"app.kubernetes.io/instance":   "agent-metrics-service",
				"app.kubernetes.io/component":  "metrics",
				"app.kubernetes.io/created-by": "bpfman-operator",
				"app.kubernetes.io/part-of":    "bpfman-operator",
				"app.kubernetes.io/managed-by": "bpfman-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{{
				Name:       "https-metrics",
				Port:       8443,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromString("https-metrics"),
			}},
			Selector: map[string]string{
				"name": "bpfman-metrics-proxy",
			},
		},
	}

	controllerSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanControllerMetricsServiceName,
			Namespace: ns,
			Labels: map[string]string{
				"control-plane":                "controller-manager",
				"app.kubernetes.io/name":       "service",
				"app.kubernetes.io/instance":   "controller-manager-metrics-service",
				"app.kubernetes.io/component":  "metrics",
				"app.kubernetes.io/created-by": "bpfman-operator",
				"app.kubernetes.io/part-of":    "bpfman-operator",
				"app.kubernetes.io/managed-by": "bpfman-operator",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       "https-metrics",
				Port:       8443,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromString("https-metrics"),
			}},
			Selector: map[string]string{
				"control-plane": "controller-manager",
			},
		},
	}

	needsUpdateFn := func(existing, desired *corev1.Service) bool {
		// Preserve immutable fields that the API server sets on
		// creation so that the subsequent Update call does not
		// attempt to clear them.
		desired.Spec.ClusterIP = existing.Spec.ClusterIP
		desired.Spec.ClusterIPs = existing.Spec.ClusterIPs
		return !maps.Equal(existing.Labels, desired.Labels) ||
			!equality.Semantic.DeepEqual(existing.Spec.Ports, desired.Spec.Ports) ||
			!equality.Semantic.DeepEqual(existing.Spec.Selector, desired.Spec.Selector)
	}

	if err := assureResource(ctx, r, bpfmanConfig, agentSvc, needsUpdateFn); err != nil {
		return err
	}
	return assureResource(ctx, r, bpfmanConfig, controllerSvc, needsUpdateFn)
}

func (r *BpfmanConfigReconciler) reconcileServiceMonitors(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	if !r.HasMonitoring {
		return nil
	}

	ns := bpfmanConfig.Spec.Namespace
	httpsScheme := monitoringv1.Scheme("https")

	agentTLS := serviceMonitorTLSConfig(r.IsOpenshift, "bpfman-agent-metrics-service", ns)
	// The operator deployment always uses controller-runtime's
	// self-signed certificates, so the controller ServiceMonitor
	// must skip TLS verification on all platforms.
	controllerTLS := &monitoringv1.TLSConfig{
		SafeTLSConfig: monitoringv1.SafeTLSConfig{
			InsecureSkipVerify: ptr.To(true),
		},
	}

	agentSM := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanAgentServiceMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "metrics",
				"app.kubernetes.io/created-by": "bpfman-operator",
				"app.kubernetes.io/instance":   "agent-metrics-monitor",
				"app.kubernetes.io/managed-by": "bpfman-operator",
				"app.kubernetes.io/name":       "agent-metrics-monitor",
				"app.kubernetes.io/part-of":    "bpfman-operator",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Path:            "/agent-metrics",
					Port:            "https-metrics",
					Scheme:          &httpsScheme,
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					HTTPConfigWithProxyAndTLSFiles: monitoringv1.HTTPConfigWithProxyAndTLSFiles{
						HTTPConfigWithTLSFiles: monitoringv1.HTTPConfigWithTLSFiles{
							TLSConfig: agentTLS,
						},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "metrics",
					"app.kubernetes.io/instance":  "agent-metrics-service",
					"app.kubernetes.io/name":      "agent-metrics-service",
				},
			},
		},
	}

	controllerSM := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanControllerServiceMonitorName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "metrics",
				"app.kubernetes.io/created-by": "bpfman-operator",
				"app.kubernetes.io/instance":   "controller-manager-metrics-monitor",
				"app.kubernetes.io/managed-by": "bpfman-operator",
				"app.kubernetes.io/name":       "servicemonitor",
				"app.kubernetes.io/part-of":    "bpfman-operator",
				"control-plane":                "controller-manager",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Path:            "/metrics",
					Port:            "https-metrics",
					Scheme:          &httpsScheme,
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					HTTPConfigWithProxyAndTLSFiles: monitoringv1.HTTPConfigWithProxyAndTLSFiles{
						HTTPConfigWithTLSFiles: monitoringv1.HTTPConfigWithTLSFiles{
							TLSConfig: controllerTLS,
						},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"control-plane": "controller-manager",
				},
			},
		},
	}

	needsUpdateFn := func(existing, desired *monitoringv1.ServiceMonitor) bool {
		return !equality.Semantic.DeepEqual(existing.Spec.Endpoints, desired.Spec.Endpoints) ||
			!equality.Semantic.DeepEqual(existing.Spec.Selector, desired.Spec.Selector)
	}

	if err := assureResource(ctx, r, bpfmanConfig, agentSM, needsUpdateFn); err != nil {
		return err
	}
	return assureResource(ctx, r, bpfmanConfig, controllerSM, needsUpdateFn)
}

// serviceMonitorTLSConfig returns the TLS configuration for a
// ServiceMonitor endpoint. On OpenShift, the service-ca operator
// provisions serving certificates and a trusted CA bundle, so we use
// strict verification with the CA file and server name. On other
// platforms we skip verification because no standard certificate
// infrastructure exists.
func serviceMonitorTLSConfig(isOpenshift bool, serviceName, namespace string) *monitoringv1.TLSConfig {
	if isOpenshift {
		return &monitoringv1.TLSConfig{
			TLSFilesConfig: monitoringv1.TLSFilesConfig{
				CAFile: "/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt",
			},
			SafeTLSConfig: monitoringv1.SafeTLSConfig{
				ServerName:         ptr.To(serviceName + "." + namespace + ".svc"),
				InsecureSkipVerify: ptr.To(false),
			},
		}
	}
	return &monitoringv1.TLSConfig{
		SafeTLSConfig: monitoringv1.SafeTLSConfig{
			InsecureSkipVerify: ptr.To(true),
		},
	}
}

// reconcileNamespace ensures the operator namespace has the correct
// labels and annotations. Pod security labels are set on all
// platforms since the bpfman-daemon requires privileged containers.
// On OpenShift, additional labels and annotations are set for
// cluster monitoring discovery and workload partitioning.
func (r *BpfmanConfigReconciler) reconcileNamespace(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	ns := bpfmanConfig.Spec.Namespace
	namespace := &corev1.Namespace{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: ns}, namespace); err != nil {
		return fmt.Errorf("get namespace %s: %w", ns, err)
	}

	if namespace.Labels == nil {
		namespace.Labels = make(map[string]string)
	}
	if namespace.Annotations == nil {
		namespace.Annotations = make(map[string]string)
	}

	changed := false

	// Pod security admission labels required on all platforms
	// for the privileged bpfman-daemon containers.
	psaLabels := map[string]string{
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
	}
	for k, v := range psaLabels {
		if namespace.Labels[k] != v {
			namespace.Labels[k] = v
			changed = true
		}
	}

	if r.IsOpenshift {
		if r.HasMonitoring && namespace.Labels["openshift.io/cluster-monitoring"] != "true" {
			namespace.Labels["openshift.io/cluster-monitoring"] = "true"
			changed = true
		}

		osAnnotations := map[string]string{
			"openshift.io/node-selector":    "",
			"openshift.io/description":      "Openshift bpfman components",
			"workload.openshift.io/allowed": "management",
		}
		for k, v := range osAnnotations {
			if namespace.Annotations[k] != v {
				namespace.Annotations[k] = v
				changed = true
			}
		}
	}

	if changed {
		if err := r.Client.Update(ctx, namespace); err != nil {
			return fmt.Errorf("update namespace %s: %w", ns, err)
		}
		r.Logger.Info("Updated namespace labels and annotations", "namespace", ns)
	}

	return nil
}

// reconcileAgentMetricsServiceAnnotation annotates the agent
// metrics service with the OpenShift service-CA serving certificate
// annotation, which triggers the service CA operator to provision
// a TLS secret for the agent metrics proxy.
func (r *BpfmanConfigReconciler) reconcileAgentMetricsServiceAnnotation(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	if !r.IsOpenshift {
		return nil
	}

	ns := bpfmanConfig.Spec.Namespace
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanAgentMetricsServiceName,
			Namespace: ns,
		},
	}
	if isOverridden(svc, bpfmanConfig.Spec.Overrides, r.Scheme) {
		return nil
	}

	if err := r.Client.Get(ctx, types.NamespacedName{
		Name:      internal.BpfmanAgentMetricsServiceName,
		Namespace: ns,
	}, svc); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get agent metrics service: %w", err)
	}

	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}
	if svc.Annotations["service.beta.openshift.io/serving-cert-secret-name"] != "agent-metrics-tls" {
		svc.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = "agent-metrics-tls"
		if err := r.Client.Update(ctx, svc); err != nil {
			return fmt.Errorf("annotate agent metrics service: %w", err)
		}
		r.Logger.Info("Annotated agent metrics service for service-CA cert provisioning")
	}

	return nil
}

// reconcileOpenShiftRBAC ensures the RBAC resources needed for the
// bpfman-daemon service account to use the privileged and
// bpfman-restricted SecurityContextConstraints on OpenShift.
func (r *BpfmanConfigReconciler) reconcileOpenShiftRBAC(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	if !r.IsOpenshift {
		return nil
	}

	ns := bpfmanConfig.Spec.Namespace

	privilegedCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanPrivilegedSccClusterRoleBindingName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:privileged",
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "bpfman-daemon",
			Namespace: ns,
		}},
	}

	if err := assureResource(ctx, r, bpfmanConfig, privilegedCRB, func(existing, desired *rbacv1.ClusterRoleBinding) bool {
		return !equality.Semantic.DeepEqual(existing.RoleRef, desired.RoleRef) ||
			!equality.Semantic.DeepEqual(existing.Subjects, desired.Subjects)
	}); err != nil {
		return err
	}

	userCR := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanUserClusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{"security.openshift.io"},
			ResourceNames: []string{"bpfman-restricted"},
			Resources:     []string{"securitycontextconstraints"},
			Verbs:         []string{"use"},
		}},
	}

	return assureResource(ctx, r, bpfmanConfig, userCR, func(existing, desired *rbacv1.ClusterRole) bool {
		return !equality.Semantic.DeepEqual(existing.Rules, desired.Rules)
	})
}

// reconcilePrometheusRBAC ensures the RBAC resources needed for the
// OpenShift Prometheus instance to scrape bpfman metrics endpoints.
func (r *BpfmanConfigReconciler) reconcilePrometheusRBAC(ctx context.Context, bpfmanConfig *v1alpha1.Config) error {
	if !r.IsOpenshift || !r.HasMonitoring {
		return nil
	}

	ns := bpfmanConfig.Spec.Namespace

	// The prometheus-k8s Role grants Prometheus read access to
	// pods, services, endpoints, configmaps, and secrets in the
	// bpfman namespace.  It was previously deployed via the
	// config/prometheus kustomize overlay but is now managed at
	// runtime so that it only exists when monitoring is available.
	promRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanPrometheusRoleName,
			Namespace: ns,
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"pods", "services", "endpoints", "configmaps", "secrets"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}

	if err := assureResource(ctx, r, bpfmanConfig, promRole, func(existing, desired *rbacv1.Role) bool {
		return !equality.Semantic.DeepEqual(existing.Rules, desired.Rules)
	}); err != nil {
		return err
	}

	promRB := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanPrometheusRoleBindingName,
			Namespace: ns,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     internal.BpfmanPrometheusRoleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "prometheus-k8s",
			Namespace: "openshift-monitoring",
		}},
	}

	if err := assureResource(ctx, r, bpfmanConfig, promRB, func(existing, desired *rbacv1.RoleBinding) bool {
		return !equality.Semantic.DeepEqual(existing.RoleRef, desired.RoleRef) ||
			!equality.Semantic.DeepEqual(existing.Subjects, desired.Subjects)
	}); err != nil {
		return err
	}

	metricsReaderCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanPrometheusClusterRoleBindingName,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "metrics",
				"app.kubernetes.io/created-by": "bpfman-operator",
				"app.kubernetes.io/instance":   "prometheus-metrics-reader",
				"app.kubernetes.io/managed-by": "bpfman-operator",
				"app.kubernetes.io/name":       "clusterrolebinding",
				"app.kubernetes.io/part-of":    "bpfman-operator",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "bpfman-metrics-reader",
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "prometheus-k8s",
			Namespace: "openshift-monitoring",
		}},
	}

	return assureResource(ctx, r, bpfmanConfig, metricsReaderCRB, func(existing, desired *rbacv1.ClusterRoleBinding) bool {
		return !equality.Semantic.DeepEqual(existing.RoleRef, desired.RoleRef) ||
			!equality.Semantic.DeepEqual(existing.Subjects, desired.Subjects)
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

// copyImagePullPolicy copies imagePullPolicy from each container in
// src to the corresponding container in dst (matched by index), but
// only when the dst container has no policy set. This prevents false
// diffs caused by server-side defaulting while still allowing an
// explicitly set policy (e.g., from BPFMAN_IMAGE_PULL_POLICY) to take
// effect on existing resources.
func copyImagePullPolicy(src, dst *corev1.PodSpec) {
	for i := range dst.InitContainers {
		if i < len(src.InitContainers) && dst.InitContainers[i].ImagePullPolicy == "" {
			dst.InitContainers[i].ImagePullPolicy = src.InitContainers[i].ImagePullPolicy
		}
	}
	for i := range dst.Containers {
		if i < len(src.Containers) && dst.Containers[i].ImagePullPolicy == "" {
			dst.Containers[i].ImagePullPolicy = src.Containers[i].ImagePullPolicy
		}
	}
}

// configureBpfmanDs configures the bpfman DaemonSet with runtime-configurable values from the Config.
// Updates container images, log levels, health probe addresses.
func configureBpfmanDs(staticBpfmanDS *appsv1.DaemonSet, config *v1alpha1.Config) {
	// Runtime Configurable fields
	bpfmanHealthProbeAddr := healthProbeAddress(config.Spec.Agent.HealthProbePort)

	newAnnotations := map[string]string{
		fmt.Sprintf("%s.%s", internal.APIPrefix, internal.BpfmanLogLevel):      config.Spec.Daemon.LogLevel,
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

	// Update init container images
	for cindex, container := range staticBpfmanDS.Spec.Template.Spec.InitContainers {
		if container.Name == internal.BpfmanInitContainerName {
			staticBpfmanDS.Spec.Template.Spec.InitContainers[cindex].Image = config.Spec.Agent.Image
		}
	}

	for cindex, container := range staticBpfmanDS.Spec.Template.Spec.Containers {
		switch container.Name {
		case internal.BpfmanContainerName:
			staticBpfmanDS.Spec.Template.Spec.Containers[cindex].Image = config.Spec.Daemon.Image
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
		case internal.BpfmanCsiDriverRegistrarName:
			if config.Spec.Daemon.CsiRegistrarImage != "" {
				staticBpfmanDS.Spec.Template.Spec.Containers[cindex].Image = config.Spec.Daemon.CsiRegistrarImage
			}
		default:
			// Do nothing
		}
	}

	if p := corev1.PullPolicy(os.Getenv("BPFMAN_IMAGE_PULL_POLICY")); p != "" {
		for i := range staticBpfmanDS.Spec.Template.Spec.InitContainers {
			staticBpfmanDS.Spec.Template.Spec.InitContainers[i].ImagePullPolicy = p
		}
		for i := range staticBpfmanDS.Spec.Template.Spec.Containers {
			staticBpfmanDS.Spec.Template.Spec.Containers[i].ImagePullPolicy = p
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

	if p := corev1.PullPolicy(os.Getenv("BPFMAN_IMAGE_PULL_POLICY")); p != "" {
		for i := range staticMetricsProxyDS.Spec.Template.Spec.Containers {
			staticMetricsProxyDS.Spec.Template.Spec.Containers[i].ImagePullPolicy = p
		}
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
	if isOverridden(resource, bpfmanConfig.Spec.Overrides, r.Scheme) {
		// Resolve GVK via the scheme to get the kind reliably
		kind := ""
		gvks, _, err := r.Scheme.ObjectKinds(resource)
		if err == nil && len(gvks) > 0 {
			kind = gvks[0].Kind
		}
		r.Logger.Info("Skipping unmanaged resource (override in place)",
			"kind", kind,
			"namespace", resource.GetNamespace(), "name", resource.GetName())
		return nil
	}

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

	if needsUpdate(existingResource, resource) ||
		!equality.Semantic.DeepEqual(existingResource.GetOwnerReferences(), resource.GetOwnerReferences()) {
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

// isOverridden determines if a given object shall be unmanaged by checking
// against the configured overrides. It uses the scheme to reliably resolve
// the GVK for the resource, rather than relying on the object's TypeMeta
// which may not be populated for in-memory objects.
func isOverridden(resource client.Object, overrides []v1alpha1.ComponentOverride, scheme *runtime.Scheme) bool {
	// Resolve GVK via the scheme - this is reliable even for objects
	// constructed in code that don't have TypeMeta populated.
	gvks, _, err := scheme.ObjectKinds(resource)
	if err != nil || len(gvks) == 0 {
		return false
	}
	resourceGVK := gvks[0]

	for _, override := range overrides {
		if !override.Unmanaged {
			continue
		}

		// Parse the apiVersion from the override (e.g., "apps/v1" or "v1")
		overrideGV, err := schema.ParseGroupVersion(override.APIVersion)
		if err != nil {
			continue
		}

		if override.Kind == resourceGVK.Kind &&
			overrideGV.Group == resourceGVK.Group &&
			override.Namespace == resource.GetNamespace() &&
			override.Name == resource.GetName() {
			return true
		}
	}
	return false
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
