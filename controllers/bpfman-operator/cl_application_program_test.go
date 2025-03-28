/*
Copyright 2024 The bpfman Authors.

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
// with an error case in which the BpfApplication object has multiple conditions.
func appProgramReconcile(t *testing.T, multiCondition bool) {
	var (
		bpfAppName                = "fakeAppProgram"
		bytecodePath              = "/tmp/hello.o"
		xdpBpfFunctionName        = "XdpTest"
		fakeNode                  = testutils.NewNode("fake-control-plane")
		ctx                       = context.TODO()
		bpfAppStateName           = fmt.Sprintf("%s-%s", bpfAppName, "12345")
		xdpPriority               = 50
		fakeInt0                  = "eth0"
		bpfFentryFunctionName     = "fentry_test"
		bpfKprobeFunctionName     = "kprobe_test"
		bpfTracepointFunctionName = "tracepoint-test"
		functionFentryName        = "do_unlinkat"
		functionKprobeName        = "try_to_wake_up"
		tracepointName            = "syscalls/sys_enter_setitimer"
		offset                    = 0
	)

	fakeInts := []string{fakeInt0}

	interfaceSelector := bpfmaniov1alpha1.InterfaceSelector{
		Interfaces: fakeInts,
	}

	proceedOn := []bpfmaniov1alpha1.XdpProceedOnValue{bpfmaniov1alpha1.XdpProceedOnValue("pass"),
		bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return")}

	attachInfo := bpfmaniov1alpha1.ClXdpAttachInfo{
		InterfaceSelector: interfaceSelector,
		NetworkNamespaces: nil,
		Priority:          int32(xdpPriority),
		ProceedOn:         proceedOn,
	}

	programs := []bpfmaniov1alpha1.ClBpfApplicationProgram{}

	xdpProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: xdpBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeXDP,
		XDP: &bpfmaniov1alpha1.ClXdpProgramInfo{
			Links: []bpfmaniov1alpha1.ClXdpAttachInfo{attachInfo},
		},
	}

	programs = append(programs, xdpProgram)

	fentryProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: bpfFentryFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeFentry,
		FEntry: &bpfmaniov1alpha1.ClFentryProgramInfo{
			ClFentryLoadInfo: bpfmaniov1alpha1.ClFentryLoadInfo{
				Function: functionFentryName,
			},
			Links: []bpfmaniov1alpha1.ClFentryAttachInfo{
				{},
			},
		},
	}

	programs = append(programs, fentryProgram)

	kprobeProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: bpfKprobeFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeKprobe,
		KProbe: &bpfmaniov1alpha1.ClKprobeProgramInfo{
			Links: []bpfmaniov1alpha1.ClKprobeAttachInfo{
				{
					Function: functionKprobeName,
					Offset:   uint64(offset),
				},
			},
		},
	}

	programs = append(programs, kprobeProgram)

	tracepointProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: bpfTracepointFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeTracepoint,
		TracePoint: &bpfmaniov1alpha1.ClTracepointProgramInfo{
			Links: []bpfmaniov1alpha1.ClTracepointAttachInfo{
				{
					Name: tracepointName,
				},
			},
		},
	}

	programs = append(programs, tracepointProgram)

	app := &bpfmaniov1alpha1.ClusterBpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: bpfAppName,
		},
		Spec: bpfmaniov1alpha1.ClBpfApplicationSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.ByteCodeSelector{
					Path: &bytecodePath,
				},
			},
			Programs: programs,
		},
	}

	// The expected accompanying BpfApplicationState object
	expectedBpfAppState := &bpfmaniov1alpha1.ClusterBpfApplicationState{
		ObjectMeta: metav1.ObjectMeta{
			Name: bpfAppStateName,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       app.Name,
					Controller: &[]bool{true}[0],
				},
			},
			Labels:     map[string]string{internal.BpfAppStateOwner: app.Name, internal.K8sHostLabel: fakeNode.Name},
			Finalizers: []string{internal.ClBpfApplicationControllerFinalizer},
		},
		Status: bpfmaniov1alpha1.ClBpfApplicationStateStatus{
			AppLoadStatus: bpfmaniov1alpha1.AppLoadSuccess,
			Programs:      []bpfmaniov1alpha1.ClBpfApplicationProgramState{},
			Conditions:    []metav1.Condition{bpfmaniov1alpha1.BpfAppCondSuccess.Condition("")},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, app, expectedBpfAppState}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, app)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationState{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationStateList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(app).WithRuntimeObjects(objs...).Build()

	rc := ReconcilerCommon[bpfmaniov1alpha1.ClusterBpfApplicationState, bpfmaniov1alpha1.ClusterBpfApplicationStateList]{
		Client: cl,
		Scheme: s,
	}

	cpr := ClusterApplicationReconciler{
		ReconcilerCommon: rc,
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a BpfApplication object with the scheme and fake client.
	r := &BpfApplicationReconciler{ClusterApplicationReconciler: cpr}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: bpfAppName,
		},
	}

	// First reconcile should add the finalizer to the applicationProgram object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check if the BpfApplication Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: metav1.NamespaceAll}, app)
	require.NoError(t, err)

	// Check the bpfman-operator finalizer was successfully added
	require.Contains(t, app.GetFinalizers(), internal.BpfmanOperatorFinalizer)

	// NOTE: THIS IS A TEST FOR AN ERROR PATH. THERE SHOULD NEVER BE MORE THAN
	// ONE CONDITION.
	if multiCondition {
		// Add some random conditions and verify that the condition still gets
		// updated correctly.
		meta.SetStatusCondition(&app.Status.Conditions, bpfmaniov1alpha1.BpfAppCondDeleteError.Condition("bogus condition #1"))
		if err := r.Status().Update(ctx, app); err != nil {
			r.Logger.V(1).Info("failed to set BpfApplication object status")
		}
		meta.SetStatusCondition(&app.Status.Conditions, bpfmaniov1alpha1.BpfAppCondError.Condition("bogus condition #2"))
		if err := r.Status().Update(ctx, app); err != nil {
			r.Logger.V(1).Info("failed to set BpfApplication object status")
		}
		// Make sure we have 2 conditions
		require.Equal(t, 2, len(app.Status.Conditions))
	}

	// Second reconcile should check bpfProgram Status and write Success condition to tcProgram Status
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check if the BpfApplication Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: metav1.NamespaceAll}, app)
	require.NoError(t, err)

	// Make sure we only have 1 condition now
	require.Equal(t, 1, len(app.Status.Conditions))
	// Make sure it's the right one.
	require.Equal(t, app.Status.Conditions[0].Type, string(bpfmaniov1alpha1.BpfAppCondSuccess))
}

func TestAppProgramReconcile(t *testing.T) {
	appProgramReconcile(t, false)
}

func TestAppUpdateStatus(t *testing.T) {
	appProgramReconcile(t, true)
}
