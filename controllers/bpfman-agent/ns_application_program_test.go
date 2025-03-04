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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestNsBpfApplicationControllerCreate(t *testing.T) {
	var (
		// global config
		appProgramName = "fakeAppProgram"
		namespace      = "bpfman"
		bytecodePath   = "/tmp/hello.o"

		xdpBpfFunctionName       = "XdpTest"
		tcxBpfFunctionName       = "TcxTest"
		uprobeBpfFunctionName    = "UprobeTest"
		uretprobeBpfFunctionName = "UretprobeTest"
		tcBpfFunctionName        = "TcTest"

		direction  = "ingress"
		AttachName = "AttachNameTest"
		priority   = 50
		fakeNode   = testutils.NewNode("fake-control-plane")
		fakeInt0   = "eth0"
		// fakeInt1        = "eth1"
		fakePodName       = "my-pod"
		fakeContainerName = "my-container-1"
		fakePid           = int32(4490)

		ctx = context.TODO()
	)

	programs := []bpfmaniov1alpha1.BpfApplicationProgram{}

	fakeContainers := bpfmaniov1alpha1.ContainerSelector{
		Pods: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "test",
			},
		},
	}

	fakeInts := []string{fakeInt0}

	interfaceSelector := bpfmaniov1alpha1.InterfaceSelector{
		Interfaces: &fakeInts,
	}

	proceedOn := []bpfmaniov1alpha1.XdpProceedOnValue{bpfmaniov1alpha1.XdpProceedOnValue("pass"),
		bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return")}

	xdpAttachInfo := bpfmaniov1alpha1.XdpAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        fakeContainers,
		Priority:          int32(priority),
		ProceedOn:         proceedOn,
	}

	xdpProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		Name: xdpBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeXDP,
		XDPInfo: &bpfmaniov1alpha1.XdpProgramInfo{
			Links: []bpfmaniov1alpha1.XdpAttachInfo{xdpAttachInfo},
		},
	}

	programs = append(programs, xdpProgram)

	tcxAttachInfo := bpfmaniov1alpha1.TcxAttachInfo{
		InterfaceSelector: interfaceSelector,
		Containers:        fakeContainers,
		Direction:         "ingress",
		Priority:          int32(priority),
	}
	tcxProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		Name: tcxBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeTCX,
		TCXInfo: &bpfmaniov1alpha1.TcxProgramInfo{
			Links: []bpfmaniov1alpha1.TcxAttachInfo{tcxAttachInfo},
		},
	}
	programs = append(programs, tcxProgram)

	uprobeProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		Name: uprobeBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeUprobe,
		UprobeInfo: &bpfmaniov1alpha1.UprobeProgramInfo{
			Links: []bpfmaniov1alpha1.UprobeAttachInfo{
				{
					Function:   AttachName,
					Offset:     200,
					Target:     "/bin/bash",
					Pid:        &fakePid,
					Containers: fakeContainers,
				},
			},
		},
	}
	programs = append(programs, uprobeProgram)

	uretprobeProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		Name: uretprobeBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeUretprobe,
		UretprobeInfo: &bpfmaniov1alpha1.UprobeProgramInfo{
			Links: []bpfmaniov1alpha1.UprobeAttachInfo{
				{
					Function:   AttachName,
					Offset:     0,
					Target:     "/bin/bash",
					Pid:        &fakePid,
					Containers: fakeContainers,
				},
			},
		},
	}
	programs = append(programs, uretprobeProgram)

	tcProceedOn := []bpfmaniov1alpha1.TcProceedOnValue{bpfmaniov1alpha1.TcProceedOnValue("ok"),
		bpfmaniov1alpha1.TcProceedOnValue("shot")}

	tcAttachInfo := bpfmaniov1alpha1.TcAttachInfo{
		InterfaceSelector: interfaceSelector,
		Direction:         direction,
		Priority:          int32(priority),
		Containers:        fakeContainers,
		ProceedOn:         tcProceedOn,
	}
	tcProgram := bpfmaniov1alpha1.BpfApplicationProgram{
		Name: tcBpfFunctionName,
		Type: bpfmaniov1alpha1.ProgTypeTC,
		TCInfo: &bpfmaniov1alpha1.TcProgramInfo{
			Links: []bpfmaniov1alpha1.TcAttachInfo{tcAttachInfo},
		},
	}
	programs = append(programs, tcProgram)

	bpfApp := &bpfmaniov1alpha1.BpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appProgramName,
			Namespace: namespace,
		},
		Spec: bpfmaniov1alpha1.BpfApplicationSpec{
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
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplication{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationStateList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationState{})

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(bpfApp).WithStatusSubresource(&bpfmaniov1alpha1.BpfApplicationState{}).WithRuntimeObjects(objs...).Build()

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

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
		ourNode:      fakeNode,
		Containers:   &testContainers,
	}

	// Set development Logger, so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	// Create a ReconcileMemcached object with the scheme and fake client.
	r := &NsBpfApplicationReconciler{
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

	// Check if the BpfApplicationState Object was created successfully
	bpfAppState, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	require.Equal(t, 1, len(bpfAppState.Status.Conditions))
	require.Equal(t, string(bpfmaniov1alpha1.BpfAppStateCondPending), bpfAppState.Status.Conditions[0].Type)

	require.Equal(t, fakeNode.Name, bpfAppState.Labels[internal.K8sHostLabel])

	require.Equal(t, appProgramName, bpfAppState.Labels[internal.BpfAppStateOwner])

	require.Equal(t, internal.NsBpfApplicationControllerFinalizer, bpfAppState.Finalizers[0])

	// Do a second reconcile to make sure that the programs are loaded and attached.
	r.Logger.Info("Second reconcile")
	res, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	r.Logger.Info("Second reconcile", "res:", res, "err:", err)

	// Require no requeue
	require.False(t, res.Requeue)

	// Check if the BpfApplicationState Object was created successfully
	bpfAppState2, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	require.Equal(t, 1, len(bpfAppState2.Status.Conditions))
	require.Equal(t, string(bpfmaniov1alpha1.BpfAppStateCondSuccess), bpfAppState2.Status.Conditions[0].Type)

	require.Equal(t, fakeNode.Name, bpfAppState2.Labels[internal.K8sHostLabel])

	require.Equal(t, appProgramName, bpfAppState2.Labels[internal.BpfAppStateOwner])

	require.Equal(t, internal.NsBpfApplicationControllerFinalizer, bpfAppState2.Finalizers[0])

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

	// Check if the BpfApplicationState Object was created successfully
	bpfAppState3, bpfAppStateNew, err := r.getBpfAppState(ctx, false)
	require.NoError(t, err)

	// Make sure we got bpfAppState from the api server and didn't create a new
	// one.
	require.Equal(t, false, bpfAppStateNew)

	// Check that the bpfAppState was not updated
	require.True(t, reflect.DeepEqual(bpfAppState2, bpfAppState3))
}
