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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	osv1 "github.com/openshift/api/security/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
	"github.com/bpfman/bpfman-operator/test/testutil"
)

const (
	logLevel = "FAKE"
)

// TestReconcile tests the BpfmanConfigReconciler's ability to create, update, and restore
// Kubernetes resources (ConfigMap, CSIDriver, DaemonSets, and OpenShift SCC).
// It verifies proper owner reference setup for cascading deletion - the Kubernetes API
// handles actual cascading deletion, so testing for correct owner reference annotations
// is sufficient in unit tests.
func TestReconcile(t *testing.T) {
	for _, tc := range []struct {
		isOpenShift bool
	}{
		{isOpenShift: false},
		{isOpenShift: true},
	} {
		t.Run(fmt.Sprintf("isOpenShift: %v", tc.isOpenShift), func(t *testing.T) {
			t.Log("Setting up test environment")
			fakeRecorder := record.NewFakeRecorder(100)
			r, bpfmanConfig, req, ctx, cl := setupTestEnvironment(tc.isOpenShift, fakeRecorder)

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

			t.Log("Checking status (should be unknown)")
			err = testStatusSet(ctx, cl, internal.BpfmanConfigName, tc.isOpenShift, "unknown")
			if err != nil {
				t.Fatalf("unexpected status on config %q: %q", internal.BpfmanConfigName, err)
			}

			t.Log("Running second reconcile (creates resources)")
			_, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("second reconcile failed: %v", err)
			}

			t.Log("Making sure that all objects are present")
			err = testAllObjectsPresent(ctx, cl, bpfmanConfig, tc.isOpenShift)
			if err != nil {
				t.Fatalf("not all objects present after initial reconcile: %q", err)
			}

			t.Log("Checking status (should be progressing)")
			err = testStatusSet(ctx, cl, internal.BpfmanConfigName, tc.isOpenShift, "progressing")
			if err != nil {
				t.Fatalf("unexpected status on config %q: %q", internal.BpfmanConfigName, err)
			}

			t.Log("Marking DaemonSets as ready")
			err = testReconcileDaemonSets(ctx, cl)
			if err != nil {
				t.Fatalf("couldn't reconcile DaemonSets, %q", err)
			}

			t.Log("Running third reconcile (flips status to available)")
			_, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("second reconcile failed: %v", err)
			}

			t.Log("Checking status (should be available)")
			err = testStatusSet(ctx, cl, internal.BpfmanConfigName, tc.isOpenShift, "available")
			if err != nil {
				t.Fatalf("unexpected status on config %q: %q", internal.BpfmanConfigName, err)
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
				"metrics-daemonset": &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanMetricsProxyDsName,
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
				err = testAllObjectsPresent(ctx, cl, bpfmanConfig, tc.isOpenShift)
				if err != nil {
					t.Fatalf("objects not properly restored after deleting %s: %v", desc, err)
				}
			}

			t.Log("Making invalid changes to objects")
			if err := modifyObjects(ctx, cl, tc.isOpenShift); err != nil {
				t.Fatalf("failed to modify objects for restoration test: %v", err)
			}

			t.Log("Running reconcile")
			_, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("reconcile failed after modifying objects: %v", err)
			}

			t.Log("Making sure that all objects are present")
			err = testAllObjectsPresent(ctx, cl, bpfmanConfig, tc.isOpenShift)
			if err != nil {
				t.Fatalf("objects not properly restored after modification: %v", err)
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
func setupTestEnvironment(isOpenShift bool, recorder record.EventRecorder) (*BpfmanConfigReconciler, *v1alpha1.Config, reconcile.Request,
	context.Context, client.Client) {
	// A configMap for bpfman with metadata and spec.
	bpfmanConfig := &v1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: internal.BpfmanConfigName,
		},
		Spec: v1alpha1.ConfigSpec{
			Image: "FAKE-IMAGE",
			Agent: v1alpha1.AgentSpec{
				Image:           "BPFMAN_AGENT_IS_SCARY",
				LogLevel:        logLevel,
				HealthProbePort: 8175,
			},
			LogLevel:  logLevel,
			Namespace: "bpfman",
		},
	}
	bpfmanConfig.Kind = "Config"
	bpfmanConfig.APIVersion = "bpfman.io/v1alpha1"

	// Objects to track in the fake client.
	objs := []runtime.Object{bpfmanConfig}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(corev1.SchemeGroupVersion, &v1alpha1.Config{})
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ConfigMap{})
	s.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DaemonSet{})
	s.AddKnownTypes(storagev1.SchemeGroupVersion, &storagev1.CSIDriver{})
	s.AddKnownTypes(osv1.GroupVersion, &osv1.SecurityContextConstraints{})

	// Create a fake client to mock API calls.
	// For WithStatusSubresource see:
	// * https://github.com/kubernetes-sigs/controller-runtime/issues/2362#issuecomment-1837270195
	// * https://stackoverflow.com/questions/77489441/go-k8s-controller-runtime-upgrade-fake-client-lacks-functionality
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).WithStatusSubresource(bpfmanConfig).Build()

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
		Recorder:                     recorder,
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
	// The APIVersion is set to v1 by the fake client, so ignore it.
	if or[0].APIVersion != "v1" || or[0].Kind != config.Kind {
		return fmt.Errorf("owner ref of %q has APIVersion=%q Kind=%q, expected APIVersion=%q Kind=%q",
			name, or[0].APIVersion, or[0].Kind, config.APIVersion, config.Kind)
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
	isOpenShift bool) error {
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
	// Workaround: DeepEqual fails for annotations in non-OpenShift cases due to nil vs empty map differences
	expectedAnnot := expectedMetricsDs.Spec.Template.ObjectMeta.Annotations
	actualAnnot := actualMetricsDs.Spec.Template.ObjectMeta.Annotations
	if len(expectedAnnot) != len(actualAnnot) {
		return fmt.Errorf("expected annotations != actual annotations, %q != %q", expectedAnnot, actualAnnot)
	}
	for k, v := range expectedAnnot {
		if actualAnnot[k] != v {
			return fmt.Errorf("expected annotation %q=%q, got %q", k, v, actualAnnot[k])
		}
	}
	expectedMetricsDs.Spec.Template.ObjectMeta.Annotations = actualMetricsDs.Spec.Template.ObjectMeta.Annotations
	// End workaround
	if !reflect.DeepEqual(actualMetricsDs.Spec, expectedMetricsDs.Spec) {
		return fmt.Errorf("deep equal failed, actualMetricsDs.Spec: %+v, expectedMetricsDs.Spec: %+v",
			actualMetricsDs.Spec, expectedMetricsDs.Spec)
	}
	if err := hasOwnerReference(bpfmanConfig, bpfmanCM); err != nil {
		return err
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
		// Match ResourceVersion and OwnerReferences as those are the only expected differences.
		expectedRestrictedSCC.ResourceVersion = actualRestrictedSCC.ResourceVersion
		expectedRestrictedSCC.OwnerReferences = actualRestrictedSCC.OwnerReferences
		if !reflect.DeepEqual(actualRestrictedSCC, expectedRestrictedSCC) {
			return fmt.Errorf("deep equal failed, actualRestrictedSCC.Spec: %+v, expectedRestrictedSCC.Spec: %+v",
				actualRestrictedSCC, expectedRestrictedSCC)
		}
	}
	return nil
}

