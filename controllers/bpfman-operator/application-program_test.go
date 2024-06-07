/*
Copyright 2024.

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
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	internal "github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"

	"github.com/stretchr/testify/require"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Runs the ApplicationProgramReconcile test.  If multiCondition == true, it runs it
// with an error case in which the program object has multiple conditions.
func appProgramReconcile(t *testing.T, multiCondition bool) {
	var (
		name                      = "fakeAppProgram"
		bytecodePath              = "/tmp/hello.o"
		bpfFentryFunctionName     = "fentry_test"
		bpfKprobeFunctionName     = "kprobe_test"
		bpfTracepointFunctionName = "tracepoint-test"
		fakeNode                  = testutils.NewNode("fake-control-plane")
		functionFentryName        = "do_unlinkat"
		functionKprobeName        = "try_to_wake_up"
		tracepointName            = "syscalls/sys_enter_setitimer"
		offset                    = 0
		retprobe                  = false
		ctx                       = context.TODO()
		bpfProgName               = fmt.Sprintf("%s-%s", name, fakeNode.Name)
	)
	// A AppProgram object with metadata and spec.
	App := &bpfmaniov1alpha1.BpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.BpfApplicationSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			Programs: []bpfmaniov1alpha1.BpfApplicationProgram{
				{
					Type: bpfmaniov1alpha1.ProgTypeFentry,
					Fentry: &bpfmaniov1alpha1.FentryProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfFentryFunctionName,
						},
						FunctionName: functionFentryName,
					},
				},
				{
					Type: bpfmaniov1alpha1.ProgTypeKprobe,
					Kprobe: &bpfmaniov1alpha1.KprobeProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfKprobeFunctionName,
						},
						FunctionName: functionKprobeName,
						Offset:       uint64(offset),
						RetProbe:     retprobe,
					},
				},
				{
					Type: bpfmaniov1alpha1.ProgTypeTracepoint,
					Tracepoint: &bpfmaniov1alpha1.TracepointProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfTracepointFunctionName,
						},
						Names: []string{tracepointName},
					},
				},
			},
		},
	}

	// The expected accompanying BpfProgram object
	expectedBpfProg := &bpfmaniov1alpha1.BpfProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: bpfProgName,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       App.Name,
					Controller: &[]bool{true}[0],
				},
			},
			Labels:     map[string]string{internal.BpfProgramOwnerLabel: App.Name, internal.K8sHostLabel: fakeNode.Name},
			Finalizers: []string{internal.BpfApplicationControllerFinalizer},
		},
		Spec: bpfmaniov1alpha1.BpfProgramSpec{
			Type: "application",
		},
		Status: bpfmaniov1alpha1.BpfProgramStatus{
			Conditions: []metav1.Condition{bpfmaniov1alpha1.BpfProgCondLoaded.Condition()},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, App, expectedBpfProg}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, App)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(App).WithRuntimeObjects(objs...).Build()

	rc := ReconcilerCommon{
		Client: cl,
		Scheme: s,
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ApplicationProgram object with the scheme and fake client.
	r := &BpfApplicationReconciler{ReconcilerCommon: rc}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: name,
		},
	}

	// First reconcile should add the finalizer to the applicationProgram object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: App.Name, Namespace: metav1.NamespaceAll}, App)
	require.NoError(t, err)

	// Check the bpfman-operator finalizer was successfully added
	require.Contains(t, App.GetFinalizers(), internal.BpfmanOperatorFinalizer)

	// NOTE: THIS IS A TEST FOR AN ERROR PATH. THERE SHOULD NEVER BE MORE THAN
	// ONE CONDITION.
	if multiCondition {
		// Add some random conditions and verify that the condition still gets
		// updated correctly.
		meta.SetStatusCondition(&App.Status.Conditions, bpfmaniov1alpha1.ProgramDeleteError.Condition("bogus condition #1"))
		if err := r.Status().Update(ctx, App); err != nil {
			r.Logger.V(1).Info("failed to set App Program object status")
		}
		meta.SetStatusCondition(&App.Status.Conditions, bpfmaniov1alpha1.ProgramReconcileError.Condition("bogus condition #2"))
		if err := r.Status().Update(ctx, App); err != nil {
			r.Logger.V(1).Info("failed to set App Program object status")
		}
		// Make sure we have 2 conditions
		require.Equal(t, 2, len(App.Status.Conditions))
	}

	// Second reconcile should check bpfProgram Status and write Success condition to tcProgram Status
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: App.Name, Namespace: metav1.NamespaceAll}, App)
	require.NoError(t, err)

	// Make sure we only have 1 condition now
	require.Equal(t, 1, len(App.Status.Conditions))
	// Make sure it's the right one.
	require.Equal(t, App.Status.Conditions[0].Type, string(bpfmaniov1alpha1.ProgramReconcileSuccess))
}

func TestAppProgramReconcile(t *testing.T) {
	appProgramReconcile(t, false)
}

func TestAppUpdateStatus(t *testing.T) {
	appProgramReconcile(t, true)
}
