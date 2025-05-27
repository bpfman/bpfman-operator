//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/bpfman/bpfman-operator/internal"
)

func createMonitoringToken(ctx context.Context, t *testing.T) (string, error) {
	t.Helper()
	clientset := env.Cluster().Client()

	serviceAccount := "test-monitoring"
	namespace := internal.BpfmanNamespace

	t.Logf("Creating test service account: %s/%s", namespace, serviceAccount)

	if err := createMonitoringServiceAccount(ctx, clientset, serviceAccount, namespace); err != nil {
		return "", fmt.Errorf("failed to create monitoring service account: %w", err)
	}

	t.Cleanup(func() {
		cleanupMonitoringServiceAccount(context.Background(), clientset, serviceAccount, namespace, t)
	})

	req := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: func() *int64 { i := int64(3600); return &i }(), // 1 hour
		},
	}

	tokenResp, err := clientset.CoreV1().
		ServiceAccounts(namespace).
		CreateToken(ctx, serviceAccount, req, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create monitoring token for %s/%s: %w", namespace, serviceAccount, err)
	}

	return tokenResp.Status.Token, nil
}

func testProxyDirectMetrics(ctx context.Context, t *testing.T, token string) {
	t.Helper()
	pod := findPodBySelector(ctx, t, "name=bpfman-metrics-proxy", "proxy")
	testHealthEndpoint(ctx, t, pod.Name, 8081)
	metricsPort, _ := setupPortForwardWithAutoPort(ctx, t, pod.Name, 8443)
	testMetricsEndpointLoad(t, metricsPort, "/metrics", "direct /metrics endpoint (no proxy)", token)
}

func testProxyRelayMetrics(ctx context.Context, t *testing.T, token string) {
	t.Helper()
	pod := findPodBySelector(ctx, t, "name=bpfman-metrics-proxy", "proxy")
	metricsPort, _ := setupPortForwardWithAutoPort(ctx, t, pod.Name, 8443)
	testMetricsEndpointLoad(t, metricsPort, "/agent-metrics", "proxy metrics endpoint /agent-metrics (unix socket relay)", token)
}

func testMetricsEndpointLoad(t *testing.T, port int, path, description, token string) {
	t.Helper()

	t.Logf("Testing %s with 100 sequential requests", description)
	t.Logf("Port forward established on port %d", port)

	client := createOptimizedHTTPClient(t, port)
	url := fmt.Sprintf("https://127.0.0.1:%d%s", port, path)

	const numRequests = 100

	start := time.Now()
	var firstBodyLen int

	for i := 1; i <= numRequests; i++ {
		if i%10 == 0 {
			t.Logf("Completed %d/%d requests", i, numRequests)
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			t.Fatalf("Failed to create request %d: %v", i, err)
		}

		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("Request %d got status %d, expected 200", i, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("Failed to read body on request %d: %v", i, err)
		}

		if i == 1 {
			firstBodyLen = len(body)
		}
	}

	duration := time.Since(start)
	rps := float64(numRequests) / duration.Seconds()

	t.Logf("Successfully completed all %d requests in %v", numRequests, duration)
	t.Logf("Rate: %.1f requests/second", rps)
	t.Logf("Response body length: %d bytes", firstBodyLen)
}

func testOperatorMetrics(ctx context.Context, t *testing.T, token string) {
	t.Helper()
	pod := findPodBySelector(ctx, t, "control-plane=controller-manager", "operator")
	testHealthEndpoint(ctx, t, pod.Name, 8175)
	metricsPort, _ := setupPortForwardWithAutoPort(ctx, t, pod.Name, 8443)
	testMetricsEndpointWithPort(ctx, t, metricsPort, "/metrics", token)
}

