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
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagent "github.com/bpfman/bpfman-operator/controllers/bpfman-agent"
	"github.com/bpfman/bpfman-operator/internal/conn"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"

	"github.com/go-logr/logr"
	"github.com/netobserv/netobserv-ebpf-agent/pkg/ifaces"
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
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	//+kubebuilder:scaffold:imports
)

const (
	buffersLength = 50

	// Internal metrics socket path for metrics-proxy
	// communication.
	internalMetricsSocketPath = "/var/run/bpfman-agent/metrics.sock"
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

// agentMetricsServer provides a minimal HTTP server for exposing
// Prometheus metrics on a Unix domain socket.
//
// It wraps a net.Listener and an http.Server, listening on the
// configured socketPath. The server is expected to be started via the
// run() method, which handles context-driven shutdown.
//
// The socket is created with world-readable permissions (0666) and
// should be cleaned up by the caller on teardown.
type agentMetricsServer struct {
	socketPath string
	listener   net.Listener
	server     *http.Server
}

// newAgentMetricsServer initialises an agentMetricsServer that
// exposes a Prometheus metrics endpoint via a Unix domain socket.
//
// It first removes any pre-existing socket file at socketPath, then
// creates and listens on a new Unix socket. The socket file is given
// 0666 permissions, but access is still subject to directory and
// security policy constraints.
//
// The returned server exposes:
//   - /metrics: Prometheus metrics served via promhttp.
//   - All other paths return HTTP 404.
//
// The caller is responsible for invoking run() on the returned server
// and handling graceful shutdown using the context lifecycle.
func newAgentMetricsServer(socketPath string) (*agentMetricsServer, error) {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("removing existing metrics socket %q: %w", socketPath, err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listening on Unix socket %q: %w", socketPath, err)
	}

	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
		return nil, fmt.Errorf("setting permissions %04o on socket %q: %w", 0666, socketPath, err)
	}

	return &agentMetricsServer{
		socketPath: socketPath,
		listener:   listener,
		server: &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/metrics" {
					promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
				} else {
					http.NotFound(w, r)
				}
			}),
		},
	}, nil
}

// run starts the HTTP metrics server and blocks until shutdown is
// triggered.
//
// It runs Serve on the configured Unix listener in a goroutine and
// waits for one of the following conditions:
//
//   - The context is cancelled (e.g. due to signal handling or manager shutdown).
//   - The HTTP server encounters a non-graceful failure during Serve.
//
// If the context is cancelled, run performs an orderly shutdown using
// server.Shutdown with a timeout. If Serve fails, the error is
// returned directly. Shutdown is skipped in the failure case since
// the server has already exited.
//
// This function ensures that the Serve goroutine exits before
// returning and removes the socket file when exiting.
func (ms *agentMetricsServer) run(ctx context.Context, logger logr.Logger) error {
	defer func() {
		if err := os.Remove(ms.socketPath); err != nil && !os.IsNotExist(err) {
			logger.Error(err, "failed to remove socket file", "path", ms.socketPath)
		}
	}()

	errCh := make(chan error, 1)

	go func() {
		logger.Info("Starting metrics server", "socket", ms.socketPath)
		if err := ms.server.Serve(ms.listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("serve on socket %q: %w", ms.socketPath, err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("context cancelled")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ms.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown of metrics server on socket %q: %w", ms.socketPath, err)
		}
		return nil
	case err := <-errCh:
		logger.Info("server failed")
		return err
	}
}

func main() {
	var probeAddr string
	var opts zap.Options
	var enableHTTP2, enableInterfacesDiscovery bool
	var pprofAddr string
	var certDir string

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

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		PprofBindAddress:       pprofAddr,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
		// Specify that Secrets's should not be cached.
		Client: client.Options{
			Cache: &client.CacheOptions{
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

	containerGetter, err := bpfmanagent.NewRealContainerGetter(nodeName)
	if err != nil {
		setupLog.Error(err, "unable to create containerGetter")
		os.Exit(1)
	}

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

	metricsServer, err := newAgentMetricsServer(internalMetricsSocketPath)
	if err != nil {
		setupLog.Error(err, "failed to set up metrics server")
		os.Exit(1)
	}

	// Subscribe to signals for terminating the program.
	signalCtx := ctrl.SetupSignalHandler()
	ctx, cancel := context.WithCancel(signalCtx)

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

	go func() {
		log := ctrl.Log.WithName("agent").WithName("metrics-server")
		if err := metricsServer.run(ctx, log); err != nil {
			log.Error(err, "run failed")
		}
		// Cancel if shutdown isn't in progress.
		if ctx.Err() == nil {
			cancel()
		}
	}()

	setupLog.Info("starting Bpfman-Agent")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
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
