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

package bpfmanagent

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	agenttestutils "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal/test-utils"
	internal "github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"

	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
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

func TestUprobeNsProgramControllerCreate(t *testing.T) {
	var (
		name              = "fakeUprobeProgram"
		namespace         = "bpfman"
		bytecodePath      = "/tmp/hello.o"
		bpfFunctionName   = "test"
		functionName      = "malloc"
		target            = "libc"
		offset            = 0
		retprobe          = false
		fakeNode          = testutils.NewNode("fake-control-plane")
		fakePodName       = "my-pod"
		fakeContainerName = "my-container-1"
		fakePid           = int64(8712)
		ctx               = context.TODO()
		appProgramId      = ""
		bpfProg           = &bpfmaniov1alpha1.BpfNsProgram{}
		fakeUID           = "ef71d42c-aa21-48e8-a697-82391d801a81"
		attachPoint       = fmt.Sprintf("%s-%s-%s-%s",
			sanitize(target),
			sanitize(functionName),
			fakePodName,
			fakeContainerName,
		)
	)
	// A UprobeProgram object with metadata and spec.
	Uprobe := &bpfmaniov1alpha1.UprobeNsProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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
							"app": fakePodName,
						},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, Uprobe}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, Uprobe)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.UprobeNsProgramList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(Uprobe).WithStatusSubresource(&bpfmaniov1alpha1.BpfNsProgram{}).WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfmanClientFake()

	testContainers := FakeContainerGetter{
		containerList: &[]ContainerInfo{
			{
				podName:       fakePodName,
				containerName: fakeContainerName,
				pid:           fakePid,
			},
		},
	}

	rc := ReconcilerCommon[bpfmaniov1alpha1.BpfNsProgram, bpfmaniov1alpha1.BpfNsProgramList]{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
		Containers:   &testContainers,
	}
	npr := NamespaceProgramReconciler{
		ReconcilerCommon: rc,
	}

	// Set development Logger so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &UprobeNsProgramReconciler{NamespaceProgramReconciler: npr, ourNode: fakeNode}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	// First reconcile should create the bpf program object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check the BpfProgram Object was created successfully
	err = r.getBpfProgram(ctx, r, name, appProgramId, attachPoint, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProg.Finalizers[0])
	// owningConfig Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.BpfProgramOwner], name)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// uprobe function Annotation was correctly set
	require.Equal(t, bpfProg.Annotations[internal.UprobeNsProgramTarget], target)
	// Type is set
	require.Equal(t, r.getRecType(), bpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	bpfProg.UID = types.UID(fakeUID)
	err = cl.Update(ctx, bpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfProgram object's maps field and id annotation.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	pid32 := int32(fakePid)

	// Require no requeue
	require.False(t, res.Requeue)
	expectedLoadReq := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Kprobe.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(bpfProg.UID), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: &gobpfman.UprobeAttachInfo{
					FnName:       &functionName,
					Target:       target,
					Offset:       uint64(offset),
					Retprobe:     retprobe,
					ContainerPid: &pid32,
				},
			},
		},
	}

	// Check that the bpfProgram's programs was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId, attachPoint, bpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err := GetID(bpfProg)
	require.NoError(t, err)

	// Check the bpfLoadRequest was correctly Built
	if !cmp.Equal(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()) {
		cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform())
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// Third reconcile should set the status to loaded
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check that the bpfProgram's status was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId, attachPoint, bpfProg)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProg.Status.Conditions[0].Type)
}
