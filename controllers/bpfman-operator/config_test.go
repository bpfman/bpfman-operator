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

package bpfmanoperator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	osv1 "github.com/openshift/api/security/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
)

const (
	logLevel = "FAKE"
)

type clusterObjects struct {
	cm           *corev1.ConfigMap
	csiDriver    *storagev1.CSIDriver
	ds           *appsv1.DaemonSet
	metricsDs    *appsv1.DaemonSet
	scc          *osv1.SecurityContextConstraints
	agentSM      *monitoringv1.ServiceMonitor
	controllerSM *monitoringv1.ServiceMonitor
}

// TestReconcile tests the BpfmanConfigReconciler's ability to create, update, and restore
// Kubernetes resources (ConfigMap, CSIDriver, DaemonSets, and OpenShift SCC).
// It verifies proper owner reference setup for cascading deletion - the Kubernetes API
// handles actual cascading deletion, so testing for correct owner reference annotations
// is sufficient in unit tests.
func TestReconcile(t *testing.T) {
	for _, tc := range []struct {
		name          string
		isOpenShift   bool
		hasMonitoring bool
	}{
		{name: "plain-k8s", isOpenShift: false, hasMonitoring: false},
		{name: "k8s-with-monitoring", isOpenShift: false, hasMonitoring: true},
		{name: "openshift", isOpenShift: true, hasMonitoring: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("Setting up test environment")
			r, bpfmanConfig, req, ctx, cl := setupTestEnvironment(tc.isOpenShift, tc.hasMonitoring)

			t.Log("Checking the restricted SCC name")
			require.Equal(t, tc.isOpenShift, r.RestrictedSCC != "",
				"RestrictedSCC should be non-empty for OpenShift and empty otherwise")

			t.Log("Getting bpfman config")
			err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanConfigName}, bpfmanConfig)
			require.NoError(t, err)

			t.Log("Running initial reconcile (adds finalizer)")
			_, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("initial reconcile failed: %v", err)
			}

			t.Log("Running second reconcile (creates resources)")
			_, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("second reconcile failed: %v", err)
			}

			t.Log("Making sure that all objects are present")
			err = testAllObjectsPresent(ctx, cl, bpfmanConfig, tc.isOpenShift, tc.hasMonitoring)
			if err != nil {
				t.Fatalf("not all objects present after initial reconcile: %q", err)
			}

			// Delete objects one by one to test restoration.
			objects := map[string]client.Object{
				"configmap": &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanCmName,
						Namespace: internal.BpfmanNamespace,
					},
				},
				"csi-driver": &storagev1.CSIDriver{
					ObjectMeta: metav1.ObjectMeta{
						Name: internal.BpfmanCsiDriverName,
					},
				},
				"bpfman-daemonset": &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanDsName,
						Namespace: internal.BpfmanNamespace,
					},
				},
			}
			if tc.isOpenShift {
				objects["restricted-scc"] = &osv1.SecurityContextConstraints{
					ObjectMeta: metav1.ObjectMeta{
						Name: internal.BpfmanRestrictedSccName,
					},
				}
			}
			if tc.hasMonitoring {
				objects["metrics-daemonset"] = &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanMetricsProxyDsName,
						Namespace: internal.BpfmanNamespace,
					},
				}
				objects["agent-service-monitor"] = &monitoringv1.ServiceMonitor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanAgentServiceMonitorName,
						Namespace: internal.BpfmanNamespace,
					},
				}
				objects["controller-service-monitor"] = &monitoringv1.ServiceMonitor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanControllerServiceMonitorName,
						Namespace: internal.BpfmanNamespace,
					},
				}
			}
			for desc, obj := range objects {
				t.Logf("Deleting the %s", desc)
				err = cl.Delete(ctx, obj)
				require.NoError(t, err)

				t.Log("Running reconcile")
				_, err = r.Reconcile(ctx, req)
				if err != nil {
					t.Fatalf("reconcile failed after deleting %s: %v", desc, err)
				}

				t.Log("Making sure that all objects are present")
				err = testAllObjectsPresent(ctx, cl, bpfmanConfig, tc.isOpenShift, tc.hasMonitoring)
				if err != nil {
					t.Fatalf("objects not properly restored after deleting %s: %v", desc, err)
				}
			}

			t.Log("Making invalid changes to objects")
			if _, err := modifyObjects(ctx, cl, tc.isOpenShift, tc.hasMonitoring); err != nil {
				t.Fatalf("failed to modify objects for restoration test: %v", err)
			}

			t.Log("Running reconcile")
			_, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("reconcile failed after modifying objects: %v", err)
			}

			t.Log("Making sure that all objects are present")
			err = testAllObjectsPresent(ctx, cl, bpfmanConfig, tc.isOpenShift, tc.hasMonitoring)
			if err != nil {
				t.Fatalf("objects not properly restored after modification: %v", err)
			}

			t.Log("Setting overrides")
			if err := setOverrides(ctx, cl); err != nil {
				t.Fatalf("failed to modify objects for restoration test: %v", err)
			}

			t.Log("Making invalid changes to objects")
			cos, err := modifyObjects(ctx, cl, tc.isOpenShift, tc.hasMonitoring)
			if err != nil {
				t.Fatalf("failed to modify objects for restoration test: %v", err)
			}

			t.Log("Running reconcile - should not change anything (overrides in place)")
			_, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("reconcile failed after modifying objects: %v", err)
			}

			t.Log("Making sure that all objects are unchanged")
			err = testObjectsUnchanged(ctx, cl, tc.isOpenShift, tc.hasMonitoring, cos)
			if err != nil {
				t.Fatalf("objects not as expected (should be unchanged) after reconcile: %v", err)
			}
		})
	}
}

