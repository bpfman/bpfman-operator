//go:build integration_tests
// +build integration_tests

package lifecycle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
	bpfmanHelpers "github.com/bpfman/bpfman-operator/pkg/helpers"
	osv1 "github.com/openshift/api/security/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/yaml"
)

const (
	// Test timeouts and intervals.
	testTimeout  = 5 * time.Minute
	pollInterval = 2 * time.Second

	// Test values.
	invalidValue = "invalid"
	fieldOwner   = "lifecycle-test"
)

// Image defaults - overridden by environment variables in CI.
var (
	bpfmanImage      = getEnvOrDefault("BPFMAN_IMG", "quay.io/bpfman/bpfman:latest")
	bpfmanAgentImage = getEnvOrDefault("BPFMAN_AGENT_IMG", "quay.io/bpfman/bpfman-agent:latest")
)

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// Create new Config with modified settings
var (
	newConfig = &v1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanConfigName,
		},
		Spec: v1alpha1.ConfigSpec{
			Namespace: "bpfman",
			Configuration: `[database]
max_retries = 35
millisec_delay = 10000
[signing]
allow_unsigned = true
verify_enabled = true`,
			Agent: v1alpha1.AgentSpec{
				Image:           bpfmanAgentImage,
				LogLevel:        "debug", // Changed from info to debug
				HealthProbePort: 8175,
			},
			Daemon: v1alpha1.DaemonSpec{
				Image:    bpfmanImage,
				LogLevel: "bpfman=info",
			},
		},
	}

	runtimeClient client.Client
	isOpenShift   bool
	hasMonitoring bool
)

// managedObjects returns the set of resources the operator is expected
// to own.  Platform-conditional gating (monitoring, OpenShift) appears
// here and nowhere else.  Each call returns fresh objects suitable for
// the API client to deserialise into.
func managedObjects() []client.Object {
	objects := []client.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanCmName,
				Namespace: internal.BpfmanNamespace,
			},
		},
		&storagev1.CSIDriver{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanCsiDriverName,
			},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanDsName,
				Namespace: internal.BpfmanNamespace,
			},
		},
	}
	if hasMonitoring {
		objects = append(objects, &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanMetricsProxyDsName,
				Namespace: internal.BpfmanNamespace,
			},
		})
	}
	if isOpenShift {
		objects = append(objects, &osv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
		})
	}
	return objects
}