func testAuthenticationFailures(ctx context.Context, t *testing.T) {
	t.Helper()
	proxyPod := findPodBySelector(ctx, t, "name=bpfman-metrics-proxy", "proxy")
	operatorPod := findPodBySelector(ctx, t, "control-plane=controller-manager", "operator")

	// Set up port forwards once for each pod
	proxyPort, _ := setupPortForwardWithAutoPort(ctx, t, proxyPod.Name, 8443)
	operatorPort, _ := setupPortForwardWithAutoPort(ctx, t, operatorPod.Name, 8443)

	invalidTokens := []struct {
		name        string
		token       string
		description string
	}{
		{
			name:        "NoToken",
			token:       "",
			description: "no authorization header",
		},
		{
			name:        "InvalidToken",
			token:       "invalid-token-12345",
			description: "completely invalid token",
		},
		{
			name:        "MalformedJWT",
			token:       "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.invalid.signature",
			description: "malformed JWT token",
		},
		{
			name:        "BogusJWT",
			token:       "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkJvZ3VzIFRva2VuIiwiaWF0IjoxNTE2MjM5MDIyfQ.invalid-signature-here",
			description: "bogus but well-formed JWT",
		},
	}

	endpoints := []struct {
		port int
		path string
		desc string
	}{
		{proxyPort, "/agent-metrics", "proxy metrics"},
		{operatorPort, "/metrics", "operator metrics"},
	}

	for _, invalidToken := range invalidTokens {
		for _, endpoint := range endpoints {
			t.Run(fmt.Sprintf("%s_%s", invalidToken.name, endpoint.desc), func(t *testing.T) {
				t.Logf("Testing %s with %s", endpoint.desc, invalidToken.description)

				resp, err := makeHTTPRequestToPort(ctx, t, endpoint.port, endpoint.path, true, invalidToken.token)
				require.NoError(t, err, "HTTP request should not fail at transport level")
				defer resp.Body.Close()

				require.True(t,
					resp.StatusCode == http.StatusUnauthorized ||
						resp.StatusCode == http.StatusForbidden ||
						resp.StatusCode == http.StatusInternalServerError,
					"Expected 401, 403, or 500, got %d for %s with %s", resp.StatusCode, endpoint.desc, invalidToken.description)

				t.Logf("Correctly rejected %s with status %d", invalidToken.description, resp.StatusCode)
			})
		}
	}
}

func testHealthEndpoint(ctx context.Context, t *testing.T, podName string, port int32) {
	t.Helper()
	resp, err := makeHTTPRequest(ctx, t, podName, port, "/healthz", false, "")
	require.NoError(t, err, "Health request failed")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read health response")

	require.Equal(t, http.StatusOK, resp.StatusCode, "Health check should return 200")
	require.Contains(t, string(body), "ok", "Health response should contain 'ok'")
	t.Logf("Health endpoint OK for pod %s", podName)
}

func testMetricsEndpoint(ctx context.Context, t *testing.T, podName string, port int32, path, token string) {
	resp, err := makeHTTPRequest(ctx, t, podName, port, path, true, token)
	require.NoError(t, err, "Metrics request failed")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"Metrics endpoint should return 200 with authentication")

	body, err := io.ReadAll(resp.Body)
	if err != nil && err != io.ErrUnexpectedEOF {
		require.NoError(t, err, "Failed to read metrics response")
	}

	bodyStr := string(body)

	require.Contains(t, bodyStr, "# HELP", "Response should contain Prometheus metrics")
	require.Contains(t, bodyStr, "# TYPE", "Response should contain Prometheus metric types")

	// Verify we have proper Prometheus metrics format and content
	validatePrometheusMetrics(t, bodyStr, path, "")
}

func testMetricsEndpointWithPort(ctx context.Context, t *testing.T, port int, path, token string) {
	resp, err := makeHTTPRequestToPort(ctx, t, port, path, true, token)
	require.NoError(t, err, "Metrics request failed")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"Metrics endpoint should return 200 with authentication")

	body, err := io.ReadAll(resp.Body)
	if err != nil && err != io.ErrUnexpectedEOF {
		require.NoError(t, err, "Failed to read metrics response")
	}

	bodyStr := string(body)

	require.Contains(t, bodyStr, "# HELP", "Response should contain Prometheus metrics")
	require.Contains(t, bodyStr, "# TYPE", "Response should contain Prometheus metric types")

	// Verify we have proper Prometheus metrics format and content
	validatePrometheusMetrics(t, bodyStr, path, "")
}

func testMetricsEndpointWithPortNonFatal(ctx context.Context, t *testing.T, port int, path, token string) error {
	resp, err := makeHTTPRequestToPort(ctx, t, port, path, true, token)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("failed to read response: %w", err)
	}

	bodyStr := string(body)

	if !strings.Contains(bodyStr, "# HELP") || !strings.Contains(bodyStr, "# TYPE") {
		return fmt.Errorf("response does not contain expected Prometheus metrics format")
	}

	return nil
}