// setupTestEnvironment initializes the test environment for BpfmanConfigReconciler testing.
// Creates a fake Kubernetes client with a test Config resource, registers all necessary
// types with the runtime scheme, and configures the reconciler with proper manifest paths.
// For OpenShift environments, it sets up the RestrictedSCC field for security context testing.
//
// Parameters:
//   - isOpenShift: Flag to enable OpenShift-specific configuration (SCC setup)
//
// Returns:
//   - *BpfmanConfigReconciler: Configured reconciler with fake client
//   - *v1alpha1.Config: Test bpfman configuration object
//   - reconcile.Request: Mock reconcile request for testing
//   - context.Context: Test context
//   - client.Client: Fake Kubernetes client for API interactions
func setupTestEnvironment(isOpenShift, hasMonitoring bool) (*BpfmanConfigReconciler, *v1alpha1.Config, reconcile.Request,
	context.Context, client.Client) {
	// A configMap for bpfman with metadata and spec.
	bpfmanConfig := &v1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanConfigName,
		},
		Spec: v1alpha1.ConfigSpec{
			Agent: v1alpha1.AgentSpec{
				Image:           "BPFMAN_AGENT_IS_SCARY",
				LogLevel:        logLevel,
				HealthProbePort: 8175,
			},
			Daemon: v1alpha1.DaemonSpec{
				Image:    "FAKE-IMAGE",
				LogLevel: logLevel,
			},
			Namespace: "bpfman",
		},
	}
	bpfmanConfig.Kind = "Config"
	bpfmanConfig.APIVersion = "bpfman.io/v1alpha1"

	bpfmanNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bpfman",
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{bpfmanConfig, bpfmanNamespace}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.Config{})
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ConfigMap{})
	s.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DaemonSet{})
	s.AddKnownTypes(storagev1.SchemeGroupVersion, &storagev1.CSIDriver{})
	s.AddKnownTypes(osv1.GroupVersion, &osv1.SecurityContextConstraints{})
	s.AddKnownTypes(monitoringv1.SchemeGroupVersion, &monitoringv1.ServiceMonitor{}, &monitoringv1.ServiceMonitorList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	rc := ReconcilerCommon[v1alpha1.ClusterBpfApplicationState, v1alpha1.ClusterBpfApplicationStateList]{
		Client: cl,
		Scheme: s,
	}

	cpr := ClusterApplicationReconciler{
		ReconcilerCommon: rc,
	}

	// Create a BpfmanConfigReconciler object with the scheme and
	// fake client.
	r := &BpfmanConfigReconciler{
		ClusterApplicationReconciler: cpr,
		BpfmanStandardDS:             resolveConfigPath(internal.BpfmanDaemonManifestPath),
		BpfmanMetricsProxyDS:         resolveConfigPath(internal.BpfmanMetricsProxyPath),
		CsiDriverDS:                  resolveConfigPath(internal.BpfmanCsiDriverPath),
		IsOpenshift:                  isOpenShift,
		HasMonitoring:                hasMonitoring,
	}
	if isOpenShift {
		r.RestrictedSCC = resolveConfigPath(internal.BpfmanRestrictedSCCPath)
	}

	// Mock request object to simulate Reconcile() being called on
	// an event for a watched resource.
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      internal.BpfmanCmName,
			Namespace: internal.BpfmanNamespace,
		},
	}

	return r, bpfmanConfig, req, context.TODO(), cl
}