// modifyObjects intentionally corrupts various Kubernetes objects with invalid data
// to test that the reconciler properly restores them to the expected state.
// Modifies ConfigMap data, DaemonSet security context, container images, and OpenShift SCC settings.
func modifyObjects(ctx context.Context, cl client.Client, isOpenShift bool) error {
	// ConfigMap.
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
	bpfmanCM.Data = map[string]string{
		"this was": "all deleted",
	}
	if err := cl.Update(ctx, bpfmanCM); err != nil {
		return err
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
	if csiDriver.Spec.AttachRequired == nil || !*csiDriver.Spec.AttachRequired {
		csiDriver.Spec.AttachRequired = ptr.To(true)
	} else {
		csiDriver.Spec.AttachRequired = ptr.To(false)
	}
	if err := cl.Update(ctx, csiDriver); err != nil {
		return err
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
	// Set invalid supplemental group ID to test reconciler restoration
	bpfmanDs.Spec.Template.Spec.SecurityContext.SupplementalGroups = []int64{12345}
	if err := cl.Update(ctx, bpfmanDs); err != nil {
		return err
	}

	// Modify the metrics proxy daemonset for restoration testing.
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
	if len(metricsDs.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("metrics DaemonSet has no containers configured")
	}
	// Set invalid container image to test reconciler restoration
	metricsDs.Spec.Template.Spec.Containers[0].Image = "invalid image"
	if err := cl.Update(ctx, metricsDs); err != nil {
		return err
	}

	if isOpenShift {
		// Check the SCC was created with the correct configuration.
		restrictedSCC := &osv1.SecurityContextConstraints{
			ObjectMeta: metav1.ObjectMeta{
				Name: internal.BpfmanRestrictedSccName,
			},
		}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanRestrictedSccName, Namespace: corev1.NamespaceAll},
			restrictedSCC); err != nil {
			return err
		}
		restrictedSCC.AllowHostPorts = false
		if err := cl.Update(ctx, restrictedSCC); err != nil {
			return err
		}
	}
	return nil
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

// testStatusSet validates that the Config's status.conditions and status.componentStatuses
// match the expected state (unknown, progressing, or available) based on the test scenario.
func testStatusSet(ctx context.Context, cl client.Client, configName string, isOpenShift bool, expected string) error {
	bpfmanConfig := &v1alpha1.Config{}
	if err := cl.Get(ctx, types.NamespacedName{Name: configName}, bpfmanConfig); err != nil {
		return err
	}

	var expectedComponentStatuses map[string]v1alpha1.ConfigComponentStatus
	var progressingCondition metav1.Condition
	var availableCondition metav1.Condition

	switch expected {
	case "available":
		expectedComponentStatuses = map[string]v1alpha1.ConfigComponentStatus{
			"ConfigMap":             v1alpha1.ConfigStatusReady,
			"DaemonSet":             v1alpha1.ConfigStatusReady,
			"MetricsProxyDaemonSet": v1alpha1.ConfigStatusReady,
			"CsiDriver":             v1alpha1.ConfigStatusReady,
		}
		if isOpenShift {
			expectedComponentStatuses["Scc"] = v1alpha1.ConfigStatusReady
		}
		progressingCondition = metav1.Condition{
			Type:    internal.ConfigConditionProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  internal.ConfigReasonAvailable,
			Message: internal.ConfigMessageAvailable,
		}
		availableCondition = metav1.Condition{
			Type:    internal.ConfigConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  internal.ConfigReasonAvailable,
			Message: internal.ConfigMessageAvailable,
		}
	case "progressing":
		expectedComponentStatuses = map[string]v1alpha1.ConfigComponentStatus{
			"ConfigMap":             v1alpha1.ConfigStatusReady,
			"DaemonSet":             v1alpha1.ConfigStatusProgressing,
			"MetricsProxyDaemonSet": v1alpha1.ConfigStatusProgressing,
			"CsiDriver":             v1alpha1.ConfigStatusReady,
		}
		if isOpenShift {
			expectedComponentStatuses["Scc"] = v1alpha1.ConfigStatusReady
		}
		progressingCondition = metav1.Condition{
			Type:    internal.ConfigConditionProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  internal.ConfigReasonProgressing,
			Message: internal.ConfigMessageProgressing,
		}
		availableCondition = metav1.Condition{
			Type:    internal.ConfigConditionAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  internal.ConfigReasonProgressing,
			Message: internal.ConfigMessageProgressing,
		}
	default:
		expectedComponentStatuses = map[string]v1alpha1.ConfigComponentStatus{
			"ConfigMap":             v1alpha1.ConfigStatusUnknown,
			"DaemonSet":             v1alpha1.ConfigStatusUnknown,
			"MetricsProxyDaemonSet": v1alpha1.ConfigStatusUnknown,
			"CsiDriver":             v1alpha1.ConfigStatusUnknown,
		}
		if isOpenShift {
			expectedComponentStatuses["Scc"] = v1alpha1.ConfigStatusUnknown
		}
		progressingCondition = metav1.Condition{
			Type:    internal.ConfigConditionProgressing,
			Status:  metav1.ConditionUnknown,
			Reason:  internal.ConfigReasonUnknown,
			Message: internal.ConfigMessageUnknown,
		}
		availableCondition = metav1.Condition{
			Type:    internal.ConfigConditionAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  internal.ConfigReasonUnknown,
			Message: internal.ConfigMessageUnknown,
		}
	}

	if bpfmanConfig.Status.Components == nil ||
		!internal.CCSEquals(bpfmanConfig.Status.Components, expectedComponentStatuses) {
		got := fmt.Sprintf("%v", bpfmanConfig.Status.Components)
		want := fmt.Sprintf("%v", expectedComponentStatuses)
		if b, err := json.Marshal(bpfmanConfig.Status.Components); err == nil {
			got = string(b)
		}
		if b, err := json.Marshal(expectedComponentStatuses); err == nil {
			want = string(b)
		}
		return fmt.Errorf("unexpected config.status.componentStatuses, got: %v, expected: %v", got, want)
	}
	if !testutil.ContainsCondition(bpfmanConfig.Status.Conditions, progressingCondition) {
		return fmt.Errorf("conditions %v does not contain condition %v",
			bpfmanConfig.Status.Conditions, progressingCondition)
	}
	if !testutil.ContainsCondition(bpfmanConfig.Status.Conditions, availableCondition) {
		return fmt.Errorf("conditions %v does not contain condition %v",
			bpfmanConfig.Status.Conditions, availableCondition)
	}
	return nil
}

// testReconcileDaemonSets simulates DaemonSets becoming ready by setting their status
// fields to indicate all desired pods are ready and scheduled.
func testReconcileDaemonSets(ctx context.Context, cl client.Client) error {
	bpfmanDs := &appsv1.DaemonSet{}
	if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanDsName, Namespace: internal.BpfmanNamespace},
		bpfmanDs); err != nil {
		return err
	}
	bpfmanDs.Status.DesiredNumberScheduled = 1
	bpfmanDs.Status.UpdatedNumberScheduled = bpfmanDs.Status.DesiredNumberScheduled
	bpfmanDs.Status.NumberAvailable = bpfmanDs.Status.DesiredNumberScheduled
	bpfmanDs.Status.NumberReady = bpfmanDs.Status.DesiredNumberScheduled
	if err := cl.Status().Update(ctx, bpfmanDs); err != nil {
		return err
	}

	metricsProxyDs := &appsv1.DaemonSet{}
	if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanMetricsProxyDsName, Namespace: internal.BpfmanNamespace},
		metricsProxyDs); err != nil {
		return err
	}
	metricsProxyDs.Status.DesiredNumberScheduled = 1
	metricsProxyDs.Status.UpdatedNumberScheduled = metricsProxyDs.Status.DesiredNumberScheduled
	metricsProxyDs.Status.NumberAvailable = metricsProxyDs.Status.DesiredNumberScheduled
	metricsProxyDs.Status.NumberReady = metricsProxyDs.Status.DesiredNumberScheduled
	if err := cl.Status().Update(ctx, metricsProxyDs); err != nil {
		return err
	}

	return nil
}