// TestLifecycle runs the complete integration test suite for the bpfman operator lifecycle.
// It tests configuration management, resource creation/deletion, modification handling,
// and cascading deletion scenarios to ensure the operator behaves correctly.
func TestLifecycle(t *testing.T) {
	var originalConfig *v1alpha1.Config
	var err error

	ctx := context.Background()
	logger := zap.New()
	scheme := runtime.NewScheme()

	// Get Kubernetes client for OpenShift detection and determine if this is OpenShift
	fmt.Println("INFO: Determine if this is OpenShift")
	kubernetesClient, err := kubernetes.NewForConfig(bpfmanHelpers.GetK8sConfigOrDie())
	if err != nil {
		t.Fatalf("Could not get new kubernetes config, err: %q", err)
	}
	isOpenShift, err = internal.IsOpenShift(kubernetesClient.DiscoveryClient, logger)
	if err != nil {
		t.Fatal(err)
	}
	hasMonitoring, err = internal.HasMonitoringAPI(kubernetesClient.DiscoveryClient, logger)
	if err != nil {
		t.Fatal(err)
	}
	// Add OpenShift scheme if needed.
	if isOpenShift {
		utilruntime.Must(osv1.Install(scheme))
	}

	// Create controller-runtime client.
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(bpfmaniov1alpha1.Install(scheme))
	runtimeClient, err = client.New(bpfmanHelpers.GetK8sConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("Could not create new runtime client, err: %q", err)
	}

	// Test config get.
	t.Logf("Running: TestGetCurrentConfig")
	originalConfig, err = getCurrentConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get current config: %q", err)
	}
	if originalConfig != nil {
		originalConfig.ResourceVersion = ""
	}

	// Verify the operator bootstrapped the Config CR with correct defaults.
	t.Logf("Running: TestConfigBootstrap")
	if err := testConfigBootstrap(ctx, t, originalConfig); err != nil {
		t.Fatalf("Failed config bootstrap verification: %q", err)
	}

	// After this test run, we want to restore to the original configuration.
	defer func() {
		t.Logf("Restoring original config")
		if err := testConfigCascadingDeletion(ctx, t); err != nil {
			t.Fatalf("Failed config cascading deletion: %q", err)
		}
		if err := testConfigCreation(ctx, t, originalConfig); err != nil {
			t.Fatalf("Failed config recreation: %q", err)
		}
	}()

	// Make sure that all resources are there at the start.
	t.Logf("Running: TestEnsureResources")
	if err := waitForResourceCreation(ctx); err != nil {
		t.Fatalf("Failed to ensure resources: %q", err)
	}

	// Test deleting resources.
	t.Logf("Running: TestResourceDeletion")
	if err := testResourceDeletion(ctx, t); err != nil {
		t.Fatalf("Failed to restore resources: %q", err)
	}

	// Test reconciling modified resources.
	t.Logf("Running: TestResourceModification")
	if err := testResourceModification(ctx, t); err != nil {
		t.Fatalf("Failed to restore resource modifications: %q", err)
	}

	// Test cascading deletion.
	t.Logf("Running: TestConfigCascadingDeletion")
	if err := testConfigCascadingDeletion(ctx, t); err != nil {
		t.Fatalf("Failed config cascading deletion: %q", err)
	}

	// Test recovery via the default-config subcommand exec'd in the operator pod.
	t.Logf("Running: TestDefaultConfigRecovery")
	if err := testDefaultConfigRecovery(ctx, t); err != nil {
		t.Fatalf("Failed default-config recovery: %q", err)
	}

	// Test config recreation with modified settings.
	t.Logf("Running: TestConfigRecreationWithModifiedSettings")
	if err := testConfigCreation(ctx, t, newConfig); err != nil {
		t.Fatalf("Failed config recreation: %q", err)
	}

	// Test config stuck deletion scenario - this should be run
	// after we have a working Config.
	t.Logf("Running: TestConfigStuckDeletion")
	if err := testConfigStuckDeletion(ctx, t); err != nil {
		t.Fatalf("Failed config stuck deletion test: %q", err)
	}
}

// getCurrentConfig retrieves the current bpfman Config resource from the cluster.
// Returns the Config object or nil if not found, along with any error encountered.
func getCurrentConfig(ctx context.Context) (*v1alpha1.Config, error) {
	config := &v1alpha1.Config{}
	err := runtimeClient.Get(ctx, types.NamespacedName{Name: internal.BpfmanConfigName}, config)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get Config: %v", err)
	}
	if errors.IsNotFound(err) {
		return nil, nil
	}

	return config, nil
}

// testResourceDeletion verifies that the operator can recreate resources after deletion.
// Deletes each managed resource individually and waits for the operator to recreate them.
func testResourceDeletion(ctx context.Context, t *testing.T) error {
	for _, obj := range managedObjects() {
		key := client.ObjectKeyFromObject(obj)
		t.Logf("Deleting %s", key)
		if err := runtimeClient.Delete(ctx, obj); err != nil {
			return fmt.Errorf("could not delete %s: %w", key, err)
		}
		if err := waitForResourceCreation(ctx); err != nil {
			return err
		}
	}

	// Make sure that the operator pod is still running and healthy.
	return testOperatorPodHealthy(ctx)
}