func makeHTTPRequest(ctx context.Context, t *testing.T, podName string, port int32, path string, useTLS bool, token string) (*http.Response, error) {
	// Set up port forwarding with automatic port allocation
	localPort, _ := setupPortForwardWithAutoPort(ctx, t, podName, int(port))

	// Port forward is already ready (verified in setupPortForwardWithListener)

	// Create HTTP client with keepalives enabled for better performance
	transport := &http.Transport{
		DisableKeepAlives:  false,
		DisableCompression: true,
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
	}

	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}

	scheme := "http"
	if useTLS {
		pool, err := extractServerCertificate(localPort)
		if err != nil {
			return nil, fmt.Errorf("failed to extract server certificate: %w", err)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
		scheme = "https"
	}

	url := fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, localPort, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return client.Do(req)
}

func makeHTTPRequestToPort(ctx context.Context, t *testing.T, port int, path string, useTLS bool, token string) (*http.Response, error) {
	// Create HTTP client with connection reuse
	transport := &http.Transport{
		DisableKeepAlives:  false,
		DisableCompression: true,
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
	}

	client := &http.Client{
		Timeout:   10 * time.Second, // Shorter timeout for individual requests
		Transport: transport,
	}

	scheme := "http"
	if useTLS {
		pool, err := extractServerCertificate(port)
		if err != nil {
			return nil, fmt.Errorf("failed to extract server certificate: %w", err)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
		scheme = "https"
	}

	url := fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, port, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return client.Do(req)
}

func setupPortForwardWithAutoPort(ctx context.Context, t *testing.T, podName string, remotePort int) (int, chan struct{}) {
	// Set up port forwarding with automatic port allocation (port 0)
	stopCh, readyCh, portCh := setupPortForward(ctx, t, podName, remotePort)

	// Wait for port-forward to be ready and get the assigned port
	select {
	case <-readyCh:
		// Port forwarding is ready, get the assigned port
		select {
		case localPort := <-portCh:
			return localPort, stopCh
		case <-time.After(1 * time.Second):
			close(stopCh)
			t.Fatal("Failed to get assigned port from port forwarder")
		}
	case <-time.After(5 * time.Second):
		close(stopCh)
		t.Fatal("Failed to establish port forwarding")
	}

	// This should never be reached due to t.Fatal above
	panic("unreachable")
}

func setupPortForward(ctx context.Context, t *testing.T, podName string, remotePort int) (chan struct{}, chan struct{}, chan int) {
	config := env.Cluster().Config()
	clientset := env.Cluster().Client()

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(internal.BpfmanNamespace).
		Name(podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(config)
	require.NoError(t, err, "Failed to create SPDY transport")

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{})
	portCh := make(chan int, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Port forwarding goroutine panic: %v", r)
			}
		}()

		pf, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", remotePort)}, stopCh, readyCh, io.Discard, io.Discard)
		if err != nil {
			t.Errorf("Port forward init failed: %v", err)
			return
		}

		go func() {
			if err := pf.ForwardPorts(); err != nil {
				if !strings.Contains(err.Error(), "connection reset") &&
					!strings.Contains(err.Error(), "use of closed network connection") {
					t.Logf("Port forwarding ended: %v", err)
				}
			}
		}()

		<-readyCh
		ports, err := pf.GetPorts()
		if err != nil {
			t.Errorf("Failed to get ports: %v", err)
			return
		}
		if len(ports) == 0 {
			t.Errorf("No ports returned from port forwarder")
			return
		}
		portCh <- int(ports[0].Local)
	}()

	return stopCh, readyCh, portCh
}

func extractServerCertificate(port int) (*x509.CertPool, error) {
	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return nil, fmt.Errorf("TLS dial failed: %w", err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates received")
	}

	pool := x509.NewCertPool()
	for _, cert := range certs {
		pool.AddCert(cert)
	}
	return pool, nil
}

