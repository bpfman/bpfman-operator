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
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagent "github.com/bpfman/bpfman-operator/controllers/bpfman-agent"
	"github.com/bpfman/bpfman-operator/internal/conn"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"

	"github.com/netobserv/netobserv-ebpf-agent/pkg/ifaces"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/credentials/insecure"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	//+kubebuilder:scaffold:imports
)

const buffersLength = 50

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
	var enableHTTP2, enableInterfacesDiscovery bool
	var pprofAddr string
	var certDir string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "The address the metric endpoint binds to. Use \"0\" to disable.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8175", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableHTTP2, "enable-http2", enableHTTP2, "If HTTP/2 should be enabled for the metrics and webhook servers.")
	flag.StringVar(&pprofAddr, "profiling-bind-address", "", "The address the profiling endpoint binds to, such as ':6060'. Leave unset to disable profiling.")
	flag.BoolVar(&enableInterfacesDiscovery, "enable-interfaces-discovery", true, "Enable ebpfman agent process to auto detect interfaces creation and deletion")
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

	certWatcher := setupCertWatcher(certDir, &metricsOptions.TLSOpts)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsOptions,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    9443,
			TLSOpts: []func(*tls.Config){disableHTTP2},
		}),
		PprofBindAddress:       pprofAddr,
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

	// Add the certificate watcher to the manager if it was
	// created. This ensures proper certificate rotation.
	if certWatcher != nil {
		if err := mgr.Add(certWatcher); err != nil {
			setupLog.Error(err, "unable to add certificate watcher to manager")
			os.Exit(1)
		}
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

	containerGetter, err := bpfmanagent.NewRealContainerGetter(nodeName)
	if err != nil {
		setupLog.Error(err, "unable to create containerGetter")
		os.Exit(1)
	}

	ctx, canceler := context.WithCancel(context.Background())
	// Subscribe to signals for terminating the program.
	go func() {
		stopper := make(chan os.Signal, 1)
		signal.Notify(stopper, os.Interrupt, syscall.SIGTERM)
		<-stopper
		canceler()
	}()

	commonApp := bpfmanagent.ReconcilerCommon{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		GrpcConn:     conn,
		BpfmanClient: gobpfman.NewBpfmanClient(conn),
		NodeName:     nodeName,
		Containers:   containerGetter,
		Interfaces:   &sync.Map{},
	}

	if err = (&bpfmanagent.ClBpfApplicationReconciler{
		ReconcilerCommon: commonApp,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create BpfApplicationReconciler")
		os.Exit(1)
	}

	if enableInterfacesDiscovery {
		informer := ifaces.NewWatcher(buffersLength)
		registerer := ifaces.NewRegisterer(informer, buffersLength)

		ifaceEvents, err := registerer.Subscribe(ctx)
		if err != nil {
			setupLog.Error(err, "instantiating interfaces' informer")
			os.Exit(1)
		}
		go interfaceListener(ctx, ifaceEvents, commonApp.Interfaces)
	}

	if err = (&bpfmanagent.NsBpfApplicationReconciler{
		ReconcilerCommon: commonApp,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create BpfNsApplicationReconciler")
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

func interfaceListener(ctx context.Context, ifaceEvents <-chan ifaces.Event, interfaces *sync.Map) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-ifaceEvents:
			iface := event.Interface
			switch event.Type {
			case ifaces.EventAdded:
				setupLog.Info("interface created", "Name", iface.Name, "netns", iface.NSName, "NsHandle", iface.NetNS)
				interfaces.Store(iface, true)
			case ifaces.EventDeleted:
				setupLog.Info("interface deleted", "Name", iface.Name, "netns", iface.NSName, "NsHandle", iface.NetNS)
				interfaces.Delete(iface)
			default:
			}
		}
	}
}
