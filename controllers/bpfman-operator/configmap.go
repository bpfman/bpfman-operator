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
	"io"
	"os"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	osv1 "github.com/openshift/api/security/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/bpfman/bpfman-operator/internal"
)

// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=bpfman.io,resources=configmaps/finalizers,verbs=update

type BpfmanConfigReconciler struct {
	ClusterApplicationReconciler
	BpfmanStandardDeployment     string
	BpfmanMetricsProxyDeployment string
	CsiDriverDeployment          string
	RestrictedSCC                string
	IsOpenshift                  bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *BpfmanConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch the bpfman-daemon configmap to configure the bpfman deployment across the whole cluster
		For(&corev1.ConfigMap{},
			builder.WithPredicates(bpfmanConfigPredicate())).
		// This only watches the bpfman daemonset which is stored on disk and will be created
		// by this operator. We're doing a manual watch since the operator (As a controller)
		// doesn't really want to have an owner-ref since we don't have a CRD for
		// configuring it, only a configmap.
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(bpfmanDaemonPredicate())).
		Owns(
			&appsv1.DaemonSet{},
			builder.WithPredicates(bpfmanMetricsProxyPredicate())).
		Complete(r)
}

func (r *BpfmanConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("configMap")

	bpfmanConfig := &corev1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, bpfmanConfig); err != nil {
		if !errors.IsNotFound(err) {
			r.Logger.Error(err, "failed getting bpfman config", "ReconcileObject", req.NamespacedName)
			return ctrl.Result{}, nil
		}
	} else {
		if updated := controllerutil.AddFinalizer(bpfmanConfig, internal.BpfmanOperatorFinalizer); updated {
			if err := r.Update(ctx, bpfmanConfig); err != nil {
				r.Logger.Error(err, "failed adding bpfman-operator finalizer to bpfman config")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		}
		return r.ReconcileBpfmanConfig(ctx, req, bpfmanConfig)
	}

	return ctrl.Result{}, nil
}

