package bpfmanagent

import (
	"context"
	"fmt"
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	bpfmanagentinternal "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal"
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

func TestBpfApplicationControllerCreate(t *testing.T) {
	var (
		// global config
		name         = "fakeAppProgram"
		namespace    = "bpfman"
		bytecodePath = "/tmp/hello.o"
		fakeNode     = testutils.NewNode("fake-control-plane")
		ctx          = context.TODO()
		// fentry program config
		bpfFentryFunctionName = "fentry_test"
		fentryFunctionName    = "do_unlinkat"
		fentryAppProgramId    = fmt.Sprintf("%s-%s", "fentry", sanitize(fentryFunctionName))
		fentryAttachPoint     = sanitize(fentryFunctionName)
		fentryBpfProg         = &bpfmaniov1alpha1.BpfProgram{}
		fentryFakeUID         = "ef71d42c-aa21-48e8-a697-82391d801a81"
		// kprobe program config
		bpfKprobeFunctionName       = "kprobe_test"
		kprobeFunctionName          = "try_to_wake_up"
		kprobeAppProgramId          = fmt.Sprintf("%s-%s", "kprobe", sanitize(kprobeFunctionName))
		kprobeAttachPoint           = sanitize(kprobeFunctionName)
		kprobeBpfProg               = &bpfmaniov1alpha1.BpfProgram{}
		kprobeFakeUID               = "ef71d42c-aa21-48e8-a697-82391d801a82"
		kprobeOffset                = 0
		kprobeRetprobe              = false
		kprobecontainerpid    int32 = 0
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
						FunctionName: fentryFunctionName,
					},
				},
				{
					Type: bpfmaniov1alpha1.ProgTypeKprobe,
					Kprobe: &bpfmaniov1alpha1.KprobeProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfKprobeFunctionName,
						},
						FunctionName: kprobeFunctionName,
						Offset:       uint64(kprobeOffset),
						RetProbe:     kprobeRetprobe,
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
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplication{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgramList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgram{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(App).WithStatusSubresource(&bpfmaniov1alpha1.BpfProgram{}).WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfmanClientFake()

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
		appOwner:     App,
	}

	// Set development Logger, so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &BpfApplicationReconciler{ReconcilerCommon: rc, ourNode: fakeNode}

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
	err = rc.getBpfProgram(ctx, name, fentryAppProgramId, fentryAttachPoint, fentryBpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, fentryBpfProg)
	// Finalizer is written
	require.Equal(t, internal.BpfApplicationControllerFinalizer, fentryBpfProg.Finalizers[0])
	// owningConfig Label was correctly set
	require.Equal(t, name, fentryBpfProg.Labels[internal.BpfProgramOwner])
	// node Label was correctly set
	require.Equal(t, fakeNode.Name, fentryBpfProg.Labels[internal.K8sHostLabel])
	// fentry function Annotation was correctly set
	require.Equal(t, fentryFunctionName, fentryBpfProg.Annotations[internal.FentryProgramFunction])
	// Type is set
	require.Equal(t, r.getRecType(), fentryBpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	fentryBpfProg.UID = types.UID(fentryFakeUID)
	err = cl.Update(ctx, fentryBpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfProgram object's maps field and id annotation.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	// do Fentry Program
	expectedLoadReq := &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfFentryFunctionName,
		ProgramType: *internal.Tracing.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(fentryBpfProg.UID), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_FentryAttachInfo{
				FentryAttachInfo: &gobpfman.FentryAttachInfo{
					FnName: fentryFunctionName,
				},
			},
		},
	}

	// Check that the bpfProgram's programs was correctly updated
	err = rc.getBpfProgram(ctx, name, fentryAppProgramId, fentryAttachPoint, fentryBpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err := bpfmanagentinternal.GetID(fentryBpfProg)
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
	err = rc.getBpfProgram(ctx, name, fentryAppProgramId, fentryAttachPoint, fentryBpfProg)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), fentryBpfProg.Status.Conditions[0].Type)

	// do kprobe program
	// First reconcile should create the bpf program object
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	err = rc.getBpfProgram(ctx, name, kprobeAppProgramId, kprobeAttachPoint, kprobeBpfProg)
	require.NoError(t, err)

	require.NotEmpty(t, kprobeBpfProg)
	// Finalizer is written
	require.Equal(t, internal.BpfApplicationControllerFinalizer, kprobeBpfProg.Finalizers[0])
	// owningConfig Label was correctly set
	require.Equal(t, name, kprobeBpfProg.Labels[internal.BpfProgramOwner])
	// node Label was correctly set
	require.Equal(t, fakeNode.Name, kprobeBpfProg.Labels[internal.K8sHostLabel])
	// fentry function Annotation was correctly set
	require.Equal(t, kprobeFunctionName, kprobeBpfProg.Annotations[internal.KprobeProgramFunction])
	// Type is set
	require.Equal(t, r.getRecType(), kprobeBpfProg.Spec.Type)
	// Require no requeue
	require.False(t, res.Requeue)

	// Update UID of bpfProgram with Fake UID since the fake API server won't
	kprobeBpfProg.UID = types.UID(kprobeFakeUID)
	err = cl.Update(ctx, kprobeBpfProg)
	require.NoError(t, err)

	// Second reconcile should create the bpfman Load Request and update the
	// BpfProgram object's maps field and id annotation.
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Require no requeue
	require.False(t, res.Requeue)

	expectedLoadReq = &gobpfman.LoadRequest{
		Bytecode: &gobpfman.BytecodeLocation{
			Location: &gobpfman.BytecodeLocation_File{File: bytecodePath},
		},
		Name:        bpfKprobeFunctionName,
		ProgramType: *internal.Kprobe.Uint32(),
		Metadata:    map[string]string{internal.UuidMetadataKey: string(kprobeBpfProg.UID), internal.ProgramNameKey: name},
		MapOwnerId:  nil,
		Attach: &gobpfman.AttachInfo{
			Info: &gobpfman.AttachInfo_KprobeAttachInfo{
				KprobeAttachInfo: &gobpfman.KprobeAttachInfo{
					FnName:       kprobeFunctionName,
					Offset:       uint64(kprobeOffset),
					Retprobe:     kprobeRetprobe,
					ContainerPid: &kprobecontainerpid,
				},
			},
		},
	}

	// Check that the bpfProgram's programs was correctly updated
	err = rc.getBpfProgram(ctx, name, kprobeAppProgramId, kprobeAttachPoint, kprobeBpfProg)
	require.NoError(t, err)

	// prog ID should already have been set
	id, err = bpfmanagentinternal.GetID(kprobeBpfProg)
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
	err = rc.getBpfProgram(ctx, name, kprobeAppProgramId, kprobeAttachPoint, kprobeBpfProg)
	require.NoError(t, err)

	require.Equal(t, string(bpfmaniov1alpha1.BpfProgCondLoaded), kprobeBpfProg.Status.Conditions[0].Type)
}
