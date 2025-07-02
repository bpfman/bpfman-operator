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
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	if len(os.Args) > 1 && os.Args[1] == "test" {
		runSelfTests()
		return
	}

	var (
		enableHTTP2 bool
		metricsAddr string
		socketPath  string
		certDir     string
	)

	flag.BoolVar(&enableHTTP2, "enable-http2", false, "Enable HTTP/2 on the metrics server")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "Metrics server address")
	flag.StringVar(&socketPath, "socket", "/var/run/bpfman-agent/metrics.sock", "Path to Unix socket to proxy")
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

type TestResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Metrics int    `json:"metrics,omitempty"`
}

type TestResults struct {
	Summary string       `json:"summary"`
	Results []TestResult `json:"results"`
}

func runSelfTests() {
	results := TestResults{
		Summary: "running",
		Results: make([]TestResult, 0),
	}

	token := os.Getenv("TOKEN")
	if token == "" {
		results.Summary = "failed"
		results.Results = append(results.Results, TestResult{
			Name:    "token_check",
			Status:  "failed",
			Message: "TOKEN environment variable not set",
		})
		outputResults(results)
		os.Exit(1)
	}

	// Test health endpoint
	healthResult := testHealthEndpoint()
	results.Results = append(results.Results, healthResult)

	// Test direct metrics endpoint
	directResult := testMetricsEndpoint("/metrics", token)
	results.Results = append(results.Results, directResult)

	// Test agent metrics proxy endpoint
	agentResult := testMetricsEndpoint("/agent-metrics", token)
	results.Results = append(results.Results, agentResult)

	// Determine overall status
	failed := false
	for _, result := range results.Results {
		if result.Status == "failed" {
			failed = true
			break
		}
	}

	if failed {
		results.Summary = "failed"
		outputResults(results)
		os.Exit(1)
	} else {
		results.Summary = "passed"
		outputResults(results)
	}
}

func testHealthEndpoint() TestResult {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get("http://localhost:8081/healthz")
	if err != nil {
		return TestResult{
			Name:    "health_check",
			Status:  "failed",
			Message: fmt.Sprintf("request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TestResult{
			Name:    "health_check",
			Status:  "failed",
			Message: fmt.Sprintf("read body failed: %v", err),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return TestResult{
			Name:    "health_check",
			Status:  "failed",
			Message: fmt.Sprintf("status %d, body: %s", resp.StatusCode, string(body)),
		}
	}

	if !strings.Contains(string(body), "ok") {
		return TestResult{
			Name:    "health_check",
			Status:  "failed",
			Message: fmt.Sprintf("response does not contain 'ok': %s", string(body)),
		}
	}

	return TestResult{
		Name:   "health_check",
		Status: "passed",
	}
}

func testMetricsEndpoint(path, token string) TestResult {
	// Create TLS config that accepts self-signed certificates.
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	// Try to get the actual server certificate for better
	// security.
	if pool := getServerCertPool(); pool != nil {
		tlsConfig.RootCAs = pool
		tlsConfig.InsecureSkipVerify = false
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	url := fmt.Sprintf("https://localhost:8443%s", path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return TestResult{
			Name:    strings.TrimPrefix(path, "/"),
			Status:  "failed",
			Message: fmt.Sprintf("create request failed: %v", err),
		}
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return TestResult{
			Name:    strings.TrimPrefix(path, "/"),
			Status:  "failed",
			Message: fmt.Sprintf("request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TestResult{
			Name:    strings.TrimPrefix(path, "/"),
			Status:  "failed",
			Message: fmt.Sprintf("read body failed: %v", err),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return TestResult{
			Name:    strings.TrimPrefix(path, "/"),
			Status:  "failed",
			Message: fmt.Sprintf("status %d, body: %s", resp.StatusCode, string(body)),
		}
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "# HELP") || !strings.Contains(bodyStr, "# TYPE") {
		return TestResult{
			Name:    strings.TrimPrefix(path, "/"),
			Status:  "failed",
			Message: "response does not contain Prometheus metrics format",
		}
	}

	// Count metrics lines (non-comment, non-empty lines).
	lines := strings.Split(bodyStr, "\n")
	metricCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			metricCount++
		}
	}

	return TestResult{
		Name:    strings.TrimPrefix(path, "/"),
		Status:  "passed",
		Metrics: metricCount,
	}
}

func getServerCertPool() *x509.CertPool {
	conn, err := tls.Dial("tcp", "localhost:8443", &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return nil
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil
	}

	pool := x509.NewCertPool()
	for _, cert := range certs {
		pool.AddCert(cert)
	}
	return pool
}

func outputResults(results TestResults) {
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Printf("ERROR: failed to marshal results: %v\n", err)
		return
	}
	fmt.Println(string(output))
}