func validatePrometheusMetrics(t *testing.T, body, path, podName string) {
	lines := strings.Split(body, "\n")

	var (
		helpCount    int
		typeCount    int
		metricCount  int
		validMetrics []string
	)

	expectedMetrics := map[string]bool{
		"controller_runtime_": false, // Controller runtime metrics
		"go_memstats_":        false, // Go runtime metrics
		"process_":            false, // Process metrics
		"workqueue_":          false, // Workqueue metrics (if present)
	}

	if !strings.Contains(path, "agent-metrics") {
		// Operator-specific metrics we might see
		expectedMetrics["certwatcher_"] = false
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "# HELP ") {
				helpCount++
			} else if strings.HasPrefix(line, "# TYPE ") {
				typeCount++
			}
			continue
		}

		// This should be a metric line.
		if strings.Contains(line, " ") || strings.Contains(line, "\t") {
			metricCount++
			metricName := strings.Split(line, " ")[0]
			metricName = strings.Split(metricName, "\t")[0]

			for pattern := range expectedMetrics {
				if strings.HasPrefix(metricName, pattern) {
					expectedMetrics[pattern] = true
					validMetrics = append(validMetrics, metricName)
					break
				}
			}
		}
	}

	require.Greater(t, helpCount, 0, "Should have HELP lines")
	require.Greater(t, typeCount, 0, "Should have TYPE lines")
	require.Greater(t, metricCount, 10, "Should have substantial number of metrics")

	foundCategories := 0
	for pattern, found := range expectedMetrics {
		if found {
			foundCategories++
			t.Logf("Found metrics matching %s", pattern)
		}
	}
	require.Greater(t, foundCategories, 0, "Should find at least one expected metric category")

	if podName != "" {
		t.Logf("Metrics endpoint %s OK for pod %s", path, podName)
	} else {
		t.Logf("Metrics endpoint %s OK", path)
	}
	t.Logf("Found %d HELP lines, %d TYPE lines, %d metric lines", helpCount, typeCount, metricCount)

	if len(validMetrics) > 0 {
		sampleCount := 3
		if len(validMetrics) < sampleCount {
			sampleCount = len(validMetrics)
		}
		t.Logf("Sample metrics: %v", validMetrics[:sampleCount])
	}
}

func createMonitoringServiceAccount(ctx context.Context, clientset kubernetes.Interface, serviceAccount, namespace string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccount,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "serviceaccount",
				"app.kubernetes.io/instance":  serviceAccount,
				"app.kubernetes.io/component": "metrics-test",
			},
		},
	}

	_, err := clientset.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create service account: %w", err)
	}

	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-metrics-binding", serviceAccount),
			Labels: map[string]string{
				"app.kubernetes.io/name":      "clusterrolebinding",
				"app.kubernetes.io/instance":  fmt.Sprintf("%s-metrics-binding", serviceAccount),
				"app.kubernetes.io/component": "metrics-test",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "bpfman-metrics-reader", // kustomize prefixed name
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount,
				Namespace: namespace,
			},
		},
	}

	_, err = clientset.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create cluster role binding: %w", err)
	}

	return nil
}

func cleanupMonitoringServiceAccount(ctx context.Context, clientset kubernetes.Interface, serviceAccount, namespace string, t *testing.T) {
	bindingName := fmt.Sprintf("%s-metrics-binding", serviceAccount)
	err := clientset.RbacV1().ClusterRoleBindings().Delete(ctx, bindingName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Logf("Warning: failed to cleanup cluster role binding %s: %v", bindingName, err)
	}

	err = clientset.CoreV1().ServiceAccounts(namespace).Delete(ctx, serviceAccount, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Logf("Warning: failed to cleanup service account %s/%s: %v", namespace, serviceAccount, err)
	}
}

func findPodBySelector(ctx context.Context, t *testing.T, selector, podType string) corev1.Pod {
	t.Helper()
	pods, err := env.Cluster().Client().CoreV1().
		Pods(internal.BpfmanNamespace).
		List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
	require.NoError(t, err, "Failed to list %s pods", podType)
	require.NotEmpty(t, pods.Items, "No %s pods found", podType)

	pod := pods.Items[0]
	t.Logf("Testing %s pod: %s", podType, pod.Name)
	return pod
}

func createOptimizedHTTPClient(t *testing.T, port int) *http.Client {
	t.Helper()

	transport := &http.Transport{
		DisableKeepAlives:  false,
		DisableCompression: true,
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
	}

	pool, err := extractServerCertificate(port)
	if err != nil {
		t.Fatalf("Failed to extract server certificate: %v", err)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: pool}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
}

func TestMetricsAuthentication(t *testing.T) {
	t.Log("Testing metrics authentication and proxy flow")

	ctx := context.Background()

	token, err := createMonitoringToken(ctx, t)
	require.NoError(t, err, "Failed to create monitoring token")
	require.NotEmpty(t, token, "Token should not be empty")
	t.Log("Successfully obtained monitoring token")

	t.Run("ProxyDirectMetrics", func(t *testing.T) {
		testProxyDirectMetrics(ctx, t, token)
	})

	t.Run("ProxyRelayMetrics", func(t *testing.T) {
		testProxyRelayMetrics(ctx, t, token)
	})

	t.Run("OperatorMetrics", func(t *testing.T) {
		testOperatorMetrics(ctx, t, token)
	})
}

func TestMetricsAuthenticationFailures(t *testing.T) {
	t.Log("Testing metrics authentication failure scenarios")
	ctx := context.Background()
	testAuthenticationFailures(ctx, t)
}
