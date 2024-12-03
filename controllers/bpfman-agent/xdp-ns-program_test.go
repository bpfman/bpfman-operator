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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Runs the XdpProgramControllerCreate test.  If multiInterface = true, it
// installs the program on two interfaces.  If multiCondition == true, it runs
// it with an error case in which the program object has multiple conditions.
func xdpNsProgramControllerCreate(t *testing.T, multiInterface bool, multiCondition bool) {
	var (
		name              = "fakeXdpProgram"
		namespace         = "my-namespace"
		bytecodePath      = "/tmp/hello.o"
		bpfFunctionName   = "test"
		fakeNode          = testutils.NewNode("fake-control-plane")
		fakePodName       = "my-pod"
		fakeContainerName = "my-container-1"
		fakePid           = int64(12201)
		fakeInt0          = "eth0"
		fakeInt1          = "eth1"
		ctx               = context.TODO()
		appProgramId0     = ""
		appProgramId1     = ""
		bpfProgEth0       = &bpfmaniov1alpha1.BpfNsProgram{}
		bpfProgEth1       = &bpfmaniov1alpha1.BpfNsProgram{}
		fakeUID0          = "ef71d42c-aa21-48e8-a697-82391d801a80"
		fakeUID1          = "ef71d42c-aa21-48e8-a697-82391d801a81"
		attachPoint0      = fmt.Sprintf("%s-%s-%s",
			fakeInt0,
			fakePodName,
			fakeContainerName,
		)
		attachPoint1 = fmt.Sprintf("%s-%s-%s",
			fakeInt1,
			fakePodName,
			fakeContainerName,
		)
	)

	var fakeInts []string
	if multiInterface {
		fakeInts = []string{fakeInt0, fakeInt1}
	} else {
		fakeInts = []string{fakeInt0}
	}

	// A XdpNsProgram object with metadata and spec.
	xdp := &bpfmaniov1alpha1.XdpNsProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: bpfmaniov1alpha1.XdpNsProgramSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			XdpNsProgramInfo: bpfmaniov1alpha1.XdpNsProgramInfo{
				BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
					BpfFunctionName: bpfFunctionName,
				},
				InterfaceSelector: bpfmaniov1alpha1.InterfaceSelector{
					Interfaces: &fakeInts,
				},
				Priority: 0,
				ProceedOn: []bpfmaniov1alpha1.XdpProceedOnValue{bpfmaniov1alpha1.XdpProceedOnValue("pass"),
					bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return"),
				},
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
	objs := []runtime.Object{fakeNode, xdp}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, xdp)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.XdpNsProgramList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgram{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgramList{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(xdp).WithStatusSubresource(&bpfmaniov1alpha1.BpfNsProgram{}).WithRuntimeObjects(objs...).Build()

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
	r := &XdpNsProgramReconciler{NamespaceProgramReconciler: npr, ourNode: fakeNode}

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

	// Check the first BpfProgram Object was created successfully
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

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	bpfProgEth0.UID = types.UID(fakeUID0)
	err = cl.Update(ctx, bpfProgEth0)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Requests for the first bpfProgram.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	uuid0 := string(bpfProgEth0.UID)
	netns := fmt.Sprintf("/host/proc/%d/ns/net", fakePid)

	expectedLoadReq0 := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFunctionName,
		ProgramType: *internal.Xdp.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: uuid0, internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_XdpAttachInfo{
				XdpAttachInfo: &gobpfman.XDPAttachInfo{
					Priority:  0,
					Iface:     fakeInts[0],
					ProceedOn: []int32{2, 31},
					Netns:     &netns,
				},
			},
		},
	}

	// Check that the bpfProgram's maps was correctly updated
	err = r.getBpfProgram(ctx, r, name, appProgramId0, attachPoint0, bpfProgEth0)
	require.NoError(t, err)

	// prog ID should already have been set
	id0, err := GetID(bpfProgEth0)
	require.NoError(t, err)

	// Check the bpfLoadRequest was correctly built
	if !cmp.Equal(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()) {
		t.Logf("Diff %v", cmp.Diff(expectedLoadReq0, cli.LoadRequests[int(*id0)], protocmp.Transform()))
		t.Fatal("Built bpfman LoadRequest does not match expected")
	}

	// NOTE: THIS IS A TEST FOR AN ERROR PATH. THERE SHOULD NEVER BE MORE THAN
	// ONE CONDITION.
	if multiCondition {
		// Add some random conditions and verify that the condition still gets
		// updated correctly.
		meta.SetStatusCondition(&bpfProgEth0.Status.Conditions, bpfmaniov1alpha1.BpfProgCondBytecodeSelectorError.Condition())
		if err := r.Status().Update(ctx, bpfProgEth0); err != nil {
			r.Logger.V(1).Info("failed to set KprobeProgram object status")
		}
		meta.SetStatusCondition(&bpfProgEth0.Status.Conditions, bpfmaniov1alpha1.BpfProgCondNotSelected.Condition())
		if err := r.Status().Update(ctx, bpfProgEth0); err != nil {
			r.Logger.V(1).Info("failed to set KprobeProgram object status")
		}
		// Make sure we have 2 conditions
		require.Equal(t, 2, len(bpfProgEth0.Status.Conditions))
	}

	// Third reconcile should update the bpfPrograms status to loaded
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// Get program object
	err = r.getBpfProgram(ctx, r, name, appProgramId0, attachPoint0, bpfProgEth0)
	require.NoError(t, err)

	// Check that the bpfProgram's status was correctly updated
	// Make sure we only have 1 condition now
	require.Equal(t, 1, len(bpfProgEth0.Status.Conditions))
	// Make sure it's the right one.
	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth0.Status.Conditions[0].Type)

	if multiInterface {
		// Fourth reconcile should create the second bpfProgram.
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}

		// Require no requeue
		require.False(t, res.Requeue)

		// Check the Second BpfProgram Object was created successfully
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
		// Require no requeue
		require.False(t, res.Requeue)

		// Update UID of bpfProgram with Fake UID since the fake API server won't
		bpfProgEth1.UID = types.UID(fakeUID1)
		err = cl.Update(ctx, bpfProgEth1)
		require.NoError(t, err)

		// Fifth reconcile should create the second bpfProgram.
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}

		// Require no requeue
		require.False(t, res.Requeue)

		// Sixth reconcile should update the second bpfProgram's status.
		res, err = r.Reconcile(ctx, req)
		if err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}

		// Require no requeue
		require.False(t, res.Requeue)

		uuid1 := string(bpfProgEth1.UID)
		netns := fmt.Sprintf("/host/proc/%d/ns/net", fakePid)

		expectedLoadReq1 := &gobpfman.LoadRequest{
			Bytecode: &gobpfman.BytecodeLocation{
				Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
			},
			Name:        bpfFunctionName,
			ProgramType: *internal.Xdp.Uint32(),
			Metadata:    map[string]string{internal.UuidMetadataKey: uuid1, internal.ProgramNameKey: name},
			MapOwnerId:  nil,
			Attach: &gobpfman.AttachInfo{
				Info: &gobpfman.AttachInfo_XdpAttachInfo{
					XdpAttachInfo: &gobpfman.XDPAttachInfo{
						Priority:  0,
						Iface:     fakeInts[1],
						ProceedOn: []int32{2, 31},
						Netns:     &netns,
					},
				},
			},
		}

		// Check that the bpfProgram's maps was correctly updated
		err = r.getBpfProgram(ctx, r, name, appProgramId1, attachPoint1, bpfProgEth1)
		require.NoError(t, err)

		// prog ID should already have been set
		id1, err := GetID(bpfProgEth1)
		require.NoError(t, err)

		// Check the bpfLoadRequest was correctly built
		if !cmp.Equal(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()) {
			t.Logf("Diff %v", cmp.Diff(expectedLoadReq1, cli.LoadRequests[int(*id1)], protocmp.Transform()))
			t.Fatal("Built bpfman LoadRequest does not match expected")
		}

		// Get program object
		err = r.getBpfProgram(ctx, r, name, appProgramId1, attachPoint1, bpfProgEth1)
		require.NoError(t, err)

		// Check that the bpfProgram's status was correctly updated
		// Make sure we only have 1 condition now
		require.Equal(t, 1, len(bpfProgEth1.Status.Conditions))
		// Make sure it's the right one.
		require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), bpfProgEth1.Status.Conditions[0].Type)
	}
}

func TestXdpNsProgramControllerCreate(t *testing.T) {
	xdpNsProgramControllerCreate(t, false, false)
}

func TestXdpNsProgramControllerCreateMultiIntf(t *testing.T) {
	xdpNsProgramControllerCreate(t, true, false)
}

func TestXdpNsUpdateStatus(t *testing.T) {
	xdpNsProgramControllerCreate(t, false, true)
}
