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
	"golang.org/x/sync/errgroup"
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

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(bpfmaniov1alpha1.Install(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// agentMetricsServer provides an HTTP server for exposing Prometheus
// metrics on a Unix domain socket.
//
// It wraps a net.Listener and an http.Server, configured to serve
// /metrics with Prometheus metrics and return HTTP 404 for all other
// paths.
type agentMetricsServer struct {
	socketPath string
	listener   net.Listener
	server     *http.Server
}

// interfaceDiscovery monitors network interface creation and deletion
// events.
//
// It wraps the netobserv interface watcher and registerer with a
// subscribed events channel and an interfaces map for tracking
// discovered interfaces.
type interfaceDiscovery struct {
	events     <-chan ifaces.Event
	interfaces *sync.Map
}

// newInterfaceDiscovery creates an interfaceDiscovery instance and
// subscribes to network interface events.
//
// It creates a netobserv watcher and registerer with the configured
// buffer length, then subscribes to interface creation and deletion
// events. The resulting events channel will be used by the run()
// method to process events.
//
// Returns an error if the subscription to interface events fails.
func newInterfaceDiscovery(ctx context.Context, interfaces *sync.Map) (*interfaceDiscovery, error) {
	informer := ifaces.NewWatcher(buffersLength)
	registerer := ifaces.NewRegisterer(informer, buffersLength)

	ifaceEvents, err := registerer.Subscribe(ctx)
	if err != nil {
		return nil, fmt.Errorf("subscribing to interface events: %w", err)
	}

	return &interfaceDiscovery{
		events:     ifaceEvents,
		interfaces: interfaces,
	}, nil
}

// run processes interface events and updates the interfaces map until
// the context is cancelled.
//
// This is a synchronous function that listens for EventAdded and
// EventDeleted events on the subscribed channel, adding or removing
// interfaces from the map accordingly. The function blocks until the
// provided context is cancelled or the events channel closes.
//
// Returns nil when the context is cancelled, or an error if the
// events channel closes unexpectedly.
func (id *interfaceDiscovery) run(ctx context.Context, logger logr.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-id.events:
			if !ok {
				return fmt.Errorf("interface events channel closed unexpectedly")
			}
			iface := event.Interface
			switch event.Type {
			case ifaces.EventAdded:
				logger.Info("interface created", "Name", iface.Name, "netns", iface.NSName, "NsHandle", iface.NetNS)
				id.interfaces.Store(iface, true)
			case ifaces.EventDeleted:
				logger.Info("interface deleted", "Name", iface.Name, "netns", iface.NSName, "NsHandle", iface.NetNS)
				id.interfaces.Delete(iface)
			default:
			}
		}
	}
}

// newAgentMetricsServer creates an agentMetricsServer that serves
// Prometheus metrics on a Unix domain socket.
//
// It removes any existing socket file at socketPath, creates a new
// Unix socket listener, and sets the socket file permissions to 0666.
// The returned server serves /metrics with Prometheus metrics and
// returns HTTP 404 for all other paths.
//
// The server must be started by calling its run() method with a
// context for shutdown coordination.
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

// run starts the HTTP server and blocks until the context is
// cancelled or the server fails.
//
// This is a synchronous function that starts the HTTP server in a
// goroutine and blocks waiting for either context cancellation or a
// server error. When the context is cancelled, it performs an orderly
// shutdown with a 5-second timeout. The socket file is removed when
// the function exits.
//
// Returns nil on successful shutdown, or an error if the server
// encounters a runtime error during serving.
func (ms *agentMetricsServer) run(ctx context.Context, logger logr.Logger) error {
	defer func() {
		if err := os.Remove(ms.socketPath); err != nil && !os.IsNotExist(err) {
			logger.Error(err, "failed to remove socket file", "path", ms.socketPath)
		}
	}()

	errCh := make(chan error, 1)

	go func() {
		logger.Info("starting", "socket", ms.socketPath)
		if err := ms.server.Serve(ms.listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("serve on socket %q: %w", ms.socketPath, err)
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ms.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown on socket %q: %w", ms.socketPath, err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// runAgent runs the agent runtime components and manages their
// lifecycle with coordinated shutdown guarantees.
//
// Components Started:
//   - Controller manager (mgr.Start) - always started
//   - Metrics server (agentMetricsServer) - always started
//   - Interface discovery (interfaceDiscovery) - optional, if enabled
//
// Each component runs in its own goroutine and receives a named
// logger derived from the provided logger (e.g., "agent.metrics",
// "agent.interface-discovery").
//
// Shutdown Behavior:
//   - Normal shutdown: External signal (SIGTERM/SIGINT) cancels the context,
//     all components receive ctx.Done(), exit cleanly, and log shutdown messages
//   - Error shutdown: Any component failure automatically cancels the context
//     for all other components via errgroup, ensuring coordinated shutdown
//
// Exit Strategy:
//   - errgroup.Wait() blocks until ALL goroutines complete, whether they
//     return nil (clean shutdown) or error (component failure)
//   - Components are guaranteed time to complete their shutdown sequence
//     and log their final messages before runAgent returns
//   - No explicit cleanup needed - components handle their own resource
//     cleanup (socket removal, server shutdown, etc.) via context cancellation
//
// Error Handling:
//   - First component error triggers context cancellation for all others
//   - All components complete shutdown before the error is returned
//   - Context cancellation is automatic via errgroup - no manual coordination
//
// Returns nil on successful coordinated shutdown, or the first
// component error encountered. In both cases, all components are
// guaranteed to have completed their shutdown sequence before return.
func runAgent(ctx context.Context, mgr ctrl.Manager, metricsServer *agentMetricsServer, ifaceDiscovery *interfaceDiscovery, logger logr.Logger) error {
	g, ctx := errgroup.WithContext(ctx)

	if ifaceDiscovery != nil {
		g.Go(func() error {
			log := logger.WithName("interface-discovery")
			if err := ifaceDiscovery.run(ctx, log); err != nil {
				return fmt.Errorf("interface discovery: %w", err)
			}
			log.Info("shut down")
			return nil
		})
	}

	g.Go(func() error {
		log := logger.WithName("metrics")
		if err := metricsServer.run(ctx, log); err != nil {
			return fmt.Errorf("metrics server: %w", err)
		}
		log.Info("shut down")
		return nil
	})

	g.Go(func() error {
		if err := mgr.Start(ctx); err != nil {
			return fmt.Errorf("manager: %w", err)
		}
		logger.Info("manager shut down")
		return nil
	})

	// Wait for all components to complete or for one to fail.
	// errgroup automatically cancels the context on first error.
	if err := g.Wait(); err != nil {
		logger.Error(err, "component failed")
		return err
	}

	logger.Info("all components shut down cleanly")
	return nil
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

	// Get the Log level for bpfman deployment where this pod is running.
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

	setupLog := ctrl.Log.WithName("setup")

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

	ctx := ctrl.SetupSignalHandler()

	var ifaceDiscovery *interfaceDiscovery
	if enableInterfacesDiscovery {
		ifaceDiscovery, err = newInterfaceDiscovery(ctx, commonApp.Interfaces)
		if err != nil {
			setupLog.Error(err, "failed to set up interface discovery")
			os.Exit(1)
		}
	}

	setupLog.Info("starting Bpfman-Agent")
	if err := runAgent(ctx, mgr, metricsServer, ifaceDiscovery, ctrl.Log.WithName("agent")); err != nil {
		setupLog.Error(err, "agent runtime failed, exiting")
		os.Exit(1)
	}

	// Normal shutdown (SIGTERM/SIGINT) exits with status code 0.
}
