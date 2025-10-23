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

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"path/filepath"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanoperator "github.com/bpfman/bpfman-operator/controllers/bpfman-operator"
	"github.com/bpfman/bpfman-operator/internal"

	osv1 "github.com/openshift/api/security/v1"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(bpfmaniov1alpha1.Install(scheme))
	utilruntime.Must(osv1.Install(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var opts zap.Options
	var enableHTTP2 bool
	var certDir string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "The address the metric endpoint binds to. Use \"0\" to disable.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8175", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableHTTP2, "enable-http2", enableHTTP2, "If HTTP/2 should be enabled for the metrics and webhook servers.")
	flag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs", "The directory containing TLS certificates for HTTPS servers.")
	flag.Parse()

	// Get the Log level for bpfman deployment where this pod is running
	logLevel := os.Getenv("GO_LOG")
	switch logLevel {
	case "info":
		opts = zap.Options{
			Development: false,
		}
	case "debug":
		opts = zap.Options{
			Development: true,
		}
	case "trace":
		opts = zap.Options{
			Development: true,
			Level:       zapcore.Level(-2),
		}
	default:
		opts = zap.Options{
			Development: false,
		}
	}

	disableHTTP2 := func(c *tls.Config) {
		if enableHTTP2 {
			return
		}
		c.NextProtos = []string{"http/1.1"}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	metricsOptions := server.Options{
		BindAddress:    metricsAddr,
		SecureServing:  true,
		CertDir:        certDir,
		TLSOpts:        []func(*tls.Config){disableHTTP2},
		FilterProvider: filters.WithAuthenticationAndAuthorization,
	}

	setupLog.Info("metricsAddr", "metricsAddr", metricsAddr)

	certWatcher := setupCertWatcher(certDir, &metricsOptions.TLSOpts)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsOptions,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    9443,
			TLSOpts: []func(*tls.Config){disableHTTP2},
		}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "8730d955.bpfman.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.ConfigMap{}: {
					Field: fields.SelectorFromSet(fields.Set{"metadata.name": internal.BpfmanCmName}),
				},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Add the certificate watcher to the manager if it was
	// created. This ensures proper certificate rotation.
	if certWatcher != nil {
		if err := mgr.Add(certWatcher); err != nil {
			setupLog.Error(err, "unable to add certificate watcher to manager")
			os.Exit(1)
		}
	}

	commonApp := bpfmanoperator.ReconcilerCommon[bpfmaniov1alpha1.ClusterBpfApplicationState, bpfmaniov1alpha1.ClusterBpfApplicationStateList]{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	commonClusterApp := bpfmanoperator.ClusterApplicationReconciler{
		ReconcilerCommon: commonApp,
	}

	commonNsApp := bpfmanoperator.ReconcilerCommon[bpfmaniov1alpha1.BpfApplicationState, bpfmaniov1alpha1.BpfApplicationStateList]{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	commonNamespaceApp := bpfmanoperator.NamespaceApplicationReconciler{
		ReconcilerCommon: commonNsApp,
	}

	setupLog.Info("Discovering APIs")
	dc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "can't instantiate discovery client")
		os.Exit(1)
	}

	isOpenshift, err := internal.IsOpenShift(dc, setupLog)
	if err != nil {
		setupLog.Error(err, "unable to determine platform")
		os.Exit(1)

	}

	if err = (&bpfmanoperator.BpfmanConfigReconciler{
		ClusterApplicationReconciler: commonClusterApp,
		BpfmanStandardDS:             internal.BpfmanDaemonManifestPath,
		BpfmanMetricsProxyDS:         internal.BpfmanMetricsProxyPath,
		CsiDriverDS:                  internal.BpfmanCsiDriverPath,
		RestrictedSCC:                internal.BpfmanRestrictedSCCPath,
		IsOpenshift:                  isOpenshift,
		Recorder:                     mgr.GetEventRecorderFor("config-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create bpfmanConfig controller")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.BpfApplicationReconciler{
		ClusterApplicationReconciler: commonClusterApp,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create BpfApplicationReconciler controller")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.BpfNsApplicationReconciler{
		NamespaceApplicationReconciler: commonNamespaceApp,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create BpfNsApplicationReconciler controller")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// setupCertWatcher creates and configures a certificate watcher.
// Returns the watcher or nil if creation failed.
func setupCertWatcher(certDir string, tlsOpts *[]func(*tls.Config)) *certwatcher.CertWatcher {
	certPath := filepath.Join(certDir, "tls.crt")
	keyPath := filepath.Join(certDir, "tls.key")

	certWatcher, err := certwatcher.New(certPath, keyPath)
	if err != nil {
		setupLog.Error(err, "Unable to create certificate watcher", "certPath", certPath, "keyPath", keyPath)
		// Don't exit on failure - controller-runtime will
		// handle certificates if the watcher fails.
		return nil
	}

	*tlsOpts = append(*tlsOpts, func(c *tls.Config) {
		c.GetCertificate = certWatcher.GetCertificate
	})

	setupLog.Info("Certificate watcher configured for metrics TLS", "certPath", certPath)
	return certWatcher
}
