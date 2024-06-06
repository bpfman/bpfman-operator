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
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagent "github.com/bpfman/bpfman-operator/controllers/bpfman-agent"

	"github.com/bpfman/bpfman-operator/internal/conn"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"

	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/credentials/insecure"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	utilruntime.Must(v1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var opts zap.Options
	var enableHTTP2 bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8174", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8175", "The address the probe endpoint binds to.")
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
		LeaderElection:         false,
		// Specify that Secrets's should not be cached.
		Client: client.Options{Cache: &client.CacheOptions{
			DisableFor: []client.Object{&v1.Secret{}},
		},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Set up a connection to bpfman, block until bpfman is up.
	setupLog.Info("Waiting for active connection to bpfman")
	conn, err := conn.CreateConnection(context.Background(), insecure.NewCredentials())
	if err != nil {
		setupLog.Error(err, "unable to connect to bpfman")
		os.Exit(1)
	}

	nodeName := os.Getenv("KUBE_NODE_NAME")
	if nodeName == "" {
		setupLog.Error(fmt.Errorf("KUBE_NODE_NAME env var not set"), "Couldn't determine bpfman-agent's node")
		os.Exit(1)
	}

	common := bpfmanagent.ReconcilerCommon{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		GrpcConn:     conn,
		BpfmanClient: gobpfman.NewBpfmanClient(conn),
		NodeName:     nodeName,
	}

	if err = (&bpfmanagent.XdpProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create xdpProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanagent.TcProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create tcProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanagent.TracepointProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create tracepointProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanagent.KprobeProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create kprobeProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanagent.UprobeProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create uprobeProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanagent.FentryProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create fentryProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanagent.FexitProgramReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create fexitProgram controller", "controller", "BpfProgram")
		os.Exit(1)
	}

	if err = (&bpfmanagent.BpfApplicationReconciler{
		ReconcilerCommon: common,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create BpfApplicationProgram controller", "controller", "BpfProgram")
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

	setupLog.Info("starting Bpfman-Agent")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
