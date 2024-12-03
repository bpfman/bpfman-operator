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
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"

	internal "github.com/bpfman/bpfman-operator/internal"

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

func TestTcxNsProgramControllerCreate(t *testing.T) {
	var (
		name              = "fakeTcxProgram"
		namespace         = "bpfman"
		bytecodePath      = "/tmp/hello.o"
		bpfFunctionName   = "test"
		direction         = "ingress"
		fakeNode          = testutils.NewNode("fake-control-plane")
		fakePodName       = "my-pod"
		fakeContainerName = "my-container-1"
		fakePid           = int64(5457)
		fakeInt           = "eth0"
		ctx               = context.TODO()
		appProgramId      = ""
		bpfProg           = &bpfmaniov1alpha1.BpfNsProgram{}
		fakeUID           = "ef71d42c-aa21-48e8-a697-82391d801a81"
		attachPoint       = fmt.Sprintf("%s-%s-%s-%s",
			fakeInt,
			direction,
			fakePodName,
			fakeContainerName,
		)
	)
	// A TcxNsProgram object with metadata and spec.
	tcx := &bpfmaniov1alpha1.TcxNsProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: bpfmaniov1alpha1.TcxNsProgramSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			TcxNsProgramInfo: bpfmaniov1alpha1.TcxNsProgramInfo{
				BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
					BpfFunctionName: bpfFunctionName,
				},
				InterfaceSelector: bpfmaniov1alpha1.InterfaceSelector{
					Interfaces: &[]string{fakeInt},
				},
				Priority:  0,
				Direction: direction,
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

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, tcx}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, tcx)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.TcxNsProgramList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(tcx).WithStatusSubresource(&bpfmaniov1alpha1.BpfNsProgram{}).WithRuntimeObjects(objs...).Build()

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
	r := &TcxNsProgramReconciler{NamespaceProgramReconciler: npr, ourNode: fakeNode}

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

	// Check the BpfNsProgram Object was created successfully
	err = r.getBpfProgram(ctx, r, name, appProgramId, attachPoint, bpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProg)
	// owningConfig Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.BpfProgramOwner], name)
	// node Label was correctly set
	require.Equal(t, bpfProg.Labels[internal.K8sHostLabel], fakeNode.Name)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProg.Finalizers[0])
	// Type is set
	require.Equal(t, r.getRecType(), bpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of BpfNsProgram with Fake UID since the fake API server won't
	bpfProg.UID = types.UID(fakeUID)
	err = cl.Update(ctx, bpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfNsProgram object's 'Programs' field.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)
	uuid := string(bpfProg.UID)
	netns := fmt.Sprintf("/host/proc/%d/ns/net", fakePid)

	expectedLoadReq := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Tc.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(uuid), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcxAttachInfo{
				TcxAttachInfo: &gobpfman.TCXAttachInfo{
					Iface:     fakeInt,
					Priority:  tcx.Spec.Priority,
					Direction: direction,
					Netns:     &netns,
				},
			},
		},
	}

	// Check that the BpfNsProgram's programs was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId, attachPoint, bpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err := GetID(bpfProg)
	require.NoError(t, err)

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq, cli.LoadRequests[int(*id)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// Third reconcile should update the BpfNsPrograms status to loaded
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check that the BpfNsProgram's status was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId, attachPoint, bpfProg)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProg.Status.Conditions[0].Type)
}

