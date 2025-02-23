/*
Copyright 2025.

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
	"reflect"
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	agenttestutils "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal/test-utils"
	"github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestBpfApplicationControllerCreate(t *testing.T) {
	var (
		// global config
		appProgramName = "fakeAppProgram"
		namespace      = "bpfman"
		bytecodePath   = "/tmp/hello.o"

		fentryBpfFunctionName     = "FentryTest"
		fexitBpfFunctionName      = "FexitTest"
		uprobeBpfFunctionName     = "UprobeTest"
		kprobeBpfFunctionName     = "KprobeTest"
		tracepointBpfFunctionName = "TracepointTest"
		tcBpfFunctionName         = "TcTest"
		tcxBpfFunctionName        = "TcxTest"
		xdpBpfFunctionName        = "XdpTest"

		direction  = "ingress"
		AttachName = "AttachNameTest"
		priority   = 50
		fakeNode   = testutils.NewNode("fake-control-plane")
		fakeInt0   = "eth0"
		fakePid    = int32(1000)
		// fakeInt1        = "eth1"
		ctx = context.TODO()
	)

	programs := []bpfmaniov1alpha1.BpfApplicationProgram{}

	fakeInts := []string{fakeInt0}

	interfaceSelector := bpfmaniov1alpha1.InterfaceSelector{
		Interfaces: &fakeInts,
	}

	// Keep XDP as the first program because there's a test that assumes it's at programs[0].
	proceedOn := []bpfmaniov1alpha1.XdpProceedOnValue{bpfmaniov1alpha1.XdpProceedOnValue("pass"),
		bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return")}
	xdpAttachInfo := bpfmaniov1alpha1.XdpAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Priority:          int32(priority),
		ProceedOn:         proceedOn,
	}
	xdpProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: xdpBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeXDP,
		XDP: &bpfmaniov1alpha1.XdpProgramInfo{
			AttachPoints: []bpfmaniov1alpha1.XdpAttachInfo{xdpAttachInfo},
		},
	}
	programs = append(programs, xdpProgram)

	fentryProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: fentryBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeFentry,
		Fentry: &bpfmaniov1alpha1.FentryProgramInfo{
			FentryLoadInfo: bpfmaniov1alpha1.FentryLoadInfo{
				FunctionName: AttachName,
			},
			FentryAttachInfo: bpfmaniov1alpha1.FentryAttachInfo{
				Attach: true,
			},
		},
	}
	programs = append(programs, fentryProgram)

	fexitProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: fexitBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeFexit,
		Fexit: &bpfmaniov1alpha1.FexitProgramInfo{
			FexitLoadInfo: bpfmaniov1alpha1.FexitLoadInfo{
				FunctionName: AttachName,
			},
			FexitAttachInfo: bpfmaniov1alpha1.FexitAttachInfo{
				Attach: true,
			},
		},
	}
	programs = append(programs, fexitProgram)

	kprobeProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: kprobeBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeKprobe,
		Kprobe: &bpfmaniov1alpha1.KprobeProgramInfo{
			AttachPoints: []bpfmaniov1alpha1.KprobeAttachInfo{
				{
					FunctionName: AttachName,
					Offset:       200,
					Retprobe:     false,
				},
			},
		},
	}
	programs = append(programs, kprobeProgram)

	uprobeProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: uprobeBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeUprobe,
		Uprobe: &bpfmaniov1alpha1.UprobeProgramInfo{
			AttachPoints: []bpfmaniov1alpha1.UprobeAttachInfo{
				{
					FunctionName: AttachName,
					Offset:       200,
					Target:       "/bin/bash",
					Retprobe:     false,
					Pid:          &fakePid,
				},
			},
		},
	}
	programs = append(programs, uprobeProgram)

	tracepointProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: tracepointBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeTracepoint,
		Tracepoint: &bpfmaniov1alpha1.TracepointProgramInfo{
			AttachPoints: []bpfmaniov1alpha1.TracepointAttachInfo{
				{
					Name: AttachName,
				},
			},
		},
	}
	programs = append(programs, tracepointProgram)

	tcAttachInfo := bpfmaniov1alpha1.TcAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Direction:         direction,
		Priority:          int32(priority),
	}
	tcProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: tcBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeTC,
		TC: &bpfmaniov1alpha1.TcProgramInfo{
			AttachPoints: []bpfmaniov1alpha1.TcAttachInfo{tcAttachInfo},
		},
	}
	programs = append(programs, tcProgram)

	tcxAttachInfo := bpfmaniov1alpha1.TcxAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Direction:         "ingress",
		Priority:          int32(priority),
	}
	tcxProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		BpfFunctionName: tcxBpfFunctionName,
		Type:            bpfmaniov1alpha1.ProgTypeTCX,
		TCX: &bpfmaniov1alpha1.TcxProgramInfo{
			AttachPoints: []bpfmaniov1alpha1.TcxAttachInfo{tcxAttachInfo},
		},
	}
	programs = append(programs, tcxProgram)

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
			Programs: programs,
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

	// Check the BpfApplicationState Object was created successfully
	bpfAppState, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	require.Equal(t, 1, len(bpfAppState.Status.Conditions))
	require.Equal(t, string(bpfmaniov1alpha1.ProgramNotYetLoaded), bpfAppState.Status.Conditions[0].Type)

	require.Equal(t, fakeNode.Name, bpfAppState.Labels[internal.K8sHostLabel])

	require.Equal(t, appProgramName, bpfAppState.Labels[internal.BpfAppStateOwner])

	require.Equal(t, internal.BpfApplicationControllerFinalizer, bpfAppState.Finalizers[0])

	// Do a second reconcile to make sure that the programs are loaded and attached.
	r.Logger.Info("Second reconcile")
	res, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	r.Logger.Info("Second reconcile", "res:", res, "err:", err)

	// Require no requeue
	require.False(t, res.Requeue)

	// Check the BpfApplicationState Object was created successfully
	bpfAppState2, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	require.Equal(t, 1, len(bpfAppState2.Status.Conditions))
	require.Equal(t, string(bpfmaniov1alpha1.ProgramReconcileSuccess), bpfAppState2.Status.Conditions[0].Type)

	require.Equal(t, fakeNode.Name, bpfAppState2.Labels[internal.K8sHostLabel])

	require.Equal(t, appProgramName, bpfAppState2.Labels[internal.BpfAppStateOwner])

	require.Equal(t, internal.BpfApplicationControllerFinalizer, bpfAppState2.Finalizers[0])

	for _, program := range bpfAppState2.Spec.Programs {
		r.Logger.Info("ProgramAttachStatus check", "program", program.BpfFunctionName, "status", program.ProgramAttachStatus)
		require.Equal(t, bpfmaniov1alpha1.ProgAttachSuccess, program.ProgramAttachStatus)
	}

	// Do a 3rd reconcile and make sure it doesn't change
	r.Logger.Info("Third reconcile")
	res, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Require no requeue
	require.False(t, res.Requeue)

	r.Logger.Info("Third reconcile", "res:", res, "err:", err)

	// Check the BpfApplicationState Object was created successfully
	bpfAppState3, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	// Check that the bpfAppState was not updated
	require.True(t, reflect.DeepEqual(bpfAppState2, bpfAppState3))

	// currentXdpProgram := programs[0]

	// attachPoint := bpfAppState2.Spec.Programs[0].XDP.AttachPoints[0]

	// 	xdpReconciler := &XdpProgramReconciler{
	// 		ReconcilerCommon: rc,
	// 		ProgramReconcilerCommon: ProgramReconcilerCommon{
	// 			appCommon: bpfmaniov1alpha1.BpfAppCommon{
	// 				NodeSelector: metav1.LabelSelector{},
	// 				ByteCode: bpfmaniov1alpha1.BytecodeSelector{
	// 					Path: &bytecodePath,
	// 				},
	// 			},
	// 			currentProgram:      &currentXdpProgram,
	// 			currentProgramState: &bpfmaniov1alpha1.BpfApplicationProgramState{},
	// 		},
	// 		currentAttachPoint: &attachPoint,
	// 	}

	// 	loadRequest, err := xdpReconciler.getLoadRequest(nil)
	// 	require.NoError(t, err)

	// require.Equal(t, xdpBpfFunctionName, loadRequest.Name)
	// require.Equal(t, uint32(6), loadRequest.ProgramType)
}