// resolveConfigPath adjusts the path for configuration files so that
// lookup of path resolves from the perspective of the calling test.
func resolveConfigPath(path string) string {
	testDir, _ := os.Getwd()
	return filepath.Clean(filepath.Join(testDir, getRelativePathToRoot(testDir), path))
}

// getRelativePathToRoot calculates the relative path from the current
// directory to the project root.
func getRelativePathToRoot(currentDir string) string {
	projectRoot := mustFindProjectRoot(currentDir, projectRootPredicate)
	relPath, _ := filepath.Rel(currentDir, projectRoot)
	return relPath
}

// mustFindProjectRoot traverses up the directory tree to find the
// project root. The project root is identified by the provided
// closure. This function panics if, after walking the tree and
// reaching the root of the filesystem, the project root is not found.
func mustFindProjectRoot(currentDir string, isProjectRoot func(string) bool) string {
	for {
		if isProjectRoot(currentDir) {
			return currentDir
		}
		if currentDir == filepath.Dir(currentDir) {
			panic("project root not found")
		}
		// Move up to the parent directory.
		currentDir = filepath.Dir(currentDir)
	}
}

// projectRootPredicate checks if the given directory is the project
// root by looking for the presence of a `go.mod` file and a `config`
// directory. Returns true if both are found, otherwise returns false.
func projectRootPredicate(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "config")); os.IsNotExist(err) {
		return false
	}
	return true
}

// hasOwnerReference validates that a Kubernetes object has the correct owner reference
// to the bpfman Config, with proper controller and blocking deletion settings.
func hasOwnerReference(config *v1alpha1.Config, o client.Object) error {
	name := o.GetName()
	or := o.GetOwnerReferences()
	if len(or) != 1 {
		return fmt.Errorf("owner ref of %q has %d OwnerReferences, expected exactly 1", name, len(or))
	}
	if or[0].BlockOwnerDeletion == nil || *or[0].BlockOwnerDeletion != true {
		return fmt.Errorf("owner ref of %q has BlockOwnerDeletion=%v, expected true", name, or[0].BlockOwnerDeletion)
	}
	if or[0].Controller == nil || *or[0].Controller != true {
		return fmt.Errorf("owner ref of %q has Controller=%v, expected true", name, or[0].Controller)
	}
	expectedGVK := v1alpha1.SchemeGroupVersion.WithKind("Config")
	if or[0].APIVersion != expectedGVK.GroupVersion().String() || or[0].Kind != expectedGVK.Kind {
		return fmt.Errorf("owner ref of %q has APIVersion=%q Kind=%q, expected APIVersion=%q Kind=%q",
			name, or[0].APIVersion, or[0].Kind, expectedGVK.GroupVersion().String(), expectedGVK.Kind)
	}
	if or[0].Name != internal.BpfmanConfigName {
		return fmt.Errorf("owner ref of %q has OwnerReference Name=%q, expected %q",
			name, or[0].Name, internal.BpfmanConfigName)
	}

	return nil
}

