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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestUprobeNsProgramReconcile(t *testing.T) {
	var (
		name            = "fakeUprobeProgram"
		bytecodePath    = "/tmp/hello.o"
		bpfFunctionName = "test"
		fakeNode        = testutils.NewNode("fake-control-plane")
		functionName    = "malloc"
		target          = "libc"
		offset          = 0
		retprobe        = false
		ctx             = context.TODO()
		bpfProgName     = fmt.Sprintf("%s-%s", name, fakeNode.Name)
	)
	// A UprobeNsProgram object with metadata and spec.
	Uprobe := &bpfmaniov1alpha1.UprobeNsProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.UprobeNsProgramSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			UprobeNsProgramInfo: bpfmaniov1alpha1.UprobeNsProgramInfo{
				BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
					BpfFunctionName: bpfFunctionName,
				},
				FunctionName: functionName,
				Target:       target,
				Offset:       uint64(offset),
				RetProbe:     retprobe,
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
					Name:       Uprobe.Name,
					Controller: &[]bool{true}[0],
				},
			},
			Labels:     map[string]string{internal.BpfProgramOwner: Uprobe.Name, internal.K8sHostLabel: fakeNode.Name},
			Finalizers: []string{internal.UprobeNsProgramControllerFinalizer},
		},
		Spec: bpfmaniov1alpha1.BpfProgramSpec{
			Type: "uprobe",
		},
		Status: bpfmaniov1alpha1.BpfProgramStatus{
			Conditions: []metav1.Condition{bpfmaniov1alpha1.BpfProgCondLoaded.Condition()},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, Uprobe, expectedBpfProg}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, Uprobe)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(Uprobe).WithRuntimeObjects(objs...).Build()

	rc := ReconcilerCommon[bpfmaniov1alpha1.BpfNsProgram, bpfmaniov1alpha1.BpfNsProgramList]{
		Client: cl,
		Scheme: s,
	}

	npr := NamespaceProgramReconciler{
		ReconcilerCommon: rc,
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a UprobeNsProgram object with the scheme and fake client.
	r := &UprobeNsProgramReconciler{NamespaceProgramReconciler: npr}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: name,
		},
	}

	// First reconcile should add the finalzier to the UprobeNsProgram object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfNsProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: Uprobe.Name, Namespace: Uprobe.Namespace}, Uprobe)
	require.NoError(t, err)

	// Check the bpfman-operator finalizer was successfully added
	require.Contains(t, Uprobe.GetFinalizers(), internal.BpfmanOperatorFinalizer)

	// Second reconcile should check BpfNsProgram Status and write Success condition to tcProgram Status
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfNsProgram Object was created successfully
	err = cl.Get(ctx, types.NamespacedName{Name: Uprobe.Name, Namespace: Uprobe.Namespace}, Uprobe)
	require.NoError(t, err)

	require.Equal(t, Uprobe.Status.Conditions[0].Type, string(bpfmaniov1alpha1.ProgramReconcileSuccess))

}