func (r *BpfmanConfigReconciler) ReconcileBpfmanConfig(ctx context.Context, req ctrl.Request, bpfmanConfig *corev1.ConfigMap) (ctrl.Result, error) {
	bpfmanCsiDriver := &storagev1.CSIDriver{}
	// one-shot try to create bpfman's CSIDriver object if it doesn't exist, does not re-trigger reconcile.
	if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: internal.BpfmanCsiDriverName}, bpfmanCsiDriver); err != nil {
		if errors.IsNotFound(err) {
			bpfmanCsiDriver = LoadCsiDriver(r.CsiDriverDeployment)

			r.Logger.Info("Creating Bpfman csi driver object")
			if err := r.Create(ctx, bpfmanCsiDriver); err != nil {
				r.Logger.Error(err, "Failed to create Bpfman csi driver")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		} else {
			r.Logger.Error(err, "Failed to get csi.bpfman.io csidriver")
		}
	}

	if r.IsOpenshift {
		bpfmanRestrictedSCC := &osv1.SecurityContextConstraints{}
		// one-shot try to create the bpfman-restricted SCC if it doesn't exist, does not re-trigger reconcile.
		if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: internal.BpfmanRestrictedSccName}, bpfmanRestrictedSCC); err != nil {
			if errors.IsNotFound(err) {
				bpfmanRestrictedSCC = LoadRestrictedSecurityContext(r.RestrictedSCC)

				r.Logger.Info("Creating Bpfman restricted scc object for unprivileged users to bind to")
				if err := r.Create(ctx, bpfmanRestrictedSCC); err != nil {
					r.Logger.Error(err, "Failed to create Bpfman restricted scc")
					return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
				}
			} else {
				r.Logger.Error(err, "Failed to get bpfman-restricted scc")
			}
		}
	}

	bpfmanDeployment := &appsv1.DaemonSet{}
	staticBpfmanDeployment := LoadAndConfigureBpfmanDs(bpfmanConfig, r.BpfmanStandardDeployment, r.IsOpenshift)
	r.Logger.V(1).Info("StaticBpfmanDeployment with CSI", "DS", staticBpfmanDeployment)
	if err := r.Get(ctx, types.NamespacedName{Namespace: bpfmanConfig.Namespace, Name: internal.BpfmanDsName}, bpfmanDeployment); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("Creating Bpfman Daemon")
			// Causes Requeue
			if err := r.Create(ctx, staticBpfmanDeployment); err != nil {
				r.Logger.Error(err, "Failed to create Bpfman Daemon")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
			return ctrl.Result{}, nil
		}

		r.Logger.Error(err, "Failed to get bpfman daemon")
		return ctrl.Result{}, nil
	}

	if !bpfmanConfig.DeletionTimestamp.IsZero() {
		r.Logger.Info("Deleting bpfman daemon and config")
		controllerutil.RemoveFinalizer(bpfmanDeployment, internal.BpfmanOperatorFinalizer)

		err := r.Update(ctx, bpfmanDeployment)
		if err != nil {
			r.Logger.Error(err, "failed removing bpfman-operator finalizer from bpfmanDs")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		bpfmanCsiDriver := &storagev1.CSIDriver{}

		// one-shot try to delete bpfman's CSIDriver object only if it exists.
		if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: internal.BpfmanCsiDriverName}, bpfmanCsiDriver); err == nil {
			r.Logger.Info("Deleting Bpfman csi driver object")
			if err := r.Delete(ctx, bpfmanCsiDriver); err != nil {
				r.Logger.Error(err, "Failed to delete Bpfman csi driver")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		}

		// Delete metrics-proxy daemonset if it exists
		metricsProxyDeployment := &appsv1.DaemonSet{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: bpfmanConfig.Namespace, Name: internal.BpfmanMetricsProxyDsName}, metricsProxyDeployment); err == nil {
			r.Logger.Info("Deleting Metrics Proxy daemon")
			controllerutil.RemoveFinalizer(metricsProxyDeployment, internal.BpfmanOperatorFinalizer)
			if err := r.Update(ctx, metricsProxyDeployment); err != nil {
				r.Logger.Error(err, "failed removing bpfman-operator finalizer from metrics proxy DS")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
			if err = r.Delete(ctx, metricsProxyDeployment); err != nil {
				r.Logger.Error(err, "failed deleting metrics proxy DS")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		}

		if err = r.Delete(ctx, bpfmanDeployment); err != nil {
			r.Logger.Error(err, "failed deleting bpfman DS")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		if r.IsOpenshift {
			bpfmanRestrictedSCC := &osv1.SecurityContextConstraints{}
			// one-shot try to delete the bpfman
			// restricted SCC object but only if it
			// exists.
			if err := r.Get(ctx, types.NamespacedName{Namespace: corev1.NamespaceAll, Name: internal.BpfmanRestrictedSccName}, bpfmanRestrictedSCC); err == nil {
				r.Logger.Info("Deleting Bpfman restricted SCC object")
				if err := r.Delete(ctx, bpfmanRestrictedSCC); err != nil {
					r.Logger.Error(err, "Failed to delete Bpfman restricted SCC")
					return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
				}
			}
		}

		controllerutil.RemoveFinalizer(bpfmanConfig, internal.BpfmanOperatorFinalizer)
		err = r.Update(ctx, bpfmanConfig)
		if err != nil {
			r.Logger.Error(err, "failed removing bpfman-operator finalizer from bpfman config")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}

		return ctrl.Result{}, nil
	}

	if !reflect.DeepEqual(staticBpfmanDeployment.Spec, bpfmanDeployment.Spec) {
		r.Logger.Info("Reconciling bpfman")

		// Causes Requeue
		if err := r.Update(ctx, staticBpfmanDeployment); err != nil {
			r.Logger.Error(err, "failed reconciling bpfman deployment")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	// Reconcile metrics-proxy daemonset
	metricsProxyDeployment := &appsv1.DaemonSet{}
	staticMetricsProxyDeployment := LoadAndConfigureMetricsProxyDs(bpfmanConfig, r.BpfmanMetricsProxyDeployment, r.IsOpenshift)
	r.Logger.V(1).Info("StaticMetricsProxyDeployment", "DS", staticMetricsProxyDeployment)
	if err := r.Get(ctx, types.NamespacedName{Namespace: bpfmanConfig.Namespace, Name: internal.BpfmanMetricsProxyDsName}, metricsProxyDeployment); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Info("Creating Metrics Proxy Daemon")
			// Causes Requeue
			if err := r.Create(ctx, staticMetricsProxyDeployment); err != nil {
				r.Logger.Error(err, "Failed to create Metrics Proxy Daemon")
				return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
			}
		} else {
			r.Logger.Error(err, "Failed to get metrics proxy daemon")
		}
	} else if !reflect.DeepEqual(staticMetricsProxyDeployment.Spec, metricsProxyDeployment.Spec) {
		r.Logger.Info("Reconciling metrics proxy")

		// Causes Requeue
		if err := r.Update(ctx, staticMetricsProxyDeployment); err != nil {
			r.Logger.Error(err, "failed reconciling metrics proxy deployment")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationOperator}, nil
		}
	}

	return ctrl.Result{}, nil
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

// Only reconcile on bpfman-metrics-proxy Daemonset events.
func bpfmanMetricsProxyPredicate() predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == internal.BpfmanMetricsProxyDsName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == internal.BpfmanMetricsProxyDsName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == internal.BpfmanMetricsProxyDsName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == internal.BpfmanMetricsProxyDsName
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
	bpfmanConfigs := config.Data["bpfman.toml"]

	// Annotate the log level on the ds so we get automatic restarts on changes.
	if staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations == nil {
		staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}

	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.loglevel"] = bpfmanLogLevel
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.agent.loglevel"] = bpfmanAgentLogLevel
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.agent.healthprobeaddr"] = bpfmanHealthProbeAddr
	staticBpfmanDeployment.Spec.Template.ObjectMeta.Annotations["bpfman.io.bpfman.toml"] = bpfmanConfigs
	staticBpfmanDeployment.Name = internal.BpfmanDsName
	staticBpfmanDeployment.Namespace = config.Namespace
	staticBpfmanDeployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)
	for cindex, container := range staticBpfmanDeployment.Spec.Template.Spec.Containers {
		switch container.Name {
		case internal.BpfmanContainerName:
			staticBpfmanDeployment.Spec.Template.Spec.Containers[cindex].Image = bpfmanImage
		case internal.BpfmanAgentContainerName:
			staticBpfmanDeployment.Spec.Template.Spec.Containers[cindex].Image = bpfmanAgentImage
			for aindex, arg := range container.Args {
				if bpfmanHealthProbeAddr != "" {
					if strings.Contains(arg, "health-probe-bind-address") {
						staticBpfmanDeployment.Spec.Template.Spec.Containers[cindex].Args[aindex] =
							"--health-probe-bind-address=" + bpfmanHealthProbeAddr
					}
				}
			}
		default:
			// Do nothing
		}
	}

	controllerutil.AddFinalizer(staticBpfmanDeployment, internal.BpfmanOperatorFinalizer)

	return staticBpfmanDeployment
}