// testAllObjectsPresent verifies that all expected Kubernetes resources are present and correctly configured
// after a reconcile operation. Validates ConfigMap, CSIDriver, DaemonSets, and OpenShift SCC if applicable.
func testAllObjectsPresent(ctx context.Context, cl client.Client, bpfmanConfig *v1alpha1.Config,
	isOpenShift, hasMonitoring bool) error {
	// Expected configmap.
	bpfmanCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanCmName,
			Namespace: internal.BpfmanNamespace,
		},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: bpfmanCM.Name, Namespace: bpfmanCM.Namespace},
		bpfmanCM); err != nil {
		return err
	}
	if err := hasOwnerReference(bpfmanConfig, bpfmanCM); err != nil {
		return err
	}
	requiredFields := map[string]*string{
		internal.BpfmanTOML:          nil,
		internal.BpfmanAgentLogLevel: ptr.To(logLevel),
		internal.BpfmanLogLevel:      ptr.To(logLevel),
	}
	if err := verifyCM(bpfmanCM, requiredFields); err != nil {
		return err
	}

	// Check the CSI driver was created successfully.
	expectedCsiDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanCsiDriverName,
		},
	}
	actualCsiDriver := &storagev1.CSIDriver{}
	if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanCsiDriverName},
		actualCsiDriver); err != nil {
		return err
	}
	expectedCsiDriver, err := load(expectedCsiDriver, resolveConfigPath(internal.BpfmanCsiDriverPath),
		expectedCsiDriver.Name)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actualCsiDriver.Spec, expectedCsiDriver.Spec) {
		return fmt.Errorf("deep equal failed, actualCsiDriver.Spec: %+v, expectedCsiDriver.Spec: %+v",
			actualCsiDriver.Spec, expectedCsiDriver.Spec)
	}
	if err := hasOwnerReference(bpfmanConfig, bpfmanCM); err != nil {
		return err
	}

	// Check the bpfman daemonset was created successfully.
	expectedBpfmanDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanDsName,
			Namespace: internal.BpfmanNamespace,
		},
	}
	actualBpfmanDs := &appsv1.DaemonSet{}
	if err := cl.Get(ctx, types.NamespacedName{Name: expectedBpfmanDs.Name, Namespace: expectedBpfmanDs.Namespace},
		actualBpfmanDs); err != nil {
		return err
	}
	expectedBpfmanDs, err = load(expectedBpfmanDs, resolveConfigPath(internal.BpfmanDaemonManifestPath),
		expectedBpfmanDs.Name)
	if err != nil {
		return err
	}
	configureBpfmanDs(expectedBpfmanDs, bpfmanConfig)
	if !reflect.DeepEqual(actualBpfmanDs.Spec, expectedBpfmanDs.Spec) {
		return fmt.Errorf("deep equal failed, actualBpfmanDs.Spec: %+v, expectedBpfmanDs.Spec: %+v",
			actualBpfmanDs.Spec, expectedBpfmanDs.Spec)
	}
	if err := hasOwnerReference(bpfmanConfig, bpfmanCM); err != nil {
		return err
	}

	if !hasMonitoring {
		// Verify the metrics proxy DaemonSet does NOT exist when monitoring is unavailable.
		absentMetricsDs := &appsv1.DaemonSet{}
		err = cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanMetricsProxyDsName,
			Namespace: internal.BpfmanNamespace,
		}, absentMetricsDs)
		if err == nil {
			return fmt.Errorf("metrics proxy DaemonSet %q should not exist without monitoring",
				internal.BpfmanMetricsProxyDsName)
		}
	}

	if isOpenShift {
		// Check the SCC was created with the correct configuration.
		actualRestrictedSCC := &osv1.SecurityContextConstraints{}
		bpfmanRestrictedSCC := &osv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
		}
		expectedRestrictedSCC, err := load(bpfmanRestrictedSCC, resolveConfigPath(internal.BpfmanRestrictedSCCPath),
			bpfmanRestrictedSCC.Name)
		if err != nil {
			return err
		}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanRestrictedSccName, Namespace: corev1.NamespaceAll},
			actualRestrictedSCC); err != nil {
			return err
		}
		if err := hasOwnerReference(bpfmanConfig, bpfmanCM); err != nil {
			return err
		}
		// Match fields that differ between the loaded manifest and the fake client's stored object.
		expectedRestrictedSCC.ResourceVersion = actualRestrictedSCC.ResourceVersion
		expectedRestrictedSCC.OwnerReferences = actualRestrictedSCC.OwnerReferences
		expectedRestrictedSCC.TypeMeta = actualRestrictedSCC.TypeMeta
		if !reflect.DeepEqual(actualRestrictedSCC, expectedRestrictedSCC) {
			return fmt.Errorf("deep equal failed, actualRestrictedSCC.Spec: %+v, expectedRestrictedSCC.Spec: %+v",
				actualRestrictedSCC, expectedRestrictedSCC)
		}
	}

	if hasMonitoring {
		// Check the metrics proxy daemonset was created successfully.
		expectedMetricsDs := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanMetricsProxyDsName,
				Namespace: internal.BpfmanNamespace,
			},
		}
		actualMetricsDs := &appsv1.DaemonSet{}
		if err := cl.Get(ctx, types.NamespacedName{Name: expectedMetricsDs.Name, Namespace: expectedMetricsDs.Namespace},
			actualMetricsDs); err != nil {
			return err
		}
		expectedMetricsDs, err = load(expectedMetricsDs, resolveConfigPath(internal.BpfmanMetricsProxyPath),
			expectedMetricsDs.Name)
		if err != nil {
			return err
		}
		configureMetricsProxyDs(expectedMetricsDs, bpfmanConfig, isOpenShift)
		if !equality.Semantic.DeepEqual(actualMetricsDs.Spec, expectedMetricsDs.Spec) {
			return fmt.Errorf("deep equal failed, actualMetricsDs.Spec: %+v, expectedMetricsDs.Spec: %+v",
				actualMetricsDs.Spec, expectedMetricsDs.Spec)
		}
		if err := hasOwnerReference(bpfmanConfig, actualMetricsDs); err != nil {
			return err
		}

		// Check the agent ServiceMonitor was created.
		agentSM := &monitoringv1.ServiceMonitor{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanAgentServiceMonitorName,
			Namespace: internal.BpfmanNamespace,
		}, agentSM); err != nil {
			return fmt.Errorf("failed to get agent ServiceMonitor: %w", err)
		}
		if err := hasOwnerReference(bpfmanConfig, agentSM); err != nil {
			return fmt.Errorf("agent ServiceMonitor owner reference: %w", err)
		}
		if len(agentSM.Spec.Endpoints) != 1 {
			return fmt.Errorf("agent ServiceMonitor has %d endpoints, expected 1", len(agentSM.Spec.Endpoints))
		}
		if agentSM.Spec.Endpoints[0].Path != "/agent-metrics" {
			return fmt.Errorf("agent ServiceMonitor endpoint path=%q, expected /agent-metrics",
				agentSM.Spec.Endpoints[0].Path)
		}
		if agentSM.Spec.Endpoints[0].Port != "https-metrics" {
			return fmt.Errorf("agent ServiceMonitor endpoint port=%q, expected https-metrics",
				agentSM.Spec.Endpoints[0].Port)
		}

		// Check the controller ServiceMonitor was created.
		controllerSM := &monitoringv1.ServiceMonitor{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanControllerServiceMonitorName,
			Namespace: internal.BpfmanNamespace,
		}, controllerSM); err != nil {
			return fmt.Errorf("failed to get controller ServiceMonitor: %w", err)
		}
		if err := hasOwnerReference(bpfmanConfig, controllerSM); err != nil {
			return fmt.Errorf("controller ServiceMonitor owner reference: %w", err)
		}
		if len(controllerSM.Spec.Endpoints) != 1 {
			return fmt.Errorf("controller ServiceMonitor has %d endpoints, expected 1",
				len(controllerSM.Spec.Endpoints))
		}
		if controllerSM.Spec.Endpoints[0].Path != "/metrics" {
			return fmt.Errorf("controller ServiceMonitor endpoint path=%q, expected /metrics",
				controllerSM.Spec.Endpoints[0].Path)
		}
	}
	return nil
}

