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
	rbacv1 "k8s.io/api/rbac/v1"
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
	cm               *corev1.ConfigMap
	csiDriver        *storagev1.CSIDriver
	ds               *appsv1.DaemonSet
	metricsDs        *appsv1.DaemonSet
	scc              *osv1.SecurityContextConstraints
	agentSvc         *corev1.Service
	controllerSvc    *corev1.Service
	agentSM          *monitoringv1.ServiceMonitor
	controllerSM     *monitoringv1.ServiceMonitor
	privilegedSccCrb *rbacv1.ClusterRoleBinding
	userCr           *rbacv1.ClusterRole
	prometheusCrb    *rbacv1.ClusterRoleBinding
	prometheusRole   *rbacv1.Role
	prometheusRb     *rbacv1.RoleBinding
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
		{name: "openshift-without-monitoring", isOpenShift: true, hasMonitoring: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("Setting up test environment")
			r, bpfmanConfig, req, ctx, cl := setupTestEnvironment(tc.isOpenShift, tc.hasMonitoring)

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
				objects["privileged-scc-crb"] = &rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: internal.BpfmanPrivilegedSccClusterRoleBindingName,
					},
				}
				objects["user-cr"] = &rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name: internal.BpfmanUserClusterRoleName,
					},
				}
				if tc.hasMonitoring {
					objects["prometheus-crb"] = &rbacv1.ClusterRoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name: internal.BpfmanPrometheusClusterRoleBindingName,
						},
					}
					objects["prometheus-role"] = &rbacv1.Role{
						ObjectMeta: metav1.ObjectMeta{
							Name:      internal.BpfmanPrometheusRoleName,
							Namespace: internal.BpfmanNamespace,
						},
					}
					objects["prometheus-rb"] = &rbacv1.RoleBinding{
						ObjectMeta: metav1.ObjectMeta{
							Name:      internal.BpfmanPrometheusRoleBindingName,
							Namespace: internal.BpfmanNamespace,
						},
					}
				}
			}
			if tc.hasMonitoring {
				objects["metrics-daemonset"] = &appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanMetricsProxyDsName,
						Namespace: internal.BpfmanNamespace,
					},
				}
				objects["agent-metrics-service"] = &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanAgentMetricsServiceName,
						Namespace: internal.BpfmanNamespace,
					},
				}
				objects["controller-metrics-service"] = &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      internal.BpfmanControllerMetricsServiceName,
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
	s.AddKnownTypes(rbacv1.SchemeGroupVersion, &rbacv1.ClusterRoleBinding{}, &rbacv1.ClusterRole{}, &rbacv1.Role{}, &rbacv1.RoleBinding{})
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
	if err := hasOwnerReference(bpfmanConfig, actualCsiDriver); err != nil {
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
	if err := hasOwnerReference(bpfmanConfig, actualBpfmanDs); err != nil {
		return err
	}

	if !hasMonitoring {
		// Verify monitoring resources do NOT exist when monitoring is unavailable.
		absentChecks := []struct {
			name string
			obj  client.Object
			key  types.NamespacedName
		}{
			{"metrics proxy DaemonSet", &appsv1.DaemonSet{}, types.NamespacedName{
				Name: internal.BpfmanMetricsProxyDsName, Namespace: internal.BpfmanNamespace}},
			{"agent metrics Service", &corev1.Service{}, types.NamespacedName{
				Name: internal.BpfmanAgentMetricsServiceName, Namespace: internal.BpfmanNamespace}},
			{"controller metrics Service", &corev1.Service{}, types.NamespacedName{
				Name: internal.BpfmanControllerMetricsServiceName, Namespace: internal.BpfmanNamespace}},
			{"agent ServiceMonitor", &monitoringv1.ServiceMonitor{}, types.NamespacedName{
				Name: internal.BpfmanAgentServiceMonitorName, Namespace: internal.BpfmanNamespace}},
			{"controller ServiceMonitor", &monitoringv1.ServiceMonitor{}, types.NamespacedName{
				Name: internal.BpfmanControllerServiceMonitorName, Namespace: internal.BpfmanNamespace}},
		}
		for _, c := range absentChecks {
			if err := cl.Get(ctx, c.key, c.obj); err == nil {
				return fmt.Errorf("%s %q should not exist without monitoring", c.name, c.key.Name)
			}
		}

		if isOpenShift {
			// Prometheus RBAC should not exist without monitoring even on OpenShift.
			promAbsentChecks := []struct {
				name string
				obj  client.Object
				key  types.NamespacedName
			}{
				{"Prometheus ClusterRoleBinding", &rbacv1.ClusterRoleBinding{}, types.NamespacedName{
					Name: internal.BpfmanPrometheusClusterRoleBindingName}},
				{"Prometheus Role", &rbacv1.Role{}, types.NamespacedName{
					Name: internal.BpfmanPrometheusRoleName, Namespace: internal.BpfmanNamespace}},
				{"Prometheus RoleBinding", &rbacv1.RoleBinding{}, types.NamespacedName{
					Name: internal.BpfmanPrometheusRoleBindingName, Namespace: internal.BpfmanNamespace}},
			}
			for _, c := range promAbsentChecks {
				if err := cl.Get(ctx, c.key, c.obj); err == nil {
					return fmt.Errorf("%s %q should not exist without monitoring", c.name, c.key.Name)
				}
			}
		}
	}

	if isOpenShift {
		// Check the SCC was created with the correct configuration.
		actualRestrictedSCC := &osv1.SecurityContextConstraints{}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanRestrictedSccName, Namespace: corev1.NamespaceAll},
			actualRestrictedSCC); err != nil {
			return err
		}
		if err := hasOwnerReference(bpfmanConfig, actualRestrictedSCC); err != nil {
			return err
		}
		if actualRestrictedSCC.RunAsUser.Type != osv1.RunAsUserStrategyMustRunAsNonRoot {
			return fmt.Errorf("restricted SCC RunAsUser.Type=%q, expected %q",
				actualRestrictedSCC.RunAsUser.Type, osv1.RunAsUserStrategyMustRunAsNonRoot)
		}
		if actualRestrictedSCC.SELinuxContext.Type != osv1.SELinuxStrategyRunAsAny {
			return fmt.Errorf("restricted SCC SELinuxContext.Type=%q, expected %q",
				actualRestrictedSCC.SELinuxContext.Type, osv1.SELinuxStrategyRunAsAny)
		}
		if actualRestrictedSCC.AllowPrivilegedContainer {
			return fmt.Errorf("restricted SCC AllowPrivilegedContainer should be false")
		}

		// Check the privileged SCC ClusterRoleBinding.
		privilegedCRB := &rbacv1.ClusterRoleBinding{}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanPrivilegedSccClusterRoleBindingName}, privilegedCRB); err != nil {
			return fmt.Errorf("get ClusterRoleBinding %s: %w", internal.BpfmanPrivilegedSccClusterRoleBindingName, err)
		}
		if err := hasOwnerReference(bpfmanConfig, privilegedCRB); err != nil {
			return err
		}
		if privilegedCRB.RoleRef.Name != "system:openshift:scc:privileged" {
			return fmt.Errorf("privileged CRB roleRef.name=%q, expected %q",
				privilegedCRB.RoleRef.Name, "system:openshift:scc:privileged")
		}
		if len(privilegedCRB.Subjects) != 1 || privilegedCRB.Subjects[0].Name != "bpfman-daemon" {
			return fmt.Errorf("privileged CRB subjects=%+v, expected single bpfman-daemon SA", privilegedCRB.Subjects)
		}

		// Check the bpfman-user ClusterRole.
		userCR := &rbacv1.ClusterRole{}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanUserClusterRoleName}, userCR); err != nil {
			return fmt.Errorf("get ClusterRole %s: %w", internal.BpfmanUserClusterRoleName, err)
		}
		if err := hasOwnerReference(bpfmanConfig, userCR); err != nil {
			return err
		}
		if len(userCR.Rules) != 1 || userCR.Rules[0].Verbs[0] != "use" {
			return fmt.Errorf("user CR rules=%+v, expected single 'use' rule for bpfman-restricted SCC", userCR.Rules)
		}

		if hasMonitoring {
			// Check the Prometheus metrics reader ClusterRoleBinding.
			promCRB := &rbacv1.ClusterRoleBinding{}
			if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanPrometheusClusterRoleBindingName}, promCRB); err != nil {
				return fmt.Errorf("get ClusterRoleBinding %s: %w", internal.BpfmanPrometheusClusterRoleBindingName, err)
			}
			if err := hasOwnerReference(bpfmanConfig, promCRB); err != nil {
				return err
			}
			if promCRB.RoleRef.Name != "bpfman-metrics-reader" {
				return fmt.Errorf("prometheus CRB roleRef.name=%q, expected %q",
					promCRB.RoleRef.Name, "bpfman-metrics-reader")
			}

			// Check the Prometheus Role.
			promRole := &rbacv1.Role{}
			if err := cl.Get(ctx, types.NamespacedName{
				Name:      internal.BpfmanPrometheusRoleName,
				Namespace: internal.BpfmanNamespace,
			}, promRole); err != nil {
				return fmt.Errorf("get Role %s: %w", internal.BpfmanPrometheusRoleName, err)
			}
			if err := hasOwnerReference(bpfmanConfig, promRole); err != nil {
				return err
			}
			if len(promRole.Rules) != 1 || promRole.Rules[0].Verbs[0] != "get" {
				return fmt.Errorf("prometheus Role rules=%+v, expected single rule with get/list/watch", promRole.Rules)
			}

			// Check the Prometheus RoleBinding.
			promRB := &rbacv1.RoleBinding{}
			if err := cl.Get(ctx, types.NamespacedName{
				Name:      internal.BpfmanPrometheusRoleBindingName,
				Namespace: internal.BpfmanNamespace,
			}, promRB); err != nil {
				return fmt.Errorf("get RoleBinding %s: %w", internal.BpfmanPrometheusRoleBindingName, err)
			}
			if err := hasOwnerReference(bpfmanConfig, promRB); err != nil {
				return err
			}
			if promRB.RoleRef.Name != internal.BpfmanPrometheusRoleName {
				return fmt.Errorf("prometheus RB roleRef.name=%q, expected %q",
					promRB.RoleRef.Name, internal.BpfmanPrometheusRoleName)
			}
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

		// Check the agent metrics Service was created.
		agentSvc := &corev1.Service{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanAgentMetricsServiceName,
			Namespace: internal.BpfmanNamespace,
		}, agentSvc); err != nil {
			return fmt.Errorf("failed to get agent metrics Service: %w", err)
		}
		if err := hasOwnerReference(bpfmanConfig, agentSvc); err != nil {
			return fmt.Errorf("agent metrics Service owner reference: %w", err)
		}
		if len(agentSvc.Spec.Ports) != 1 || agentSvc.Spec.Ports[0].Port != 8443 {
			return fmt.Errorf("agent metrics Service ports=%+v, expected single port 8443", agentSvc.Spec.Ports)
		}
		if agentSvc.Spec.Selector["name"] != "bpfman-metrics-proxy" {
			return fmt.Errorf("agent metrics Service selector=%+v, expected name=bpfman-metrics-proxy",
				agentSvc.Spec.Selector)
		}

		// Check the controller metrics Service was created.
		controllerSvc := &corev1.Service{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanControllerMetricsServiceName,
			Namespace: internal.BpfmanNamespace,
		}, controllerSvc); err != nil {
			return fmt.Errorf("failed to get controller metrics Service: %w", err)
		}
		if err := hasOwnerReference(bpfmanConfig, controllerSvc); err != nil {
			return fmt.Errorf("controller metrics Service owner reference: %w", err)
		}
		if len(controllerSvc.Spec.Ports) != 1 || controllerSvc.Spec.Ports[0].Port != 8443 {
			return fmt.Errorf("controller metrics Service ports=%+v, expected single port 8443",
				controllerSvc.Spec.Ports)
		}
		if controllerSvc.Spec.Selector["control-plane"] != "controller-manager" {
			return fmt.Errorf("controller metrics Service selector=%+v, expected control-plane=controller-manager",
				controllerSvc.Spec.Selector)
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

		// Verify ServiceMonitor selectors match corresponding Service
		// labels so that Prometheus actually discovers scrape targets.
		for k, v := range agentSM.Spec.Selector.MatchLabels {
			if agentSvc.Labels[k] != v {
				return fmt.Errorf("agent ServiceMonitor selector %s=%s does not match agent Service labels %v",
					k, v, agentSvc.Labels)
			}
		}
		for k, v := range controllerSM.Spec.Selector.MatchLabels {
			if controllerSvc.Labels[k] != v {
				return fmt.Errorf("controller ServiceMonitor selector %s=%s does not match controller Service labels %v",
					k, v, controllerSvc.Labels)
			}
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

		// Corrupt the privileged SCC ClusterRoleBinding subjects.
		privilegedCRB := &rbacv1.ClusterRoleBinding{}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanPrivilegedSccClusterRoleBindingName}, privilegedCRB); err != nil {
			return co, err
		}
		privilegedCRB.Subjects = []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      "wrong-sa",
			Namespace: "wrong-ns",
		}}
		if err := cl.Update(ctx, privilegedCRB); err != nil {
			return co, err
		}
		co.privilegedSccCrb = privilegedCRB

		// Corrupt the bpfman-user ClusterRole rules.
		userCR := &rbacv1.ClusterRole{}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanUserClusterRoleName}, userCR); err != nil {
			return co, err
		}
		userCR.Rules = []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get"},
		}}
		if err := cl.Update(ctx, userCR); err != nil {
			return co, err
		}
		co.userCr = userCR

		if hasMonitoring {
			// Corrupt the Prometheus metrics reader ClusterRoleBinding subjects.
			promCRB := &rbacv1.ClusterRoleBinding{}
			if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanPrometheusClusterRoleBindingName}, promCRB); err != nil {
				return co, err
			}
			promCRB.Subjects = []rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "wrong-sa",
				Namespace: "wrong-ns",
			}}
			if err := cl.Update(ctx, promCRB); err != nil {
				return co, err
			}
			co.prometheusCrb = promCRB

			// Corrupt the Prometheus Role rules.
			promRole := &rbacv1.Role{}
			if err := cl.Get(ctx, types.NamespacedName{
				Name:      internal.BpfmanPrometheusRoleName,
				Namespace: internal.BpfmanNamespace,
			}, promRole); err != nil {
				return co, err
			}
			promRole.Rules = []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get"},
			}}
			if err := cl.Update(ctx, promRole); err != nil {
				return co, err
			}
			co.prometheusRole = promRole

			// Corrupt the Prometheus RoleBinding subjects.
			promRB := &rbacv1.RoleBinding{}
			if err := cl.Get(ctx, types.NamespacedName{
				Name:      internal.BpfmanPrometheusRoleBindingName,
				Namespace: internal.BpfmanNamespace,
			}, promRB); err != nil {
				return co, err
			}
			promRB.Subjects = []rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "wrong-sa",
				Namespace: "wrong-ns",
			}}
			if err := cl.Update(ctx, promRB); err != nil {
				return co, err
			}
			co.prometheusRb = promRB
		}
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

		// Modify agent metrics Service.
		agentSvc := &corev1.Service{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanAgentMetricsServiceName,
			Namespace: internal.BpfmanNamespace,
		}, agentSvc); err != nil {
			return co, err
		}
		agentSvc.Spec.Ports[0].Port = 9999
		if err := cl.Update(ctx, agentSvc); err != nil {
			return co, err
		}
		co.agentSvc = agentSvc

		// Modify controller metrics Service.
		controllerSvc := &corev1.Service{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanControllerMetricsServiceName,
			Namespace: internal.BpfmanNamespace,
		}, controllerSvc); err != nil {
			return co, err
		}
		controllerSvc.Spec.Ports[0].Port = 9999
		if err := cl.Update(ctx, controllerSvc); err != nil {
			return co, err
		}
		co.controllerSvc = controllerSvc

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
			APIVersion: "v1",
			Kind:       "Service",
			Namespace:  "bpfman",
			Name:       internal.BpfmanAgentMetricsServiceName,
			Unmanaged:  true,
		},
		{
			APIVersion: "v1",
			Kind:       "Service",
			Namespace:  "bpfman",
			Name:       internal.BpfmanControllerMetricsServiceName,
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
		{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
			Namespace:  "",
			Name:       internal.BpfmanPrivilegedSccClusterRoleBindingName,
			Unmanaged:  true,
		},
		{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
			Namespace:  "",
			Name:       internal.BpfmanUserClusterRoleName,
			Unmanaged:  true,
		},
		{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
			Namespace:  "",
			Name:       internal.BpfmanPrometheusClusterRoleBindingName,
			Unmanaged:  true,
		},
		{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
			Namespace:  internal.BpfmanNamespace,
			Name:       internal.BpfmanPrometheusRoleName,
			Unmanaged:  true,
		},
		{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
			Namespace:  internal.BpfmanNamespace,
			Name:       internal.BpfmanPrometheusRoleBindingName,
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

		// Privileged SCC ClusterRoleBinding should be unchanged.
		privilegedCRB := &rbacv1.ClusterRoleBinding{}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanPrivilegedSccClusterRoleBindingName}, privilegedCRB); err != nil {
			return err
		}
		if !reflect.DeepEqual(privilegedCRB, cos.privilegedSccCrb) {
			return fmt.Errorf("deep equal failed for privileged SCC CRB, got: %+v, expected: %+v", privilegedCRB, cos.privilegedSccCrb)
		}

		// bpfman-user ClusterRole should be unchanged.
		userCR := &rbacv1.ClusterRole{}
		if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanUserClusterRoleName}, userCR); err != nil {
			return err
		}
		if !reflect.DeepEqual(userCR, cos.userCr) {
			return fmt.Errorf("deep equal failed for user CR, got: %+v, expected: %+v", userCR, cos.userCr)
		}

		if hasMonitoring {
			// Prometheus metrics reader ClusterRoleBinding should be unchanged.
			promCRB := &rbacv1.ClusterRoleBinding{}
			if err := cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanPrometheusClusterRoleBindingName}, promCRB); err != nil {
				return err
			}
			if !reflect.DeepEqual(promCRB, cos.prometheusCrb) {
				return fmt.Errorf("deep equal failed for prometheus CRB, got: %+v, expected: %+v", promCRB, cos.prometheusCrb)
			}

			// Prometheus Role should be unchanged.
			promRole := &rbacv1.Role{}
			if err := cl.Get(ctx, types.NamespacedName{
				Name:      internal.BpfmanPrometheusRoleName,
				Namespace: internal.BpfmanNamespace,
			}, promRole); err != nil {
				return err
			}
			if !reflect.DeepEqual(promRole, cos.prometheusRole) {
				return fmt.Errorf("deep equal failed for prometheus Role, got: %+v, expected: %+v", promRole, cos.prometheusRole)
			}

			// Prometheus RoleBinding should be unchanged.
			promRB := &rbacv1.RoleBinding{}
			if err := cl.Get(ctx, types.NamespacedName{
				Name:      internal.BpfmanPrometheusRoleBindingName,
				Namespace: internal.BpfmanNamespace,
			}, promRB); err != nil {
				return err
			}
			if !reflect.DeepEqual(promRB, cos.prometheusRb) {
				return fmt.Errorf("deep equal failed for prometheus RB, got: %+v, expected: %+v", promRB, cos.prometheusRb)
			}
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

		// Agent metrics Service.
		agentSvc := &corev1.Service{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanAgentMetricsServiceName,
			Namespace: internal.BpfmanNamespace,
		}, agentSvc); err != nil {
			return err
		}
		if !reflect.DeepEqual(agentSvc, cos.agentSvc) {
			return fmt.Errorf("deep equal failed for agent metrics Service, got: %+v, expected: %+v",
				agentSvc, cos.agentSvc)
		}

		// Controller metrics Service.
		controllerSvc := &corev1.Service{}
		if err := cl.Get(ctx, types.NamespacedName{
			Name:      internal.BpfmanControllerMetricsServiceName,
			Namespace: internal.BpfmanNamespace,
		}, controllerSvc); err != nil {
			return err
		}
		if !reflect.DeepEqual(controllerSvc, cos.controllerSvc) {
			return fmt.Errorf("deep equal failed for controller metrics Service, got: %+v, expected: %+v",
				controllerSvc, cos.controllerSvc)
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

// TestAdoptExistingResources verifies that the reconciler adopts
// pre-existing resources that lack an owner reference by writing the
// controller owner reference on the next reconciliation, even when
// the resource spec has not changed.  Resources are pre-created
// directly in the fake client (without ownerRefs) before the first
// reconcile, simulating an upgrade from a release where these objects
// were deployed via static manifests.
func TestAdoptExistingResources(t *testing.T) {
	r, bpfmanConfig, req, ctx, cl := setupTestEnvironment(true, true)

	ns := bpfmanConfig.Spec.Namespace

	// Pre-create resources that the controller would normally
	// create, but without owner references -- simulating objects
	// left behind by static manifests from an earlier release.
	preExisting := []struct {
		name string
		obj  client.Object
		key  types.NamespacedName
	}{
		{
			name: "privileged SCC ClusterRoleBinding",
			obj: &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: internal.BpfmanPrivilegedSccClusterRoleBindingName},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "system:openshift:scc:privileged",
				},
				Subjects: []rbacv1.Subject{{
					Kind: "ServiceAccount", Name: "bpfman-daemon", Namespace: ns,
				}},
			},
			key: types.NamespacedName{Name: internal.BpfmanPrivilegedSccClusterRoleBindingName},
		},
		{
			name: "bpfman-user ClusterRole",
			obj: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: internal.BpfmanUserClusterRoleName},
				Rules: []rbacv1.PolicyRule{{
					APIGroups:     []string{"security.openshift.io"},
					ResourceNames: []string{"bpfman-restricted"},
					Resources:     []string{"securitycontextconstraints"},
					Verbs:         []string{"use"},
				}},
			},
			key: types.NamespacedName{Name: internal.BpfmanUserClusterRoleName},
		},
		{
			name: "Prometheus Role",
			obj: &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name: internal.BpfmanPrometheusRoleName, Namespace: ns,
				},
				Rules: []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods", "services", "endpoints", "configmaps", "secrets"},
					Verbs:     []string{"get", "list", "watch"},
				}},
			},
			key: types.NamespacedName{Name: internal.BpfmanPrometheusRoleName, Namespace: ns},
		},
		{
			name: "Prometheus RoleBinding",
			obj: &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: internal.BpfmanPrometheusRoleBindingName, Namespace: ns,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     internal.BpfmanPrometheusRoleName,
				},
				Subjects: []rbacv1.Subject{{
					Kind: "ServiceAccount", Name: "prometheus-k8s", Namespace: "openshift-monitoring",
				}},
			},
			key: types.NamespacedName{Name: internal.BpfmanPrometheusRoleBindingName, Namespace: ns},
		},
		{
			name: "Prometheus ClusterRoleBinding",
			obj: &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: internal.BpfmanPrometheusClusterRoleBindingName},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "bpfman-metrics-reader",
				},
				Subjects: []rbacv1.Subject{{
					Kind: "ServiceAccount", Name: "prometheus-k8s", Namespace: "openshift-monitoring",
				}},
			},
			key: types.NamespacedName{Name: internal.BpfmanPrometheusClusterRoleBindingName},
		},
		{
			// Populate the full spec so that needsUpdate returns
			// false and only the missing ownerRef triggers the
			// update -- the exact failure mode behind P2.
			name: "agent ServiceMonitor",
			obj: &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name: internal.BpfmanAgentServiceMonitorName, Namespace: ns,
				},
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{{
						Path:            "/agent-metrics",
						Port:            "https-metrics",
						Scheme:          ptr.To(monitoringv1.Scheme("https")),
						BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
						HTTPConfigWithProxyAndTLSFiles: monitoringv1.HTTPConfigWithProxyAndTLSFiles{
							HTTPConfigWithTLSFiles: monitoringv1.HTTPConfigWithTLSFiles{
								TLSConfig: serviceMonitorTLSConfig(true, "bpfman-agent-metrics-service", ns),
							},
						},
					}},
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{
						"app.kubernetes.io/component": "metrics",
						"app.kubernetes.io/instance":  "agent-metrics-service",
						"app.kubernetes.io/name":      "agent-metrics-service",
					}},
				},
			},
			key: types.NamespacedName{Name: internal.BpfmanAgentServiceMonitorName, Namespace: ns},
		},
		{
			name: "controller ServiceMonitor",
			obj: &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name: internal.BpfmanControllerServiceMonitorName, Namespace: ns,
				},
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{{
						Path:            "/metrics",
						Port:            "https-metrics",
						Scheme:          ptr.To(monitoringv1.Scheme("https")),
						BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
						HTTPConfigWithProxyAndTLSFiles: monitoringv1.HTTPConfigWithProxyAndTLSFiles{
							HTTPConfigWithTLSFiles: monitoringv1.HTTPConfigWithTLSFiles{
								TLSConfig: &monitoringv1.TLSConfig{
									SafeTLSConfig: monitoringv1.SafeTLSConfig{
										InsecureSkipVerify: ptr.To(true),
									},
								},
							},
						},
					}},
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{
						"control-plane": "controller-manager",
					}},
				},
			},
			key: types.NamespacedName{Name: internal.BpfmanControllerServiceMonitorName, Namespace: ns},
		},
	}

	for _, p := range preExisting {
		require.NoError(t, cl.Create(ctx, p.obj), "pre-create %s", p.name)
	}

	// Run initial reconcile (adds finalizer).
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Run second reconcile (reconciles resources).
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Every pre-existing resource should now carry the controller
	// owner reference despite having been created externally.
	for _, p := range preExisting {
		fresh := p.obj.DeepCopyObject().(client.Object)
		require.NoError(t, cl.Get(ctx, p.key, fresh), "get %s", p.name)
		require.NoError(t, hasOwnerReference(bpfmanConfig, fresh),
			"%s should have been adopted", p.name)
	}
}

