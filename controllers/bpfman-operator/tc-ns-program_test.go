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
	"github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestTcNsProgramReconcile(t *testing.T) {
	var (
		name            = "fakeTcNsProgram"
		bytecodePath    = "/tmp/hello.o"
		bpfFunctionName = "test"
		direction       = "ingress"
		fakeNode        = testutils.NewNode("fake-control-plane")
		fakeInt         = "eth0"
		ctx             = context.TODO()
		bpfProgName     = fmt.Sprintf("%s-%s", name, fakeNode.Name)
	)

	// A TcNsProgram object with metadata and spec.
	tc := &bpfmaniov1alpha1.TcNsProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.TcNsProgramSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			TcNsProgramInfo: bpfmaniov1alpha1.TcNsProgramInfo{

				BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
					BpfFunctionName: bpfFunctionName,
				},
				InterfaceSelector: bpfmaniov1alpha1.InterfaceSelector{
					Interfaces: &[]string{fakeInt},
				},
				Priority:  0,
				Direction: direction,
				ProceedOn: []bpfmaniov1alpha1.TcProceedOnValue{
					bpfmaniov1alpha1.TcProceedOnValue("pipe"),
					bpfmaniov1alpha1.TcProceedOnValue("dispatcher_return"),
				},
				Containers: bpfmaniov1alpha1.ContainerNsSelector{
					Pods: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
			},
		},
	}

	// The expected accompanying BpfNsProgram object
	expectedBpfProg := &bpfmaniov1alpha1.BpfNsProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: bpfProgName,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       tc.Name,
					Controller: &[]bool{true}[0],
				},
			},
			Labels:     map[string]string{internal.BpfProgramOwner: tc.Name, internal.K8sHostLabel: fakeNode.Name},
			Finalizers: []string{internal.TcNsProgramControllerFinalizer},
		},
		Spec: bpfmaniov1alpha1.BpfProgramSpec{
			Type: "tc",
		},
		Status: bpfmaniov1alpha1.BpfProgramStatus{
			Conditions: []metav1.Condition{bpfmaniov1alpha1.BpfProgCondLoaded.Condition()},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, tc, expectedBpfProg}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, tc)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(tc).WithRuntimeObjects(objs...).Build()

	rc := ReconcilerCommon[bpfmaniov1alpha1.BpfNsProgram, bpfmaniov1alpha1.BpfNsProgramList]{
		Client: cl,
		Scheme: s,
	}

	npr := NamespaceProgramReconciler{
		ReconcilerCommon: rc,
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &TcNsProgramReconciler{NamespaceProgramReconciler: npr}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: name,
		},
	}

	// First reconcile should add the finalzier to the TcNsProgram object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfNsProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, tc)
	require.NoError(t, err)

	// Check the bpfman-operator finalizer was successfully added
	require.Contains(t, tc.GetFinalizers(), internal.BpfmanOperatorFinalizer)

	// Second reconcile should check BpfNsProgram Status and write Success condition to TcNsProgram Status
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfNsProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}, tc)
	require.NoError(t, err)

	require.Equal(t, tc.Status.Conditions[0].Type, string(bpfmaniov1alpha1.ProgramReconcileSuccess))

}