// modifyObjects intentionally corrupts various Kubernetes objects with invalid data
// to test that the reconciler properly restores them to the expected state (or not in the case of overrides).
// Modifies ConfigMap data, DaemonSet security context, container images, and OpenShift SCC settings.
// Returns the modified objects.
func modifyObjects(ctx context.Context, cl client.Client, isOpenShift, hasMonitoring bool) (clusterObjects, error) {
	var co clusterObjects

	// ConfigMap.
	bpfmanCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanCmName,
			Namespace: internal.BpfmanNamespace,
		},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: bpfmanCM.Name, Namespace: bpfmanCM.Namespace},
		bpfmanCM); err != nil {
		return co, err
	}
	bpfmanCM.Data = map[string]string{
		"this was": "all deleted",
	}
	if err := cl.Update(ctx, bpfmanCM); err != nil {
		return co, err
	}
	co.cm = bpfmanCM

	// CSI Driver.
	csiDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanCsiDriverName,
		},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanCsiDriverName}, csiDriver); err != nil {
		return co, err
	}
	if csiDriver.Spec.AttachRequired == nil || !*csiDriver.Spec.AttachRequired {
		csiDriver.Spec.AttachRequired = ptr.To(true)
	} else {
		csiDriver.Spec.AttachRequired = ptr.To(false)
	}
	if err := cl.Update(ctx, csiDriver); err != nil {
		return co, err
	}
	co.csiDriver = csiDriver

	// Bpfman DaemonSet.
	bpfmanDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanDsName,
			Namespace: internal.BpfmanNamespace,
		},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: bpfmanDs.Name, Namespace: bpfmanDs.Namespace},
		bpfmanDs); err != nil {
		return co, err
	}
	// Set invalid supplemental group ID to test reconciler restoration
	bpfmanDs.Spec.Template.Spec.SecurityContext.SupplementalGroups = []int64{12345}
	if err := cl.Update(ctx, bpfmanDs); err != nil {
		return co, err
	}
	co.ds = bpfmanDs

	if isOpenShift {
		// Check the SCC was created with the correct configuration.
		restrictedSCC := &osv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
		}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanRestrictedSccName, Namespace: corev1.NamespaceAll},
			restrictedSCC); err != nil {
			return co, err
		}
		restrictedSCC.AllowHostPorts = false
		if err := cl.Update(ctx, restrictedSCC); err != nil {
			return co, err
		}
		co.scc = restrictedSCC
	}

	if hasMonitoring {
		// Modify the metrics proxy daemonset for restoration testing.
		metricsDs := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanMetricsProxyDsName,
				Namespace: internal.BpfmanNamespace,
			},
		}
		if err := cl.Get(ctx, types.NamespacedName{Name: metricsDs.Name, Namespace: metricsDs.Namespace},
			metricsDs); err != nil {
			return co, err
		}
		if len(metricsDs.Spec.Template.Spec.Containers) == 0 {
			return co, fmt.Errorf("metrics DaemonSet has no containers configured")
		}
		// Set invalid container image to test reconciler restoration
		metricsDs.Spec.Template.Spec.Containers[0].Image = "invalid image"
		if err := cl.Update(ctx, metricsDs); err != nil {
			return co, err
		}
		co.metricsDs = metricsDs

		// Modify agent ServiceMonitor.
		agentSM := &monitoringv1.ServiceMonitor{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanAgentServiceMonitorName,
			Namespace: internal.BpfmanNamespace,
		}, agentSM); err != nil {
			return co, err
		}
		agentSM.Spec.Endpoints[0].Path = "/invalid-path"
		if err := cl.Update(ctx, agentSM); err != nil {
			return co, err
		}
		co.agentSM = agentSM

		// Modify controller ServiceMonitor.
		controllerSM := &monitoringv1.ServiceMonitor{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanControllerServiceMonitorName,
			Namespace: internal.BpfmanNamespace,
		}, controllerSM); err != nil {
			return co, err
		}
		controllerSM.Spec.Endpoints[0].Path = "/invalid-path"
		if err := cl.Update(ctx, controllerSM); err != nil {
			return co, err
		}
		co.controllerSM = controllerSM
	}
	return co, nil
}