// testResourceModification verifies that the operator reconciles modified resources back to desired state.
// Modifies ConfigMap data, CSI driver SELinuxMount setting, and DaemonSet service account names.
func testResourceModification(ctx context.Context, t *testing.T) error {
	// Test ConfigMap modification.
	cm := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Name: internal.BpfmanCmName, Namespace: internal.BpfmanNamespace}
	if err := runtimeClient.Get(ctx, cmKey, cm); err != nil {
		return err
	}
	origData := cm.Data["bpfman.toml"]
	// Create a new ConfigMap for server-side apply (this way, no retry logic needed).
	applyCM := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm.Name,
			Namespace: cm.Namespace,
		},
		Data: map[string]string{
			"bpfman.toml": invalidValue,
		},
	}
	if err := runtimeClient.Patch(ctx, applyCM, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner)); err != nil {
		return fmt.Errorf("could not apply patch, err: %w", err)
	}
	// Wait until reconciliation.
	if err := waitUntilCondition(
		ctx,
		func() (bool, error) {
			t.Logf("Checking that config map was correctly reconciled")
			cm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: internal.BpfmanCmName, Namespace: internal.BpfmanNamespace}
			if err := runtimeClient.Get(ctx, cmKey, cm); err != nil {
				return false, err
			}
			return cm.Data["bpfman.toml"] == origData, nil
		},
	); err != nil {
		return err
	}

	// Test CSI Driver modification.
	csiDriver := &storagev1.CSIDriver{}
	csiKey := types.NamespacedName{Name: internal.BpfmanCsiDriverName}
	if err := runtimeClient.Get(ctx, csiKey, csiDriver); err != nil {
		return err
	}
	origSELinuxMount := csiDriver.Spec.SELinuxMount
	// Create a new CSI Driver for server-side apply (this way, no retry logic needed).
	var newSELinuxMount *bool
	if origSELinuxMount == nil || !*origSELinuxMount {
		newSELinuxMount = ptr.To(true)
	} else {
		newSELinuxMount = ptr.To(false)
	}
	applyCSI := &storagev1.CSIDriver{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "storage.k8s.io/v1",
			Kind:       "CSIDriver",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanCsiDriverName,
		},
		Spec: storagev1.CSIDriverSpec{
			SELinuxMount: newSELinuxMount,
		},
	}
	if err := runtimeClient.Patch(ctx, applyCSI, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner)); err != nil {
		return fmt.Errorf("could not apply patch, err: %w", err)
	}
	// Wait until reconciliation.
	if err := waitUntilCondition(
		ctx,
		func() (bool, error) {
			t.Logf("Checking that CSI was correctly reconciled")
			csiDriver := &storagev1.CSIDriver{}
			csiKey := types.NamespacedName{Name: internal.BpfmanCsiDriverName}
			if err := runtimeClient.Get(ctx, csiKey, csiDriver); err != nil {
				return false, err
			}
			if csiDriver.Spec.SELinuxMount == nil {
				if origSELinuxMount == nil {
					return true, nil
				}
			} else if origSELinuxMount != nil && *csiDriver.Spec.SELinuxMount == *origSELinuxMount {
				return true, nil
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	// Test bpfman DaemonSet modification.
	bpfmanDs := &appsv1.DaemonSet{}
	bpfmanDsKey := types.NamespacedName{Name: internal.BpfmanDsName, Namespace: internal.BpfmanNamespace}
	if err := runtimeClient.Get(ctx, bpfmanDsKey, bpfmanDs); err != nil {
		return err
	}
	origServiceAccountName := bpfmanDs.Spec.Template.Spec.ServiceAccountName
	// Create a new DaemonSet for server-side apply (this way, no retry logic needed).
	applyBpfmanDs := &appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanDsName,
			Namespace: internal.BpfmanNamespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: bpfmanDs.Spec.Selector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: bpfmanDs.Spec.Template.ObjectMeta,
				Spec: corev1.PodSpec{
					ServiceAccountName: invalidValue,
				},
			},
		},
	}
	if err := runtimeClient.Patch(ctx, applyBpfmanDs, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner)); err != nil {
		return fmt.Errorf("could not apply patch, err: %w", err)
	}
	// Wait until reconciliation.
	if err := waitUntilCondition(
		ctx,
		func() (bool, error) {
			t.Logf("Checking that standard ds was correctly reconciled")
			bpfmanDs := &appsv1.DaemonSet{}
			bpfmanDsKey := types.NamespacedName{Name: internal.BpfmanDsName, Namespace: internal.BpfmanNamespace}
			if err := runtimeClient.Get(ctx, bpfmanDsKey, bpfmanDs); err != nil {
				return false, err
			}
			return bpfmanDs.Spec.Template.Spec.ServiceAccountName == origServiceAccountName, nil
		},
	); err != nil {
		return err
	}

	// Test metrics proxy DaemonSet modification if monitoring is available.
	if hasMonitoring {
		metricsDs := &appsv1.DaemonSet{}
		metricsDsKey := types.NamespacedName{Name: internal.BpfmanMetricsProxyDsName, Namespace: internal.BpfmanNamespace}
		if err := runtimeClient.Get(ctx, metricsDsKey, metricsDs); err != nil {
			return err
		}
		origServiceAccountName = metricsDs.Spec.Template.Spec.ServiceAccountName
		// Create a new DaemonSet for server-side apply (this way, no retry needed).
		applyMetricsDs := &appsv1.DaemonSet{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps/v1",
				Kind:       "DaemonSet",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanMetricsProxyDsName,
				Namespace: internal.BpfmanNamespace,
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: metricsDs.Spec.Selector,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metricsDs.Spec.Template.ObjectMeta,
					Spec: corev1.PodSpec{
						ServiceAccountName: invalidValue,
					},
				},
			},
		}
		if err := runtimeClient.Patch(ctx, applyMetricsDs, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner)); err != nil {
			return fmt.Errorf("could not apply patch, err: %w", err)
		}
		// Wait until reconciliation.
		if err := waitUntilCondition(
			ctx,
			func() (bool, error) {
				t.Logf("Checking that metrics ds was correctly reconciled")
				metricsDs := &appsv1.DaemonSet{}
				metricsDsKey := types.NamespacedName{Name: internal.BpfmanMetricsProxyDsName, Namespace: internal.BpfmanNamespace}
				if err := runtimeClient.Get(ctx, metricsDsKey, metricsDs); err != nil {
					return false, err
				}
				return metricsDs.Spec.Template.Spec.ServiceAccountName == origServiceAccountName, nil
			},
		); err != nil {
			return err
		}
	}

	// Test OpenShift SCC modification if on OpenShift.
	if isOpenShift {
		scc := &osv1.SecurityContextConstraints{}
		sccKey := types.NamespacedName{Name: internal.BpfmanRestrictedSccName}
		if err := runtimeClient.Get(ctx, sccKey, scc); err != nil {
			return err
		}
		origAllowHostNetwork := scc.AllowHostNetwork

		// Create a new SCC for server-side apply (this way, no retry needed).
		applySCC := &osv1.SecurityContextConstraints{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "security.openshift.io/v1",
				Kind:       "SecurityContextConstraints",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
			AllowHostNetwork: !origAllowHostNetwork,
		}
		if err := runtimeClient.Patch(ctx, applySCC, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner)); err != nil {
			return fmt.Errorf("could not apply patch, err: %w", err)
		}
		// Wait until reconcile.
		if err := waitUntilCondition(
			ctx,
			func() (bool, error) {
				t.Logf("Checking that SCC was correctly reconciled")
				scc := &osv1.SecurityContextConstraints{}
				sccKey := types.NamespacedName{Name: internal.BpfmanRestrictedSccName}
				if err := runtimeClient.Get(ctx, sccKey, scc); err != nil {
					return false, err
				}
				return scc.AllowHostNetwork == origAllowHostNetwork, nil
			},
		); err != nil {
			return err
		}
	}

	// Make sure that the operator pod is still running and healthy.
	return testOperatorPodHealthy(ctx)
}