func LoadAndConfigureMetricsProxyDs(config *corev1.ConfigMap, path string, isOpenshift bool) *appsv1.DaemonSet {
	// Load static metrics-proxy deployment from disk
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

	staticMetricsProxyDeployment := obj.(*appsv1.DaemonSet)

	// Runtime Configurable fields
	bpfmanAgentImage := config.Data["bpfman.agent.image"]

	// Set the name and namespace from the config
	staticMetricsProxyDeployment.Name = internal.BpfmanMetricsProxyDsName
	staticMetricsProxyDeployment.Namespace = config.Namespace
	staticMetricsProxyDeployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)

	// Configure the metrics-proxy container image
	for cindex, container := range staticMetricsProxyDeployment.Spec.Template.Spec.Containers {
		if container.Name == internal.BpfmanMetricsProxyContainer {
			staticMetricsProxyDeployment.Spec.Template.Spec.Containers[cindex].Image = bpfmanAgentImage
		}
	}

	// Add OpenShift-specific TLS configuration
	if isOpenshift {
		// Add serving certificate annotation
		if staticMetricsProxyDeployment.Spec.Template.Annotations == nil {
			staticMetricsProxyDeployment.Spec.Template.Annotations = make(map[string]string)
		}
		staticMetricsProxyDeployment.Spec.Template.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = "agent-metrics-tls"

		// Add TLS volume mount to container
		for cindex, container := range staticMetricsProxyDeployment.Spec.Template.Spec.Containers {
			if container.Name == internal.BpfmanMetricsProxyContainer {
				staticMetricsProxyDeployment.Spec.Template.Spec.Containers[cindex].VolumeMounts = append(
					staticMetricsProxyDeployment.Spec.Template.Spec.Containers[cindex].VolumeMounts,
					corev1.VolumeMount{
						Name:      "agent-metrics-tls",
						MountPath: "/tmp/k8s-webhook-server/serving-certs",
						ReadOnly:  true,
					},
				)
			}
		}

		// Add TLS volume
		staticMetricsProxyDeployment.Spec.Template.Spec.Volumes = append(
			staticMetricsProxyDeployment.Spec.Template.Spec.Volumes,
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

	controllerutil.AddFinalizer(staticMetricsProxyDeployment, internal.BpfmanOperatorFinalizer)

	return staticMetricsProxyDeployment
}