// TestImagePullPolicyEnvVar verifies that setting
// BPFMAN_IMAGE_PULL_POLICY after DaemonSets already exist causes the
// reconciler to update them with the new policy. The test covers both
// the bpfman-daemon DaemonSet (always present) and the
// bpfman-metrics-proxy DaemonSet (only when monitoring is enabled).
func TestImagePullPolicyEnvVar(t *testing.T) {
	tests := []struct {
		name          string
		hasMonitoring bool
		daemonSets    []string
	}{
		{
			name:          "plain-k8s",
			hasMonitoring: false,
			daemonSets:    []string{internal.BpfmanDsName},
		},
		{
			name:          "k8s-with-monitoring",
			hasMonitoring: true,
			daemonSets:    []string{internal.BpfmanDsName, internal.BpfmanMetricsProxyDsName},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, req, ctx, cl := setupTestEnvironment(false, tc.hasMonitoring)

			// First reconcile adds the finalizer.
			_, err := r.Reconcile(ctx, req)
			require.NoError(t, err)

			// Second reconcile creates resources.
			_, err = r.Reconcile(ctx, req)
			require.NoError(t, err)

			// Verify DaemonSets were created without an explicit imagePullPolicy.
			for _, dsName := range tc.daemonSets {
				ds := &appsv1.DaemonSet{}
				require.NoError(t, cl.Get(ctx, types.NamespacedName{
					Name: dsName, Namespace: internal.BpfmanNamespace,
				}, ds))
				for _, c := range ds.Spec.Template.Spec.Containers {
					require.Empty(t, c.ImagePullPolicy,
						"%s container %s should have no imagePullPolicy initially", dsName, c.Name)
				}
				for _, c := range ds.Spec.Template.Spec.InitContainers {
					require.Empty(t, c.ImagePullPolicy,
						"%s init container %s should have no imagePullPolicy initially", dsName, c.Name)
				}
			}

			// Now set the env var and reconcile again.
			t.Setenv("BPFMAN_IMAGE_PULL_POLICY", "IfNotPresent")

			_, err = r.Reconcile(ctx, req)
			require.NoError(t, err)

			// Verify DaemonSets were updated with the new policy.
			for _, dsName := range tc.daemonSets {
				ds := &appsv1.DaemonSet{}
				require.NoError(t, cl.Get(ctx, types.NamespacedName{
					Name: dsName, Namespace: internal.BpfmanNamespace,
				}, ds))
				for _, c := range ds.Spec.Template.Spec.Containers {
					require.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy,
						"%s container %s should have IfNotPresent after env var set", dsName, c.Name)
				}
				for _, c := range ds.Spec.Template.Spec.InitContainers {
					require.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy,
						"%s init container %s should have IfNotPresent after env var set", dsName, c.Name)
				}
			}
		})
	}
}