func TestTcxNsProgramControllerCreateMultiIntf(t *testing.T) {
	var (
		name              = "fakeTcProgram"
		namespace         = "bpfman"
		bytecodePath      = "/tmp/hello.o"
		bpfFunctionName   = "test"
		direction         = "ingress"
		fakeNode          = testutils.NewNode("fake-control-plane")
		fakePodName       = "my-pod"
		fakeContainerName = "my-container-1"
		fakePid           = int64(9574)
		fakeInts          = []string{"eth0", "eth1"}
		ctx               = context.TODO()
		appProgramId0     = ""
		appProgramId1     = ""
		bpfProgEth0       = &bpfmaniov1alpha1.BpfNsProgram{}
		bpfProgEth1       = &bpfmaniov1alpha1.BpfNsProgram{}
		fakeUID0          = "ef71d42c-aa21-48e8-a697-82391d801a80"
		fakeUID1          = "ef71d42c-aa21-48e8-a697-82391d801a81"
		attachPoint0      = fmt.Sprintf("%s-%s-%s-%s",
			fakeInts[0],
			direction,
			fakePodName,
			fakeContainerName,
		)
		attachPoint1 = fmt.Sprintf("%s-%s-%s-%s",
			fakeInts[1],
			direction,
			fakePodName,
			fakeContainerName,
		)
	)
	// A TcProgram object with metadata and spec.
	tcx := &bpfmaniov1alpha1.TcxNsProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: bpfmaniov1alpha1.TcxNsProgramSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			TcxNsProgramInfo: bpfmaniov1alpha1.TcxNsProgramInfo{
				BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
					BpfFunctionName: bpfFunctionName,
				},
				InterfaceSelector: bpfmaniov1alpha1.InterfaceSelector{
					Interfaces: &fakeInts,
				},
				Priority:  10,
				Direction: direction,
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

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, tcx}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, tcx)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(tcx).WithStatusSubresource(&bpfmaniov1alpha1.BpfNsProgram{}).WithRuntimeObjects(objs...).Build()

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
	r := &TcxNsProgramReconciler{NamespaceProgramReconciler: npr, ourNode: fakeNode}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	// First reconcile should create the first bpf program object
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check the first BpfNsProgram Object was created successfully
	err = r.getBpfProgram(ctx, r, name, appProgramId0, attachPoint0, bpfProgEth0)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProgEth0)
	// owningConfig Label was correctly set
	require.Equal(t, bpfProgEth0.Labels[internal.BpfProgramOwner], name)
	// node Label was correctly set
	require.Equal(t, bpfProgEth0.Labels[internal.K8sHostLabel], fakeNode.Name)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProgEth0.Finalizers[0])
	// Type is set
	require.Equal(t, r.getRecType(), bpfProgEth0.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of BpfNsProgram with Fake UID since the fake API server won't
	bpfProgEth0.UID = types.UID(fakeUID0)
	err = cl.Update(ctx, bpfProgEth0)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Requests for the first BpfNsProgram and update the prog id.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Third reconcile should set the second bpf program object's status.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Fourth reconcile should create the bpfman Load Requests for the second BpfNsProgram.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the Second BpfNsProgram Object was created successfully
	err = r.getBpfProgram(ctx, r, name, appProgramId1, attachPoint1, bpfProgEth1)
	require.NoError(t, err)

	require.NotEmpty(t, bpfProgEth1)
	// owningConfig Label was correctly set
	require.Equal(t, bpfProgEth1.Labels[internal.BpfProgramOwner], name)
	// node Label was correctly set
	require.Equal(t, bpfProgEth1.Labels[internal.K8sHostLabel], fakeNode.Name)
	// Finalizer is written
	require.Equal(t, r.getFinalizer(), bpfProgEth1.Finalizers[0])
	// Type is set
	require.Equal(t, r.getRecType(), bpfProgEth1.Spec.Type)

	// Update UID of BpfNsProgram with Fake UID since the fake API server won't
	bpfProgEth1.UID = types.UID(fakeUID1)
	err = cl.Update(ctx, bpfProgEth1)
	require.NoError(t, err)

	// Fifth reconcile should create the second BpfNsProgram.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Sixth reconcile should update the second BpfNsProgram's status.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	uuid0 := string(bpfProgEth0.UID)
	netns := fmt.Sprintf("/host/proc/%d/ns/net", fakePid)

	expectedLoadReq0 := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Tc.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(uuid0), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcxAttachInfo{
				TcxAttachInfo: &gobpfman.TCXAttachInfo{
					Iface:     fakeInts[0],
					Priority:  tcx.Spec.Priority,
					Direction: direction,
					Netns:     &netns,
				},
			},
		},
	}

	uuid1 := string(bpfProgEth1.UID)

	expectedLoadReq1 := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Tc.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(uuid1), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_TcxAttachInfo{
				TcxAttachInfo: &gobpfman.TCXAttachInfo{
					Iface:     fakeInts[1],
					Priority:  tcx.Spec.Priority,
					Direction: direction,
					Netns:     &netns,
				},
			},
		},
	}

	// Check that the BpfNsProgram's maps was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId0, attachPoint0, bpfProgEth0)
	require.NoError(t, err)

	// prog ID should already have been set
	id0, err := GetID(bpfProgEth0)
	require.NoError(t, err)

	// Check that the BpfNsProgram's maps was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId1, attachPoint1, bpfProgEth1)
	require.NoError(t, err)

	// prog ID should already have been set
	id1, err := GetID(bpfProgEth1)
	require.NoError(t, err)

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// Check that the BpfNsProgram's maps was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId0, attachPoint0, bpfProgEth0)
	require.NoError(t, err)

	// Check that the BpfNsProgram's maps was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId1, attachPoint1, bpfProgEth1)
	require.NoError(t, err)

	// Third reconcile should update the BpfNsPrograms status to loaded
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Check that the BpfNsProgram's status was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId0, attachPoint0, bpfProgEth0)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth0.Status.Conditions[0].Type)

	// Check that the BpfNsProgram's status was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId1, attachPoint1, bpfProgEth1)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth1.Status.Conditions[0].Type)
}