// testConfigCascadingDeletion verifies that deleting the Config resource triggers deletion of all owned resources.
// Tests the cascading deletion behavior and ensures the operator remains healthy throughout.
func testConfigCascadingDeletion(ctx context.Context, t *testing.T) error {
	// Delete the Config CRD.
	config := &v1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanConfigName,
		},
	}
	if err := runtimeClient.Delete(ctx, config); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete Config: %w", err)
	}

	// Wait for and verify all owned resources are deleted
	if err := waitForResourceDeletion(ctx, t); err != nil {
		return fmt.Errorf("failed to wait for resource deletion: %w", err)
	}

	// Make sure that the operator pod is still running and healthy.
	return testOperatorPodHealthy(ctx)
}

// testConfigCreation creates a new Config resource and verifies that all managed resources
// are created with the new configuration settings. Validates that ConfigMap contains
// the expected configuration values from the new Config spec.
func testConfigCreation(ctx context.Context, t *testing.T, newConfig *v1alpha1.Config) error {
	err := runtimeClient.Create(ctx, newConfig)
	if err != nil {
		return fmt.Errorf("failed to create new Config: %v", err)
	}

	// Wait for resources to be recreated with new configuration.
	if err := waitForResourceCreation(ctx); err != nil {
		return fmt.Errorf("failed to wait for resource creation: %v", err)
	}

	// Verify configuration changes are applied.
	configMap := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Name: internal.BpfmanConfigName, Namespace: internal.BpfmanNamespace}
	if err := runtimeClient.Get(ctx, cmKey, configMap); err != nil {
		return fmt.Errorf("failed to get configmap: %v", err)
	}

	if configMap.Data["bpfman.toml"] != newConfig.Spec.Configuration {
		return fmt.Errorf("config map bpfman.toml not correct")
	}
	if configMap.Data["bpfman.agent.log.level"] != newConfig.Spec.Agent.LogLevel {
		return fmt.Errorf("config map bpfman.agent.log.level not correct")
	}
	if configMap.Data["bpfman.log.level"] != newConfig.Spec.Daemon.LogLevel {
		return fmt.Errorf("config map bpfman.log.level not correct")
	}

	return testOperatorPodHealthy(ctx)
}

