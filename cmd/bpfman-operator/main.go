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

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanoperator "github.com/bpfman/bpfman-operator/controllers/bpfman-operator"
	"github.com/bpfman/bpfman-operator/internal"

	osv1 "github.com/openshift/api/security/v1"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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

// Returns true if the current platform is Openshift.
func isOpenshift(client discovery.DiscoveryInterface, cfg *rest.Config) (bool, error) {
	k8sVersion, err := client.ServerVersion()
	if err != nil {
		setupLog.Info("issue occurred while fetching ServerVersion")
		return false, err
	}

	setupLog.Info("detected platform version", "PlatformVersion", k8sVersion)
	apiList, err := client.ServerGroups()
	if err != nil {
		setupLog.Info("issue occurred while fetching ServerGroups")
		return false, err
	}

	for _, v := range apiList.Groups {
		if v.Name == "route.openshift.io" {
			setupLog.Info("route.openshift.io found in apis, platform is OpenShift")
			return true, nil
		}
	}
	return false, nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var opts zap.Options
	var enableHTTP2 bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8174", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8175", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableHTTP2, "enable-http2", enableHTTP2, "If HTTP/2 should be enabled for the metrics and webhook servers.")
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
			TLSOpts:     []func(*tls.Config){disableHTTP2},
		},
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
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	common := bpfmanoperator.ReconcilerCommon{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	setupLog.Info("Discovering APIs")
	dc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "can't instantiate discovery client")
		os.Exit(1)
	}

	isOpenshift, err := isOpenshift(dc, mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to determine platform")
		os.Exit(1)

	}

	if err = (&bpfmanoperator.BpfmanConfigReconciler{
		ReconcilerCommon:         common,
		BpfmanStandardDeployment: internal.BpfmanDaemonManifestPath,
		CsiDriverDeployment:      internal.BpfmanCsiDriverPath,
		RestrictedSCC:            internal.BpfmanRestrictedSCCPath,
		IsOpenshift:              isOpenshift,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create bpfmanCofig controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.XdpProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create xdpProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.TcProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create tcProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.TracepointProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create tracepointProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.KprobeProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create kprobeProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.UprobeProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create uprobeProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.FentryProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create fentryProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanoperator.FexitProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create fexitProgram controller", "controller", "BpfProgram")
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