func verifyCM(cm *corev1.ConfigMap, requiredFields map[string]*string) error {
	for f, v := range requiredFields {
		if _, ok := cm.Data[f]; !ok {
			return fmt.Errorf("ConfigMap is missing required field %q", f)
		}
		if v != nil {
			if cm.Data[f] != *v {
				return fmt.Errorf("ConfigMap field %q has value %q but should have %q", f, cm.Data[f], *v)
			}
		}
	}
	return nil
}

// TestConfigureBpfmanDsCsiRegistrarImageOverride verifies that the optional
// CsiRegistrarImage field in the Config CR correctly overrides the CSI node
// driver registrar container image in the DaemonSet.
func TestConfigureBpfmanDsCsiRegistrarImageOverride(t *testing.T) {
	tests := []struct {
		name                      string
		csiRegistrarImage         string
		expectedCsiRegistrarImage string
	}{
		{
			name:                      "default preserved when field empty",
			csiRegistrarImage:         "",
			expectedCsiRegistrarImage: "quay.io/bpfman/csi-node-driver-registrar:v2.13.0",
		},
		{
			name:                      "csi registrar image overridden when set",
			csiRegistrarImage:         "registry.example.com/custom-csi:v1",
			expectedCsiRegistrarImage: "registry.example.com/custom-csi:v1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Load the static DaemonSet from the manifest file.
			ds := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      internal.BpfmanDsName,
					Namespace: internal.BpfmanNamespace,
				},
			}
			ds, err := load(ds, resolveConfigPath(internal.BpfmanDaemonManifestPath), ds.Name)
			require.NoError(t, err)

			// Create a Config with the test case values.
			config := &v1alpha1.Config{
				Spec: v1alpha1.ConfigSpec{
					Agent: v1alpha1.AgentSpec{
						Image:           "quay.io/bpfman/bpfman-agent:latest",
						LogLevel:        "info",
						HealthProbePort: 8175,
					},
					Daemon: v1alpha1.DaemonSpec{
						Image:             "quay.io/bpfman/bpfman:latest",
						LogLevel:          "info",
						CsiRegistrarImage: tc.csiRegistrarImage,
					},
				},
			}

			// Apply configuration to the DaemonSet.
			configureBpfmanDs(ds, config)

			// Verify CSI registrar container image.
			var csiImage string
			for _, c := range ds.Spec.Template.Spec.Containers {
				if c.Name == internal.BpfmanCsiDriverRegistrarName {
					csiImage = c.Image
					break
				}
			}
			require.Equal(t, tc.expectedCsiRegistrarImage, csiImage,
				"CSI registrar container image mismatch")
		})
	}
}