// testOperatorPodHealthy verifies that the bpfman operator deployment is running and healthy.
// Checks that the number of ready replicas matches the desired replica count.
func testOperatorPodHealthy(ctx context.Context) error {
	operatorDeployment := &appsv1.Deployment{}
	deployKey := types.NamespacedName{Name: internal.BpfmanOperatorName, Namespace: internal.BpfmanNamespace}
	err := runtimeClient.Get(ctx, deployKey, operatorDeployment)
	if err != nil {
		return err
	}
	if operatorDeployment.Spec.Replicas == nil ||
		*operatorDeployment.Spec.Replicas != operatorDeployment.Status.ReadyReplicas {
		return fmt.Errorf("operator deployment is not healthy")
	}
	return nil
}

// waitForResourceCreation polls for all managed resources to be created and ready.
func waitForResourceCreation(ctx context.Context) error {
	return waitUntilCondition(ctx, func() (bool, error) {
		for _, obj := range managedObjects() {
			if err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			if ds, ok := obj.(*appsv1.DaemonSet); ok && ds.Status.NumberAvailable == 0 {
				return false, nil
			}
		}
		return true, nil
	})
}

// waitForResourceDeletion polls for all managed resources to be deleted.
func waitForResourceDeletion(ctx context.Context, t *testing.T) error {
	return waitUntilCondition(ctx, func() (bool, error) {
		for _, obj := range managedObjects() {
			key := client.ObjectKeyFromObject(obj)
			if err := runtimeClient.Get(ctx, key, obj); err == nil {
				t.Logf("%s not yet deleted", key)
				return false, nil
			} else if !errors.IsNotFound(err) {
				return false, err
			}
		}
		return true, nil
	})
}

func waitUntilCondition(ctx context.Context, conditionFunc func() (bool, error)) error {
	timeoutCTX, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCTX.Done():
			return fmt.Errorf("timeout or context cancelled: %w", timeoutCTX.Err())
		case <-ticker.C:
			b, err := conditionFunc()
			if err != nil {
				return err
			}
			if b {
				return nil
			}
		}
	}
}

