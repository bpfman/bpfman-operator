package appagent

import (
	"context"
	"reflect"
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	agenttestutils "github.com/bpfman/bpfman-operator/controllers/app-agent/internal/test-utils"
	"github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"
	"github.com/stretchr/testify/require"
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
		appProgramName        = "fakeAppProgram"
		namespace             = "bpfman"
		bytecodePath          = "/tmp/hello.o"
		xdpBpfFunctionName    = "XdpTest"
		tcxBpfFunctionName    = "TcxTest"
		fentryBpfFunctionName = "FentryTest"
		priority              = 50
		fakeNode              = testutils.NewNode("fake-control-plane")
		fakeInt0              = "eth0"
		// fakeInt1        = "eth1"
		ctx = context.TODO()
		// appProgramId0   = ""
		// attachPoint0    = fakeInt0
		// appProgramId1   = ""
		// attachPoint1    = fakeInt1
		// bpfProgEth0     = &bpfmaniov1alpha1.BpfProgram{}
		// bpfProgEth1     = &bpfmaniov1alpha1.BpfProgram{}
		// fakeUID0        = "ef71d42c-aa21-48e8-a697-82391d801a80"
		// fakeUID1        = "ef71d42c-aa21-48e8-a697-82391d801a81"
	)

	fakeInts := []string{fakeInt0}

	interfaceSelector := bpfmaniov1alpha1.InterfaceSelector{
		Interfaces: &fakeInts,
	}

	proceedOn := []bpfmaniov1alpha1.XdpProceedOnValue{bpfmaniov1alpha1.XdpProceedOnValue("pass"),
		bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return")}

	xdpAttachInfo := bpfmaniov1alpha1.XdpAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Priority:          int32(priority),
		ProceedOn:         proceedOn,
	}

	// A AppProgram object with metadata and spec.
	programMap := make(map[string]bpfmaniov1alpha1.BpfApplicationProgram)

	programMap[xdpBpfFunctionName] = bpfmaniov1alpha1.BpfApplicationProgram{
		Type: bpfmaniov1alpha1.ProgTypeXDP,
		XDP: &bpfmaniov1alpha1.XdpProgramInfo{
			BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
				BpfFunctionName:     xdpBpfFunctionName,
				OldMapOwnerSelector: metav1.LabelSelector{},
			},
			AttachPoints: []bpfmaniov1alpha1.XdpAttachInfo{xdpAttachInfo},
		},
	}

	tcxAttachInfo := bpfmaniov1alpha1.TcxAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Direction:         "ingress",
		Priority:          int32(priority),
	}

	programMap[tcxBpfFunctionName] = bpfmaniov1alpha1.BpfApplicationProgram{
		Type: bpfmaniov1alpha1.ProgTypeTCX,
		TCX: &bpfmaniov1alpha1.TcxProgramInfo{
			BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
				BpfFunctionName:     tcxBpfFunctionName,
				OldMapOwnerSelector: metav1.LabelSelector{},
			},
			AttachPoints: []bpfmaniov1alpha1.TcxAttachInfo{tcxAttachInfo},
		},
	}

	programMap[fentryBpfFunctionName] = bpfmaniov1alpha1.BpfApplicationProgram{
		Type: bpfmaniov1alpha1.ProgTypeFentry,
		Fentry: &bpfmaniov1alpha1.FentryProgramInfo{
			BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
				BpfFunctionName:     fentryBpfFunctionName,
				OldMapOwnerSelector: metav1.LabelSelector{},
			},
			FentryAttachInfo: bpfmaniov1alpha1.FentryAttachInfo{Attach: true},
		},
	}

	bpfApp := &bpfmaniov1alpha1.BpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: appProgramName,
		},
		Spec: bpfmaniov1alpha1.BpfApplicationSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			Programs: programMap,
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, bpfApp}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, bpfApp)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplication{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationStateList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationState{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(bpfApp).WithStatusSubresource(&bpfmaniov1alpha1.BpfApplicationState{}).WithRuntimeObjects(objs...).Build()

	cli := agenttestutils.NewBpfmanClientFake()

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
		ourNode:      fakeNode,
	}

	// Set development Logger, so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &BpfApplicationReconciler{
		ReconcilerCommon: rc,
	}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      appProgramName,
			Namespace: namespace,
		},
	}

	// First reconcile should create the BpfApplicationState object
	r.Logger.Info("First reconcile")
	res, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	r.Logger.Info("First reconcile", "res:", res, "err:", err)

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfProgram Object was created successfully
	bpfAppState, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	require.Equal(t, 1, len(bpfAppState.Status.Conditions))
	require.Equal(t, string(bpfmaniov1alpha1.ProgramReconcileSuccess), bpfAppState.Status.Conditions[0].Type)

	require.Equal(t, fakeNode.Name, bpfAppState.Labels[internal.K8sHostLabel])

	require.Equal(t, appProgramName, bpfAppState.Labels[internal.BpfAppStateOwner])

	require.Equal(t, internal.BpfApplicationControllerFinalizer, bpfAppState.Finalizers[0])

	// Do a 2nd reconcile and make sure it doesn't change
	r.Logger.Info("Second reconcile")
	res, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Require no requeue
	require.False(t, res.Requeue)

	r.Logger.Info("Second reconcile", "res:", res, "err:", err)

	// Check the BpfProgram Object was created successfully
	bpfAppState2, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	// Check that the bpfAppState was not updated
	require.True(t, reflect.DeepEqual(bpfAppState, bpfAppState2))

	currentProgram := programMap[xdpBpfFunctionName]

	attachPoint := bpfAppState2.Spec.Programs[xdpBpfFunctionName].XDP.AttachPoints[0]

	xdpReconciler := &XdpProgramReconciler{
		ReconcilerCommon: rc,
		ProgramReconcilerCommon: ProgramReconcilerCommon{
			appCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
					Path: &bytecodePath,
				},
			},
			currentProgram:      &currentProgram,
			currentProgramState: &bpfmaniov1alpha1.BpfApplicationProgramState{},
		},
		currentAttachPoint: &attachPoint,
	}

	loadRequest, err := xdpReconciler.getLoadRequest(nil)
	require.NoError(t, err)

	require.Equal(t, xdpBpfFunctionName, loadRequest.Name)
	require.Equal(t, uint32(6), loadRequest.ProgramType)
}
