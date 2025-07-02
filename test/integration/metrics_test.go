//go:build integration_tests
// +build integration_tests

// This file provides integration tests for bpfman-agent metrics
// endpoints.
//
// This package tests metrics authentication, collection, and proxy
// functionality using the metrics-proxy binary's built-in self-test
// capability to validate bpfman-agent metrics endpoints from within
// the pod's network context, eliminating port forwarding complexity.
// The self-test runs "/metrics-proxy test" with a monitoring token
// and returns structured JSON results for validation.
//
// The metrics-proxy pod exposes:
//   - /healthz (HTTP) - health check endpoint
//   - /metrics (HTTPS) - direct controller-runtime metrics with authentication
//   - /agent-metrics (HTTPS) - proxied bpfman-agent metrics via Unix socket with authentication
//
// Authentication uses Kubernetes ServiceAccount tokens with RBAC
// permissions via the "bpfman-metrics-reader" ClusterRole.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/bpfman/bpfman-operator/internal"
)

// agentMetricTestResult and agentMetricTestsResults types copied from
// cmd/metrics-proxy/main.go These must be kept in sync with the
// metrics-proxy implementation.

type agentMetricTestResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Metrics int    `json:"metrics,omitempty"`
}

type agentMetricsTestResults struct {
	Summary string                  `json:"summary"`
	Results []agentMetricTestResult `json:"results"`
}

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

	require.Equal(t, corev1.PodRunning, pod.Status.Phase,
		"%s pod %s must be in Running phase", podType, pod.Name)

	return pod
}

func testMetricsProxySelfTest(ctx context.Context, t *testing.T, pod corev1.Pod, token string) {
	t.Helper()

	cmd := []string{"env", "TOKEN=" + token, "/metrics-proxy", "test"}

	var stdout, stderr bytes.Buffer
	err := podExec(ctx, t, pod, &stdout, &stderr, cmd)

	// For self-test, we expect exit code 0 for success, non-zero
	// for failure. Parse the JSON regardless of exit code to get
	// detailed results. Only fail early if we can't get any output.
	output := stdout.String()
	if stderr.Len() > 0 {
		t.Logf("Stderr: %s", stderr.String())
	}

	if output == "" {
		t.Logf("Pod exec error: %v", err)
		t.Logf("Stderr: %s", stderr.String())
		t.Fatalf("No output received from metrics-proxy test command")
	}

	var results agentMetricsTestResults
	if parseErr := json.Unmarshal([]byte(output), &results); parseErr != nil {
		t.Logf("Pod exec error: %v", err)
		t.Logf("Raw output: %s", output)
		t.Fatalf("Failed to parse JSON results: %v", parseErr)
	}

	t.Logf("Self-test summary: %s", results.Summary)
	for _, result := range results.Results {
		if result.Status == "passed" {
			if result.Metrics > 0 {
				t.Logf("%s: PASSED (%d metrics)", result.Name, result.Metrics)
			} else {
				t.Logf("%s: PASSED", result.Name)
			}
		} else {
			t.Logf("%s: FAILED - %s", result.Name, result.Message)
		}
	}

	require.Equal(t, "passed", results.Summary, "Self-test should pass overall")
	require.NoError(t, err, "pod exec should succeed when self-test passes")
	require.Len(t, results.Results, 3, "Should have 3 test results")

	testsByName := make(map[string]agentMetricTestResult)
	for _, result := range results.Results {
		testsByName[result.Name] = result
	}

	// Health check should pass.
	healthResult, exists := testsByName["health_check"]
	require.True(t, exists, "Should have health_check result")
	require.Equal(t, "passed", healthResult.Status, "Health check should pass")

	// Direct metrics should pass.
	metricsResult, exists := testsByName["metrics"]
	require.True(t, exists, "Should have metrics result")
	require.Equal(t, "passed", metricsResult.Status, "Metrics endpoint should pass")
	require.Greater(t, metricsResult.Metrics, 0, "Should have metrics data")

	// Agent metrics proxy should pass.
	agentResult, exists := testsByName["agent-metrics"]
	require.True(t, exists, "Should have agent-metrics result")
	require.Equal(t, "passed", agentResult.Status, "Agent metrics endpoint should pass")
	require.Greater(t, agentResult.Metrics, 0, "Should have agent metrics data")

	t.Logf("All self-tests passed successfully on pod %s", pod.Name)
}

func podExec(ctx context.Context, t *testing.T, pod corev1.Pod, stdout, stderr *bytes.Buffer, cmd []string) error {
	t.Helper()
	kubeConfig, err := config.GetConfig()
	if err != nil {
		t.Fatalf("failed to get kube config: %v", err)
	}

	cl, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		t.Fatalf("failed to create kube client: %v", err)
	}

	req := cl.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	execOptions := &corev1.PodExecOptions{
		Command: cmd,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}

	req.VersionedParams(execOptions, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(kubeConfig, "POST", req.URL())
	if err != nil {
		return err
	}

	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})
}

// TestAgentMetricsCollection verifies that the metrics-proxy pod
// correctly exposes /healthz, /metrics, and /agent-metrics endpoints
// using in-pod execution of its built-in self-test.
//
// The self-test is executed 10 times to catch any flakiness in
// metrics availability, token propagation, or SPDY exec behaviour —
// especially in CI environments where startup timing or pod
// scheduling may vary.
//
// Each iteration is run as a subtest to isolate failures and make
// test output more readable and actionable.
func TestAgentMetricsCollection(t *testing.T) {
	ctx := context.Background()
	token, err := createMonitoringToken(ctx, t)
	require.NoError(t, err, "Failed to create monitoring token")
	require.NotEmpty(t, token, "Token should not be empty")
	t.Log("Successfully obtained monitoring token")

	proxyPod := findPodBySelector(ctx, t, "name=bpfman-metrics-proxy", "proxy")

	for i := 1; i <= 10; i++ {
		t.Run(fmt.Sprintf("SelfTestRun-%02d", i), func(t *testing.T) {
			testMetricsProxySelfTest(ctx, t, proxyPod, token)
		})
	}
}