// testConfigBootstrap verifies that the operator auto-created the Config CR
// on startup with the expected default values from compiled constants. Image
// fields are checked for non-emptiness only because they come from
// environment variables which vary between test environments.
func testConfigBootstrap(ctx context.Context, t *testing.T, config *v1alpha1.Config) error {
	if config == nil {
		return fmt.Errorf("expected operator to bootstrap Config CR on startup, but none found")
	}
	if config.Name != internal.BpfmanConfigName {
		return fmt.Errorf("expected Config name %q, got %q", internal.BpfmanConfigName, config.Name)
	}
	if config.Spec.Namespace != internal.DefaultConfigNamespace {
		return fmt.Errorf("expected namespace %q, got %q", internal.DefaultConfigNamespace, config.Spec.Namespace)
	}
	if config.Spec.Agent.LogLevel != internal.DefaultLogLevel {
		return fmt.Errorf("expected agent log level %q, got %q", internal.DefaultLogLevel, config.Spec.Agent.LogLevel)
	}
	if config.Spec.Daemon.LogLevel != internal.DefaultLogLevel {
		return fmt.Errorf("expected daemon log level %q, got %q", internal.DefaultLogLevel, config.Spec.Daemon.LogLevel)
	}
	if config.Spec.Agent.HealthProbePort != internal.DefaultHealthProbePort {
		return fmt.Errorf("expected health probe port %d, got %d", internal.DefaultHealthProbePort, config.Spec.Agent.HealthProbePort)
	}
	if config.Spec.Configuration != internal.DefaultConfiguration {
		return fmt.Errorf("expected default configuration block, got %q", config.Spec.Configuration)
	}
	if config.Spec.Agent.Image == "" {
		return fmt.Errorf("expected non-empty agent image")
	}
	if config.Spec.Daemon.Image == "" {
		return fmt.Errorf("expected non-empty daemon image")
	}

	t.Logf("Config CR bootstrapped by operator with correct defaults (agent=%s, daemon=%s)",
		config.Spec.Agent.Image, config.Spec.Daemon.Image)
	return nil
}

// testDefaultConfigRecovery verifies that the default-config subcommand,
// exec'd inside the running operator pod, produces valid YAML that can be
// applied to restore the Config CR and bring all managed resources back.
// This mirrors the documented recovery procedure:
//
//	kubectl exec -n bpfman deploy/bpfman-operator -- /bpfman-operator default-config | kubectl apply -f -
func testDefaultConfigRecovery(ctx context.Context, t *testing.T) error {
	// Confirm the Config CR is absent (deleted by the preceding cascading deletion test).
	existing := &v1alpha1.Config{}
	err := runtimeClient.Get(ctx, types.NamespacedName{Name: internal.BpfmanConfigName}, existing)
	if !errors.IsNotFound(err) {
		if err == nil {
			return fmt.Errorf("Config CR should not exist before recovery test")
		}
		return err
	}

	// Set up a kubernetes client and REST config for pod exec.
	restConfig := bpfmanHelpers.GetK8sConfigOrDie()
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Find the operator pod.
	pods, err := kubeClient.CoreV1().Pods(internal.BpfmanNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "control-plane=controller-manager",
	})
	if err != nil {
		return fmt.Errorf("failed to list operator pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no operator pods found with label control-plane=controller-manager")
	}
	podName := pods.Items[0].Name
	t.Logf("Executing default-config in pod %s", podName)

	// Exec /bpfman-operator default-config in the operator container.
	req := kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(internal.BpfmanNamespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   []string{"/bpfman-operator", "default-config"},
			Container: "bpfman-operator",
			Stdout:    true,
			Stderr:    true,
		}, clientgoscheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return fmt.Errorf("default-config exec failed (stderr: %s): %w", stderr.String(), err)
	}
	t.Logf("default-config output:\n%s", stdout.String())

	// Parse the YAML output into a Config CR and create it.
	var recoveredConfig v1alpha1.Config
	if err := yaml.Unmarshal(stdout.Bytes(), &recoveredConfig); err != nil {
		return fmt.Errorf("failed to unmarshal default-config output: %w", err)
	}
	if err := runtimeClient.Create(ctx, &recoveredConfig); err != nil {
		return fmt.Errorf("failed to create Config CR from default-config output: %w", err)
	}
	t.Logf("Created Config CR from default-config subcommand output")

	// Verify the recovered Config CR has the correct default values.
	if err := testConfigBootstrap(ctx, t, &recoveredConfig); err != nil {
		return fmt.Errorf("recovered Config CR does not match defaults: %w", err)
	}

	// Wait for all managed resources to be recreated.
	if err := waitForResourceCreation(ctx); err != nil {
		return fmt.Errorf("resources did not recover after applying default-config output: %w", err)
	}
	t.Logf("All resources recovered via default-config subcommand")

	// Clean up for the next test by deleting the Config CR.
	return testConfigCascadingDeletion(ctx, t)
}

