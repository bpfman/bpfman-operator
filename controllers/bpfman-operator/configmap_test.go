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
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	"github.com/bpfman/bpfman-operator/internal"
)

// setupTestEnvironment sets up the testing environment for the
// BpfmanConfigReconciler. It initialises a fake client with a
// ConfigMap, registers necessary types with the runtime scheme, and
// configures the reconciler with the fake client and scheme. The
// function will set the RestrictedSCC field on the reconciler object
// if the isOpenShift parameter is set to true, which is required for
// OpenShift-specific tests.
//
// Parameters:
//   - isOpenShift (bool):
//     A flag indicating whether to set up the test environment for
//     OpenShift, which includes setting the RestrictedSCC field on the
//     reconciler object.
//
// Returns:
// - *BpfmanConfigReconciler: The configured reconciler.
// - *corev1.ConfigMap: The test ConfigMap object.
// - reconcile.Request: The reconcile request object.
// - context.Context: The context for the reconcile functio
func setupTestEnvironment(isOpenShift bool) (*BpfmanConfigReconciler, *corev1.ConfigMap, reconcile.Request, context.Context, client.Client) {
	// A configMap for bpfman with metadata and spec.
	bpfmanConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internal.BpfmanConfigName,
			Namespace: internal.BpfmanNamespace,
		},
		Data: map[string]string{
			"bpfman.agent.image":     "BPFMAN_AGENT_IS_SCARY",
			"bpfman.image":           "FAKE-IMAGE",
			"bpfman.agent.log.level": "FAKE",
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{bpfmanConfig}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.ConfigMap{})
	s.AddKnownTypes(appsv1.SchemeGroupVersion, &appsv1.DaemonSet{})
	s.AddKnownTypes(storagev1.SchemeGroupVersion, &storagev1.CSIDriver{})
	s.AddKnownTypes(osv1.GroupVersion, &osv1.SecurityContextConstraints{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	rc := ReconcilerCommon[bpfmaniov1alpha1.ClusterBpfApplicationState, bpfmaniov1alpha1.ClusterBpfApplicationStateList]{
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
		BpfmanStandardDeployment:     resolveConfigPath(internal.BpfmanDaemonManifestPath),
		BpfmanMetricsProxyDeployment: resolveConfigPath(internal.BpfmanMetricsProxyPath),
		CsiDriverDeployment:          resolveConfigPath(internal.BpfmanCsiDriverPath),
		IsOpenshift:                  isOpenShift,
	}
	if isOpenShift {
		r.RestrictedSCC = resolveConfigPath(internal.BpfmanRestrictedSCCPath)
	}

	// Mock request object to simulate Reconcile() being called on
	// an event for a watched resource.
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      internal.BpfmanConfigName,
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

func TestBpfmanConfigReconcileAndDeleteNEW(t *testing.T) {
	for _, tc := range []struct {
		isOpenShift bool
	}{
		{isOpenShift: false},
		{isOpenShift: true},
	} {
		t.Run(fmt.Sprintf("isOpenShift: %v", tc.isOpenShift), func(t *testing.T) {
			r, bpfmanConfig, req, ctx, cl := setupTestEnvironment(tc.isOpenShift)
			require.Equal(t, tc.isOpenShift, r.RestrictedSCC != "", "RestrictedSCC should be non-empty for OpenShift and empty otherwise")

			// The expected bpfman daemonset
			expectedBpfmanDs := LoadAndConfigureBpfmanDs(bpfmanConfig, resolveConfigPath(internal.BpfmanDaemonManifestPath), tc.isOpenShift)

			// First reconcile will add bpfman-operator finalizer to bpfman configmap
			res, err := r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Require no requeue
			require.False(t, res.Requeue)

			// Check the Object was created successfully
			err = cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanConfigName, Namespace: internal.BpfmanNamespace}, bpfmanConfig)
			require.NoError(t, err)

			// Check the bpfman-operator finalizer was successfully added
			require.Contains(t, bpfmanConfig.GetFinalizers(), internal.BpfmanOperatorFinalizer)

			// Second reconcile will create bpfman daemonset and, when isOpenshift holds true, a SCC.
			res, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Require no requeue
			require.False(t, res.Requeue)

			// Check the bpfman daemonset was created successfully
			actualBpfmanDs := &appsv1.DaemonSet{}

			err = cl.Get(ctx, types.NamespacedName{Name: expectedBpfmanDs.Name, Namespace: expectedBpfmanDs.Namespace}, actualBpfmanDs)
			require.NoError(t, err)

			// Check the bpfman daemonset was created with the correct configuration.
			require.True(t, reflect.DeepEqual(actualBpfmanDs.Spec, expectedBpfmanDs.Spec))

			if tc.isOpenShift {
				// Check the SCC was created with the correct configuration.
				actualRestrictedSCC := &osv1.SecurityContextConstraints{}
				expectedRestrictedSCC := LoadRestrictedSecurityContext(resolveConfigPath(internal.BpfmanRestrictedSCCPath))
				err = cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanRestrictedSccName, Namespace: corev1.NamespaceAll}, actualRestrictedSCC)
				require.NoError(t, err)
				// Match ResourceVersion's as that is the only expected difference (0 versus 1).
				expectedRestrictedSCC.ResourceVersion = actualRestrictedSCC.ResourceVersion
				require.True(t, reflect.DeepEqual(actualRestrictedSCC, expectedRestrictedSCC))
			}

			// Delete the bpfman configmap
			err = cl.Delete(ctx, bpfmanConfig)
			require.NoError(t, err)

			// Third reconcile will delete bpfman daemonset
			res, err = r.Reconcile(ctx, req)
			if err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Require no requeue
			require.False(t, res.Requeue)

			err = cl.Get(ctx, types.NamespacedName{Name: expectedBpfmanDs.Name, Namespace: expectedBpfmanDs.Namespace}, actualBpfmanDs)
			require.True(t, errors.IsNotFound(err))

			err = cl.Get(ctx, types.NamespacedName{Name: bpfmanConfig.Name, Namespace: bpfmanConfig.Namespace}, bpfmanConfig)
			require.True(t, errors.IsNotFound(err))

			err = cl.Get(ctx, types.NamespacedName{Name: internal.BpfmanRestrictedSccName, Namespace: corev1.NamespaceAll}, &osv1.SecurityContextConstraints{})
			require.True(t, errors.IsNotFound(err))
		})
	}
}