func setOverrides(ctx context.Context, cl client.Client) error {
	bpfmanConfig := &v1alpha1.Config{}
	if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanConfigName}, bpfmanConfig); err != nil {
		return err
	}

	bpfmanConfig.Spec.Overrides = []v1alpha1.ComponentOverride{
		{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Namespace:  "bpfman",
			Name:       "bpfman-config",
			Unmanaged:  true,
		},
		{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
			Namespace:  "bpfman",
			Name:       "bpfman-daemon",
			Unmanaged:  true,
		},
		{
			APIVersion: "apps/v1",
			Kind:       "DaemonSet",
			Namespace:  "bpfman",
			Name:       "bpfman-metrics-proxy",
			Unmanaged:  true,
		},
		{
			APIVersion: "storage.k8s.io/v1",
			Kind:       "CSIDriver",
			Namespace:  "",
			Name:       "csi.bpfman.io",
			Unmanaged:  true,
		},
		{
			APIVersion: "security.openshift.io/v1",
			Kind:       "SecurityContextConstraints",
			Namespace:  "",
			Name:       "bpfman-restricted",
			Unmanaged:  true,
		},
		{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "ServiceMonitor",
			Namespace:  "bpfman",
			Name:       internal.BpfmanAgentServiceMonitorName,
			Unmanaged:  true,
		},
		{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "ServiceMonitor",
			Namespace:  "bpfman",
			Name:       internal.BpfmanControllerServiceMonitorName,
			Unmanaged:  true,
		},
	}

	if err := cl.Update(ctx, bpfmanConfig); err != nil {
		return err
	}
	return nil
}