// testConfigStuckDeletion verifies that bpfman can handle scenarios
// where owned DaemonSets get stuck in deletion due to finalizers, and
// that the system can recover when those finalizers are removed. This
// simulates real-world scenarios like stuck pods, finalizer
// conflicts, or external dependencies blocking deletion.
func testConfigStuckDeletion(ctx context.Context, t *testing.T) error {
	const blockingFinalizer = "test.example.com/block-deletion"

	ds := &appsv1.DaemonSet{}
	dsKey := types.NamespacedName{Name: internal.BpfmanDsName, Namespace: internal.BpfmanNamespace}
	if err := runtimeClient.Get(ctx, dsKey, ds); err != nil {
		return fmt.Errorf("failed to get DaemonSet before blocking: %w", err)
	}

	// Ensure we clean up our test finalizer even if the test
	// fails This is critical for the main TestLifecycle defer
	// cleanup to work.
	defer func() {
		ds := &appsv1.DaemonSet{}
		if runtimeClient.Get(ctx, dsKey, ds) == nil {
			if slices.Contains(ds.Finalizers, blockingFinalizer) {
				t.Logf("Cleaning up test finalizer on test completion")
				controllerutil.RemoveFinalizer(ds, blockingFinalizer)
				runtimeClient.Update(ctx, ds)
			}
		}
	}()

	t.Logf("Adding blocking finalizer to DaemonSet to simulate stuck deletion")
	controllerutil.AddFinalizer(ds, blockingFinalizer)
	if err := runtimeClient.Update(ctx, ds); err != nil {
		return fmt.Errorf("failed to add blocking finalizer to DaemonSet: %w", err)
	}

	t.Logf("Deleting Config resource - DaemonSet should remain due to blocking finalizer")
	config := &v1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanConfigName,
		},
	}
	if err := runtimeClient.Delete(ctx, config); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete Config: %w", err)
	}

	t.Logf("Verifying Config is deleted and DaemonSet remains stuck")
	if err := waitUntilCondition(ctx, func() (bool, error) {
		config := &v1alpha1.Config{}
		err := runtimeClient.Get(ctx, types.NamespacedName{Name: internal.BpfmanConfigName}, config)
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		configGone := errors.IsNotFound(err)

		ds := &appsv1.DaemonSet{}
		if err := runtimeClient.Get(ctx, dsKey, ds); err != nil {
			if errors.IsNotFound(err) {
				return false, fmt.Errorf("DaemonSet should still exist but was deleted despite blocking finalizer")
			}
			return false, err
		}

		daemonSetStuck := !ds.DeletionTimestamp.IsZero() && slices.Contains(ds.Finalizers, blockingFinalizer)

		if configGone && daemonSetStuck {
			t.Logf("Config deleted and DaemonSet stuck in deletion due to blocking finalizer")
			return true, nil
		}

		t.Logf("Waiting for stuck state: Config gone=%v, DaemonSet stuck=%v", configGone, daemonSetStuck)
		return false, nil
	}); err != nil {
		return fmt.Errorf("Expected Config to be deleted and DaemonSet to be stuck: %w", err)
	}

	t.Logf("Removing blocking finalizer to allow DaemonSet deletion")
	if err := runtimeClient.Get(ctx, dsKey, ds); err != nil {
		return fmt.Errorf("failed to get DaemonSet for unblocking: %w", err)
	}

	controllerutil.RemoveFinalizer(ds, blockingFinalizer)
	if err := runtimeClient.Update(ctx, ds); err != nil {
		return fmt.Errorf("failed to remove blocking finalizer: %w", err)
	}

	t.Logf("Verifying DaemonSet deletion completes after removing finalizer")
	if err := waitUntilCondition(ctx, func() (bool, error) {
		ds := &appsv1.DaemonSet{}
		err := runtimeClient.Get(ctx, dsKey, ds)
		if errors.IsNotFound(err) {
			t.Logf("DaemonSet successfully deleted after removing blocking finalizer")
			return true, nil
		}
		if err != nil {
			return false, err
		}

		t.Logf("DaemonSet still exists, waiting for deletion...")
		return false, nil
	}); err != nil {
		return fmt.Errorf("DaemonSet should be deleted after removing finalizer: %w", err)
	}

	t.Logf("Config stuck deletion test completed successfully")
	return nil
}
