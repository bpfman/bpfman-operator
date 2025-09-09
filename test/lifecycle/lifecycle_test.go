//go:build integration_tests
// +build integration_tests

package lifecycle

import (
	"context"
	"fmt"
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
	"github.com/bpfman/bpfman-operator/test/testutil"
	osv1 "github.com/openshift/api/security/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	// Test timeouts and intervals.
	testTimeout  = 5 * time.Minute
	pollInterval = 2 * time.Second

	// Test values.
	invalidValue = "invalid"
	fieldOwner   = "lifecycle-test"
)

// Create new Config with modified settings
var (
	newConfig = &v1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanConfigName,
		},
		Spec: v1alpha1.ConfigSpec{
			Namespace: "bpfman",
			Image:     "quay.io/bpfman/bpfman:latest",
			LogLevel:  "bpfman=info", // Changed from debug to info
			Configuration: `[database]
max_retries = 35
millisec_delay = 10000
[signing]
allow_unsigned = true
verify_enabled = true`,
			Agent: v1alpha1.AgentSpec{
				HealthProbePort: 8175,
				Image:    "quay.io/bpfman/bpfman-agent:latest",
				LogLevel: "debug", // Changed from info to debug
			},
		},
	}

	runtimeClient client.Client
	isOpenShift   bool
)

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
	t.Logf("Running: waitForResourceCreation and waitForAvailable")
	if err := waitForResourceCreation(ctx); err != nil {
		t.Fatalf("Failed to ensure resources: %q", err)
	}
	if err := waitForAvailable(ctx, isOpenShift); err != nil {
		t.Fatalf("Config never reported status available: %q", err)
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
// Tests the operator's reconciliation capabilities and resource recovery.
func testResourceDeletion(ctx context.Context, t *testing.T) error {
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
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanMetricsProxyDsName,
				Namespace: internal.BpfmanNamespace,
			},
		},
	}
	// Add OpenShift SCC deletion if on OpenShift.
	if isOpenShift {
		objects = append(
			objects,
			&osv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{
					Name: internal.BpfmanRestrictedSccName,
				},
			},
		)
	}
	for _, obj := range objects {
		if err := runtimeClient.Delete(ctx, obj); err != nil {
			return fmt.Errorf("could not delete obj %v, err: %w", obj, err)
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

	// Test metrics proxy DaemonSet modification.
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
				t.Logf("Checking that standard metrics ds was correctly reconciled")
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
	if configMap.Data["bpfman.log.level"] != newConfig.Spec.LogLevel {
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

// waitForResourceCreation polls for all required bpfman resources to be created and ready.
// Checks for CSI driver, ConfigMap, and both DaemonSets (bpfman and metrics proxy).
// Returns error if timeout is reached or if context is cancelled.
func waitForResourceCreation(ctx context.Context) error {
	return waitUntilCondition(ctx, func() (bool, error) {
		// Check CSI driver exists.
		csiDriver := &storagev1.CSIDriver{}
		if err := runtimeClient.Get(ctx, types.NamespacedName{Name: internal.BpfmanCsiDriverName}, csiDriver); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		// Check ConfigMap exists.
		configMap := &corev1.ConfigMap{}
		cmKey := types.NamespacedName{Name: internal.BpfmanConfigName, Namespace: internal.BpfmanNamespace}
		if err := runtimeClient.Get(ctx, cmKey, configMap); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		// Check DaemonSet is ready.
		ds := &appsv1.DaemonSet{}
		dsKey := types.NamespacedName{Name: internal.BpfmanDsName, Namespace: internal.BpfmanNamespace}
		if err := runtimeClient.Get(ctx, dsKey, ds); err != nil && errors.IsNotFound(err) || ds.Status.NumberAvailable == 0 {
			return false, nil
		} else if err != nil {
			return false, err
		}
		// Check Metrics Proxy DaemonSet is ready.
		mds := &appsv1.DaemonSet{}
		mdsKey := types.NamespacedName{Name: internal.BpfmanMetricsProxyDsName, Namespace: internal.BpfmanNamespace}
		if err := runtimeClient.Get(ctx, mdsKey, mds); err != nil && errors.IsNotFound(err) || mds.Status.NumberAvailable == 0 {
			return false, nil
		} else if err != nil {
			return false, err
		}
		// Check the SCC exists.
		if isOpenShift {
			scc := &osv1.SecurityContextConstraints{}
			sccKey := types.NamespacedName{Name: internal.BpfmanRestrictedSccName}
			if err := runtimeClient.Get(ctx, sccKey, scc); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
		}
		return true, nil
	})
}

// waitForResourceDeletion polls for all bpfman managed resources to be deleted.
// Checks that CSI driver, ConfigMap, and both DaemonSets are no longer present.
// Returns error if timeout is reached or if context is cancelled.
func waitForResourceDeletion(ctx context.Context, t *testing.T) error {
	return waitUntilCondition(ctx, func() (bool, error) {
		// Check CSI driver.
		csiDriver := &storagev1.CSIDriver{}
		if err := runtimeClient.Get(ctx, types.NamespacedName{Name: internal.BpfmanCsiDriverName}, csiDriver); err == nil {
			t.Logf("csi driver not yet deleted, %v", *csiDriver)
			return false, nil
		} else if !errors.IsNotFound(err) {
			return false, err
		}
		// Check ConfigMap.
		configMap := &corev1.ConfigMap{}
		cmKey := types.NamespacedName{Name: internal.BpfmanConfigName, Namespace: internal.BpfmanNamespace}
		if err := runtimeClient.Get(ctx, cmKey, configMap); err == nil {
			t.Logf("config map not yet deleted, %v", *configMap)
			return false, nil
		} else if !errors.IsNotFound(err) {
			return false, err
		}
		// Check DaemonSet.
		ds := &appsv1.DaemonSet{}
		dsKey := types.NamespacedName{Name: internal.BpfmanDsName, Namespace: internal.BpfmanNamespace}
		if err := runtimeClient.Get(ctx, dsKey, ds); err == nil {
			t.Logf("daemonset not yet deleted, %v", *ds)
			return false, nil
		} else if !errors.IsNotFound(err) {
			return false, err
		}

		// Check Metrics Proxy DaemonSet.
		mds := &appsv1.DaemonSet{}
		mdsKey := types.NamespacedName{Name: internal.BpfmanMetricsProxyDsName, Namespace: internal.BpfmanNamespace}
		if err := runtimeClient.Get(ctx, mdsKey, mds); err == nil {
			t.Logf("daemonset not yet deleted, %v", *mds)
			return false, nil
		} else if !errors.IsNotFound(err) {
			return false, err
		}
		// Check OpenShift SCC if on OpenShift.
		if isOpenShift {
			scc := &osv1.SecurityContextConstraints{}
			sccKey := types.NamespacedName{Name: internal.BpfmanRestrictedSccName}
			if err := runtimeClient.Get(ctx, sccKey, scc); err == nil {
				t.Logf("scc not yet deleted, %v", *scc)
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
			return fmt.Errorf("timeout or context cancelled waiting for resource deletion: %w", ctx.Err())
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

// podHasContainerArg checks if a DaemonSet has the given argument in any of its containers.
func podHasContainerArg(ds *appsv1.DaemonSet, arg string) bool {
	for _, container := range ds.Spec.Template.Spec.Containers {
		for _, containerArg := range container.Args {
			if containerArg == arg {
				return true
			}
		}
	}
	return false
}

// waitForAvailable waits until the bpfman Config shows "Available" status conditions,
// indicating that all components are ready and reconciliation is complete.
func waitForAvailable(ctx context.Context, isOpenShift bool) error {
	return waitUntilCondition(ctx, func() (bool, error) {
		bpfmanConfig := &v1alpha1.Config{}
		if err := runtimeClient.Get(ctx, types.NamespacedName{Name: internal.BpfmanConfigName}, bpfmanConfig); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		progressingCondition := metav1.Condition{
			Type:    internal.ConfigConditionProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  internal.ConfigReasonAvailable,
			Message: internal.ConfigMessageAvailable,
		}
		availableCondition := metav1.Condition{
			Type:    internal.ConfigConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  internal.ConfigReasonAvailable,
			Message: internal.ConfigMessageAvailable,
		}
		if !testutil.ContainsCondition(bpfmanConfig.Status.Conditions, progressingCondition) {
			return false, nil
		}
		if !testutil.ContainsCondition(bpfmanConfig.Status.Conditions, availableCondition) {
			return false, nil
		}
		componentsReady := bpfmanConfig.Status.Components != nil &&
			internal.CCSAllComponentsReady(bpfmanConfig.Status.Components, isOpenShift)
		return componentsReady, nil
	})
}