// testObjectsUnchanged verifies that objects marked as unmanaged in overrides
// remain unchanged after reconciliation by comparing their current state
// against their previously captured state.
func testObjectsUnchanged(ctx context.Context, cl client.Client, isOpenShift, hasMonitoring bool, cos clusterObjects) error {
	bpfmanCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanCmName,
			Namespace: internal.BpfmanNamespace,
		},
		Data: map[string]string{
			"this was": "all deleted",
		},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: bpfmanCM.Name, Namespace: bpfmanCM.Namespace},
		bpfmanCM); err != nil {
		return err
	}
	if !reflect.DeepEqual(bpfmanCM, cos.cm) {
		return fmt.Errorf("deep equal failed for CM, got: %+v, expected: %+v", bpfmanCM, cos.cm)
	}

	// CSI Driver.
	csiDriver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanCsiDriverName,
		},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanCsiDriverName}, csiDriver); err != nil {
		return err
	}
	if !reflect.DeepEqual(csiDriver, cos.csiDriver) {
		return fmt.Errorf("deep equal failed for CSI Driver, got: %+v, expected: %+v", csiDriver, cos.csiDriver)
	}

	// Bpfman DaemonSet.
	bpfmanDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanDsName,
			Namespace: internal.BpfmanNamespace,
		},
	}
	if err := cl.Get(ctx, types.NamespacedName{Name: bpfmanDs.Name, Namespace: bpfmanDs.Namespace},
		bpfmanDs); err != nil {
		return err
	}
	if !reflect.DeepEqual(bpfmanDs, cos.ds) {
		return fmt.Errorf("deep equal failed for Bpfman DS, got: %+v, expected: %+v", bpfmanDs, cos.ds)
	}

	if isOpenShift {
		// SCC.
		restrictedSCC := &osv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
		}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanRestrictedSccName, Namespace: corev1.NamespaceAll},
			restrictedSCC); err != nil {
			return err
		}
		if !reflect.DeepEqual(restrictedSCC, cos.scc) {
			return fmt.Errorf("deep equal failed for SCC, got: %+v, expected: %+v", restrictedSCC, cos.scc)
		}
	}

	if hasMonitoring {
		// Metrics proxy DaemonSet.
		metricsDs := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      internal.BpfmanMetricsProxyDsName,
				Namespace: internal.BpfmanNamespace,
			},
		}
		if err := cl.Get(ctx, types.NamespacedName{Name: metricsDs.Name, Namespace: metricsDs.Namespace},
			metricsDs); err != nil {
			return err
		}
		if !reflect.DeepEqual(metricsDs, cos.metricsDs) {
			return fmt.Errorf("deep equal failed for Metrics DS, got: %+v, expected: %+v", metricsDs, cos.metricsDs)
		}

		// Agent ServiceMonitor.
		agentSM := &monitoringv1.ServiceMonitor{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanAgentServiceMonitorName,
			Namespace: internal.BpfmanNamespace,
		}, agentSM); err != nil {
			return err
		}
		if !reflect.DeepEqual(agentSM, cos.agentSM) {
			return fmt.Errorf("deep equal failed for agent ServiceMonitor, got: %+v, expected: %+v",
				agentSM, cos.agentSM)
		}

		// Controller ServiceMonitor.
		controllerSM := &monitoringv1.ServiceMonitor{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanControllerServiceMonitorName,
			Namespace: internal.BpfmanNamespace,
		}, controllerSM); err != nil {
			return err
		}
		if !reflect.DeepEqual(controllerSM, cos.controllerSM) {
			return fmt.Errorf("deep equal failed for controller ServiceMonitor, got: %+v, expected: %+v",
				controllerSM, cos.controllerSM)
		}
	}

	return nil
}
