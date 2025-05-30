/*
Copyright 2025.

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
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	var (
		enableHTTP2 bool
		metricsAddr string
		socketPath  string
		certDir     string
	)

	flag.BoolVar(&enableHTTP2, "enable-http2", false, "Enable HTTP/2 on the metrics server")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "Metrics server address")
	flag.StringVar(&socketPath, "socket", "/var/run/bpfman-metrics/metrics.sock", "Path to Unix socket to proxy")
	flag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory for TLS certs")
	flag.Parse()

	opts := zap.Options{Development: false}
	switch os.Getenv("GO_LOG") {
	case "debug":
		opts.Development = true
	case "trace":
		opts.Development = true
		opts.Level = zapcore.Level(-2)
	}

	disableHTTP2 := func(cfg *tls.Config) {
		if !enableHTTP2 {
			cfg.NextProtos = []string{"http/1.1"}
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("metrics-proxy")
	log.Info("starting", "http2", enableHTTP2, "addr", metricsAddr, "socket", socketPath)

	transport := http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false, // Enable keepalives for connection reuse
	}

	upstreamURL, err := url.Parse("http://unix")
	if err != nil {
		log.Error(err, "failed to parse upstream URL")
		os.Exit(1)
	}

	proxy := &httputil.ReverseProxy{
		Transport: &transport,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstreamURL)
			r.Out.URL.Path = r.In.URL.Path
			r.Out.URL.RawQuery = r.In.URL.RawQuery
		},
		ModifyResponse: func(resp *http.Response) error {
			log.Info("received response from upstream", "socket", socketPath, "status", resp.StatusCode)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error(err, "proxy error during request processing")
			http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusInternalServerError)
		},
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Metrics:                server.Options{BindAddress: "0"}, // disable default metrics
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
	})
	if err != nil {
		log.Error(err, "create manager failed")
		os.Exit(1)
	}

	// Must create metrics server after manager so filters (i.e.,
	// FilterProvider) can access the client config.
	metricsHandler := redirectAgentMetrics("/agent-metrics", "/metrics", proxy, log)
	metricsServer, err := server.NewServer(server.Options{
		BindAddress:    metricsAddr,
		SecureServing:  true,
		CertDir:        certDir,
		TLSOpts:        []func(*tls.Config){disableHTTP2},
		FilterProvider: filters.WithAuthenticationAndAuthorization,
		ExtraHandlers: map[string]http.Handler{
			"/agent-metrics": metricsHandler,
		},
	}, mgr.GetConfig(), mgr.GetHTTPClient())
	if err != nil {
		log.Error(err, "create metrics server failed")
		os.Exit(1)
	}

	if err := mgr.Add(metricsServer); err != nil {
		log.Error(err, "add metrics server failed")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("metrics-socket", readiness(&transport, upstreamURL)); err != nil {
		log.Error(err, "add readiness check failed")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		log.Error(err, "add health check failed")
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "manager exited with error")
		os.Exit(1)
	}
}

// redirectAgentMetrics rewrites requests to a known prefix (e.g.
// "/agent-metrics") to the internal agent metrics path (e.g.
// "/metrics"). It logs the original request details and rejects all
// non-matching paths with 404.
func redirectAgentMetrics(prefix, targetPath string, next http.Handler, logger logr.Logger) http.Handler {
	logger = logger.WithName("redirect")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		logger.Info("incoming request", "method", r.Method, "path", r.URL.Path, "host", r.Host, "remoteAddr", r.RemoteAddr, "userAgent", r.Header.Get("User-Agent"))
		r.URL.Path = targetPath
		next.ServeHTTP(w, r)
	})
}

// readiness checks that the upstream metrics endpoint is reachable
// and returns 2xx.
func readiness(transport *http.Transport, upstreamURL *url.URL) func(*http.Request) error {
	return func(_ *http.Request) error {
		url := upstreamURL.String() + "/metrics"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return fmt.Errorf("readyz: cannot build request: %w", err)
		}

		resp, err := transport.RoundTrip(req)
		if err != nil {
			return fmt.Errorf("readyz: cannot connect: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("readyz: bad response: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		}

		return nil
	}
}
