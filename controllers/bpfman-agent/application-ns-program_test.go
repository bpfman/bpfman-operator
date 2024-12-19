package bpfmanagent

import (
	"context"
	"fmt"
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	agenttestutils "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal/test-utils"
	"github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"
	gobpfman "github.com/bpfman/bpfman/clients/gobpfman/v1"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestBpfNsApplicationControllerCreate(t *testing.T) {
	var (
		// global config
		name                  = "fakeAppProgram"
		namespace             = "bpfman"
		bytecodePath          = "/tmp/hello.o"
		fakeNode              = testutils.NewNode("fake-control-plane")
		fakePodName           = "my-pod"
		fakeContainerName     = "my-container-1"
		fakePid               = int64(4490)
		ctx                   = context.TODO()
		bpfUprobeFunctionName = "test"
		uprobeFunctionName    = "malloc"
		uprobeTarget          = "libc"
		uprobeAppProgramId    = fmt.Sprintf("%s-%s-%s", "uprobe", sanitize(uprobeFunctionName), bpfUprobeFunctionName)
		uprobeBpfProg         = &bpfmaniov1alpha1.BpfNsProgram{}
		uprobeFakeUID         = "ef71d42c-aa21-48e8-a697-82391d801a80"
		uprobeOffset          = 0
		uprobeRetprobe        = false
		uprobeAttachPoint     = fmt.Sprintf("%s-%s-%s-%s",
			sanitize(uprobeTarget),
			sanitize(uprobeFunctionName),
			fakePodName,
			fakeContainerName,
		)
		// xdp program config
		bpfXdpFunctionName = "test"
		xdpFakeInt         = "eth0"
		xdpAppProgramId    = fmt.Sprintf("%s-%s", "xdp", bpfXdpFunctionName)
		xdpBpfProg         = &bpfmaniov1alpha1.BpfNsProgram{}
		xdpFakeUID         = "ef71d42c-aa21-48e8-a697-82391d801a82"
		xdpAttachPoint     = fmt.Sprintf("%s-%s-%s",
			xdpFakeInt,
			fakePodName,
			fakeContainerName,
		)
	)

	var fakeInts = []string{xdpFakeInt}

	// A AppProgram object with metadata and spec.
	App := &bpfmaniov1alpha1.BpfNsApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: bpfmaniov1alpha1.BpfNsApplicationSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			Programs: []bpfmaniov1alpha1.BpfNsApplicationProgram{
				{
					Type: bpfmaniov1alpha1.ProgTypeUprobe,
					Uprobe: &bpfmaniov1alpha1.UprobeNsProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfUprobeFunctionName,
						},
						FunctionName: uprobeFunctionName,
						Target:       uprobeTarget,
						Offset:       uint64(uprobeOffset),
						RetProbe:     uprobeRetprobe,
						Containers: bpfmaniov1alpha1.ContainerNsSelector{
							Pods: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "test",
								},
							},
						},
					},
				},
				{
					Type: bpfmaniov1alpha1.ProgTypeXDP,
					XDP: &bpfmaniov1alpha1.XdpNsProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfXdpFunctionName,
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
									"app": "test",
								},
							},
						},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, App}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, App)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsApplicationList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsApplication{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgramList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfNsProgram{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().
		WithStatusSubresource(App).
		WithStatusSubresource(&bpfmaniov1alpha1.BpfNsProgram{}).
		WithRuntimeObjects(objs...).Build()

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
		appOwner:     App,
	}
	npr := NamespaceProgramReconciler{
		ReconcilerCommon: rc,
	}

	// Set development Logger, so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &BpfNsApplicationReconciler{NamespaceProgramReconciler: npr, ourNode: fakeNode}

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
	ruprobe := &UprobeNsProgramReconciler{NamespaceProgramReconciler: npr, ourNode: fakeNode}
	err = r.getBpfProgram(ctx, ruprobe, name, uprobeAppProgramId, uprobeAttachPoint, uprobeBpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, uprobeBpfProg)
	// Finalizer is written
	require.Equal(t, internal.BpfNsApplicationControllerFinalizer, uprobeBpfProg.Finalizers[0])
	// owningConfig Label was correctly set
	require.Equal(t, name, uprobeBpfProg.Labels[internal.BpfProgramOwner])
	// node Label was correctly set
	require.Equal(t, fakeNode.Name, uprobeBpfProg.Labels[internal.K8sHostLabel])
	// uprobe target Annotation was correctly set
	require.Equal(t, uprobeTarget, uprobeBpfProg.Annotations[internal.UprobeNsProgramTarget])
	// Type is set
	require.Equal(t, r.getRecType(), uprobeBpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of BpfNsProgram with Fake UID since the fake API server won't
	uprobeBpfProg.UID = types.UID(uprobeFakeUID)
	err = cl.Update(ctx, uprobeBpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfNsProgram object's maps field and id annotation.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	pid32 := int32(fakePid)

	// Require no requeue
	require.False(t, res.Requeue)

	// do Uprobe Program
	expectedLoadReq := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfUprobeFunctionName,
		ProgramType: *internal.Kprobe.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(uprobeBpfProg.UID), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_UprobeAttachInfo{
				UprobeAttachInfo: &gobpfman.UprobeAttachInfo{
					FnName:       &uprobeFunctionName,
					Target:       uprobeTarget,
					Offset:       uint64(uprobeOffset),
					Retprobe:     uprobeRetprobe,
					ContainerPid: &pid32,
				},
			},
		},
	}

	// Check that the BpfNsProgram's programs was correctly updated
	err = r.getBpfProgram(ctx, ruprobe, name, uprobeAppProgramId, uprobeAttachPoint, uprobeBpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err := GetID(uprobeBpfProg)
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

	// Check that the BpfNsProgram's status was correctly updated
	err = r.getBpfProgram(ctx, ruprobe, name, uprobeAppProgramId, uprobeAttachPoint, uprobeBpfProg)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), uprobeBpfProg.Status.Conditions[0].Type)

	// do xdp program
	// First reconcile should create the bpf program object
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	rxdp := &XdpNsProgramReconciler{NamespaceProgramReconciler: npr, ourNode: fakeNode}
	err = r.getBpfProgram(ctx, rxdp, name, xdpAppProgramId, xdpAttachPoint, xdpBpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, xdpBpfProg)
	// Finalizer is written
	require.Equal(t, internal.BpfNsApplicationControllerFinalizer, xdpBpfProg.Finalizers[0])
	// owningConfig Label was correctly set
	require.Equal(t, name, xdpBpfProg.Labels[internal.BpfProgramOwner])
	// node Label was correctly set
	require.Equal(t, fakeNode.Name, xdpBpfProg.Labels[internal.K8sHostLabel])
	// Type is set
	require.Equal(t, r.getRecType(), xdpBpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of BpfNsProgram with Fake UID since the fake API server won't
	xdpBpfProg.UID = types.UID(xdpFakeUID)
	err = cl.Update(ctx, xdpBpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfNsProgram object's maps field and id annotation.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)
	netns := fmt.Sprintf("/host/proc/%d/ns/net", fakePid)

	expectedLoadReq = &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfXdpFunctionName,
		ProgramType: *internal.Xdp.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(xdpBpfProg.UID), internal.ProgramNameKey: name},
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

	// Check that the BpfNsProgram's programs was correctly updated
	err = r.getBpfProgram(ctx, rxdp, name, xdpAppProgramId, xdpAttachPoint, xdpBpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err = GetID(xdpBpfProg)
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

	// Check that the BpfNsProgram's status was correctly updated
	err = r.getBpfProgram(ctx, rxdp, name, xdpAppProgramId, xdpAttachPoint, xdpBpfProg)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), xdpBpfProg.Status.Conditions[0].Type)
}
