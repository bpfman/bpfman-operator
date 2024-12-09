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

func TestClBpfApplicationControllerCreate(t *testing.T) {
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

	programs := []bpfmaniov1alpha1.ClBpfApplicationProgram{}

	fakeInts := []string{fakeInt0}

	interfaceSelector := bpfmaniov1alpha1.InterfaceSelector{
		Interfaces: &fakeInts,
	}

	proceedOn := []bpfmaniov1alpha1.XdpProceedOnValue{bpfmaniov1alpha1.XdpProceedOnValue("pass"),
		bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return")}
	xdpAttachInfo := bpfmaniov1alpha1.ClXdpAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Priority:          int32(priority),
		ProceedOn:         proceedOn,
	}
	xdpProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: xdpBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeXDP,
		XDPInfo: &bpfmaniov1alpha1.ClXdpProgramInfo{
			Links: []bpfmaniov1alpha1.ClXdpAttachInfo{xdpAttachInfo},
		},
	}
	programs = append(programs, xdpProgram)

	fentryProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: fentryBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeFentry,
		FentryInfo: &bpfmaniov1alpha1.ClFentryProgramInfo{
			ClFentryLoadInfo: bpfmaniov1alpha1.ClFentryLoadInfo{
				Function: AttachName,
			},
			Links: []bpfmaniov1alpha1.ClFentryAttachInfo{
				{},
			},
		},
	}
	programs = append(programs, fentryProgram)

	fexitProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: fexitBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeFexit,
		FexitInfo: &bpfmaniov1alpha1.ClFexitProgramInfo{
			ClFexitLoadInfo: bpfmaniov1alpha1.ClFexitLoadInfo{
				Function: AttachName,
			},
			Links: []bpfmaniov1alpha1.ClFexitAttachInfo{
				{Attach: true},
			},
		},
	}
	programs = append(programs, fexitProgram)

	kprobeProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: kprobeBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeKprobe,
		KprobeInfo: &bpfmaniov1alpha1.ClKprobeProgramInfo{
			Links: []bpfmaniov1alpha1.ClKprobeAttachInfo{
				{
					Function: AttachName,
					Offset:   200,
				},
			},
		},
	}
	programs = append(programs, kprobeProgram)

	uprobeProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: uprobeBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeUprobe,
		UprobeInfo: &bpfmaniov1alpha1.ClUprobeProgramInfo{
			Links: []bpfmaniov1alpha1.ClUprobeAttachInfo{
				{
					Function: AttachName,
					Offset:   200,
					Target:   "/bin/bash",
					Pid:      &fakePid,
				},
			},
		},
	}
	programs = append(programs, uprobeProgram)

	tracepointProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: tracepointBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeTracepoint,
		TracepointInfo: &bpfmaniov1alpha1.ClTracepointProgramInfo{
			Links: []bpfmaniov1alpha1.ClTracepointAttachInfo{
				{
					Name: AttachName,
				},
			},
		},
	}
	programs = append(programs, tracepointProgram)

	tcAttachInfo := bpfmaniov1alpha1.ClTcAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Direction:         direction,
		Priority:          int32(priority),
	}
	tcProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: tcBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeTC,
		TCInfo: &bpfmaniov1alpha1.ClTcProgramInfo{
			Links: []bpfmaniov1alpha1.ClTcAttachInfo{tcAttachInfo},
		},
	}
	programs = append(programs, tcProgram)

	tcxAttachInfo := bpfmaniov1alpha1.ClTcxAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        nil,
		Direction:         "ingress",
		Priority:          int32(priority),
	}
	tcxProgram := bpfmaniov1alpha1.ClBpfApplicationProgram{
		Name: tcxBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeTCX,
		TCXInfo: &bpfmaniov1alpha1.ClTcxProgramInfo{
			Links: []bpfmaniov1alpha1.ClTcxAttachInfo{tcxAttachInfo},
		},
	}
	programs = append(programs, tcxProgram)

	bpfApp := &bpfmaniov1alpha1.ClusterBpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: appProgramName,
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

	// Objects to track in the fake client.
	objs := []runtime.Object{fakeNode, bpfApp}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, bpfApp)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplication{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationStateList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.ClusterBpfApplicationState{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(bpfApp).WithStatusSubresource(&bpfmaniov1alpha1.ClusterBpfApplicationState{}).WithRuntimeObjects(objs...).Build()

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
	r := &ClBpfApplicationReconciler{
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

	// First reconcile should create the ClusterBpfApplicationState object
	r.Logger.Info("First reconcile")
	res, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	r.Logger.Info("First reconcile", "res:", res, "err:", err)

	// Require no requeue
	require.False(t, res.Requeue)

	// Check if the ClusterBpfApplicationState Object was created successfully
	bpfAppState, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	require.Equal(t, 1, len(bpfAppState.Status.Conditions))
	require.Equal(t, string(bpfmaniov1alpha1.BpfAppStateCondPending), bpfAppState.Status.Conditions[0].Type)

	require.Equal(t, fakeNode.Name, bpfAppState.Labels[internal.K8sHostLabel])

	require.Equal(t, appProgramName, bpfAppState.Labels[internal.BpfAppStateOwner])

	require.Equal(t, internal.ClBpfApplicationControllerFinalizer, bpfAppState.Finalizers[0])

	// Do a second reconcile to make sure that the programs are loaded and attached.
	r.Logger.Info("Second reconcile")
	res, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	r.Logger.Info("Second reconcile", "res:", res, "err:", err)

	// Require no requeue
	require.False(t, res.Requeue)

	// Check if the ClusterBpfApplicationState Object was created successfully
	bpfAppState2, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	require.Equal(t, 1, len(bpfAppState2.Status.Conditions))
	require.Equal(t, string(bpfmaniov1alpha1.BpfAppStateCondSuccess), bpfAppState2.Status.Conditions[0].Type)

	require.Equal(t, fakeNode.Name, bpfAppState2.Labels[internal.K8sHostLabel])

	require.Equal(t, appProgramName, bpfAppState2.Labels[internal.BpfAppStateOwner])

	require.Equal(t, internal.ClBpfApplicationControllerFinalizer, bpfAppState2.Finalizers[0])

	for _, program := range bpfAppState2.Spec.Programs {
		r.Logger.Info("ProgramAttachStatus check", "program", program.Name, "status", program.ProgramLinkStatus)
		require.Equal(t, bpfmaniov1alpha1.ProgAttachSuccess, program.ProgramLinkStatus)
	}

	// Do a 3rd reconcile and make sure it doesn't change
	r.Logger.Info("Third reconcile")
	res, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Require no requeue
	require.False(t, res.Requeue)

	r.Logger.Info("Third reconcile", "res:", res, "err:", err)

	// Check if the ClusterBpfApplicationState Object was created successfully
	bpfAppState3, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	// Check that the bpfAppState was not updated
	require.True(t, reflect.DeepEqual(bpfAppState2, bpfAppState3))
}
