/*
Copyright 2025 The bpfman Authors.

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
	"reflect"
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	agenttestutils "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal/test-utils"
	"github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"
	"github.com/bpfman/bpfman-operator/pkg/helpers"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
)

func TestNsBpfApplicationReconcilerGetBpfAppState(t *testing.T) {
	// Create a mock BpfApplicationState that will be found.
	mockAppState := &bpfmaniov1alpha1.BpfApplicationState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app-state",
			Namespace: "test-namespace",
			Labels: map[string]string{
				internal.BpfAppStateOwner: "test-app",
				internal.K8sHostLabel:     "test-node",
			},
		},
	}
	objs := []runtime.Object{mockAppState}

	bpfApp := &bpfmaniov1alpha1.BpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "test-namespace",
		},
	}
	fakeNode := testutils.NewNode("test-node")
	r := createFakeNamespaceReconciler(objs, bpfApp, fakeNode, nil)
	// Set currentApp (required for getBpfAppState).
	r.currentApp = bpfApp

	// Test getBpfAppState when currentAppState is nil.
	ctx := context.Background()
	result, err := r.getBpfAppState(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "test-app-state", result.Name)
}

func TestNsBpfApplicationControllerCreate(t *testing.T) {
	var (
		fakePid           = int32(1000)
		fakeNode          = testutils.NewNode("fake-control-plane")
		interfaceSelector = bpfmaniov1alpha1.InterfaceSelector{
			Interfaces: []string{fakeInt0},
		}
		fakeNetNamespaces = bpfmaniov1alpha1.NetworkNamespaceSelector{
			Pods: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
		}
		proceedOn = []bpfmaniov1alpha1.XdpProceedOnValue{
			bpfmaniov1alpha1.XdpProceedOnValue("pass"),
			bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return"),
		}
		tcProceedOn = []bpfmaniov1alpha1.TcProceedOnValue{bpfmaniov1alpha1.TcProceedOnValue("ok"),
			bpfmaniov1alpha1.TcProceedOnValue("shot")}
		fakeContainers = bpfmaniov1alpha1.ContainerSelector{
			Pods: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
		}
		ctx = context.TODO()
	)

	// Set development Logger, so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	tcs := []struct {
		testName string
		programs []bpfmaniov1alpha1.BpfApplicationProgram
	}{
		{
			testName: "application test",
			programs: []bpfmaniov1alpha1.BpfApplicationProgram{
				// XDP Program
				{
					Name: testXdpBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeXDP,
					XDP: &bpfmaniov1alpha1.XdpProgramInfo{
						Links: []bpfmaniov1alpha1.XdpAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Priority:          ptr.To(int32(testPriority)),
								ProceedOn:         proceedOn,
							},
						},
					},
				},
				// TCX Program
				{
					Name: testTcxBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeTCX,
					TCX: &bpfmaniov1alpha1.TcxProgramInfo{
						Links: []bpfmaniov1alpha1.TcxAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Direction:         testDirectionIngress,
								Priority:          ptr.To(int32(testPriority)),
							},
						},
					},
				},
				// Uprobe Program
				{
					Name: testUprobeBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeUprobe,
					UProbe: &bpfmaniov1alpha1.UprobeProgramInfo{
						Links: []bpfmaniov1alpha1.UprobeAttachInfo{
							{
								Function:   testAttachName,
								Offset:     200,
								Target:     "/bin/bash",
								Pid:        &fakePid,
								Containers: fakeContainers,
							},
						},
					},
				},
				// Uretprobe Program
				{
					Name: testUretprobeBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeUretprobe,
					URetProbe: &bpfmaniov1alpha1.UprobeProgramInfo{
						Links: []bpfmaniov1alpha1.UprobeAttachInfo{
							{
								Function:   testAttachName,
								Offset:     0,
								Target:     "/bin/bash",
								Pid:        &fakePid,
								Containers: fakeContainers,
							},
						},
					},
				},
				// TC Program
				{
					Name: testTcBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeTC,
					TC: &bpfmaniov1alpha1.TcProgramInfo{
						Links: []bpfmaniov1alpha1.TcAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								Direction:         testDirectionIngress,
								Priority:          ptr.To(int32(testPriority)),
								NetworkNamespaces: fakeNetNamespaces,
								ProceedOn:         tcProceedOn,
							},
						},
					},
				},
			},
		},
		{
			testName: "link priority test XDP",
			programs: []bpfmaniov1alpha1.BpfApplicationProgram{
				// XDP Program
				{
					Name: fmt.Sprintf("%s-%d", testXdpBpfFunctionName, 0),
					Type: bpfmaniov1alpha1.ProgTypeXDP,
					XDP: &bpfmaniov1alpha1.XdpProgramInfo{
						Links: []bpfmaniov1alpha1.XdpAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Priority:          nil, // nil priority should yield 1000.
								ProceedOn:         proceedOn,
							},
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-%d", testXdpBpfFunctionName, 1),
					Type: bpfmaniov1alpha1.ProgTypeXDP,
					XDP: &bpfmaniov1alpha1.XdpProgramInfo{
						Links: []bpfmaniov1alpha1.XdpAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Priority:          ptr.To(int32(999)),
								ProceedOn:         proceedOn,
							},
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-%d", testXdpBpfFunctionName, 2),
					Type: bpfmaniov1alpha1.ProgTypeXDP,
					XDP: &bpfmaniov1alpha1.XdpProgramInfo{
						Links: []bpfmaniov1alpha1.XdpAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Priority:          ptr.To(int32(0)), // 0 priority should be allowed.
								ProceedOn:         proceedOn,
							},
						},
					},
				},
			},
		},
		{
			testName: "link priority test TC",
			programs: []bpfmaniov1alpha1.BpfApplicationProgram{
				// TC Program
				{
					Name: fmt.Sprintf("%s-%d", testTcBpfFunctionName, 0),
					Type: bpfmaniov1alpha1.ProgTypeTC,
					TC: &bpfmaniov1alpha1.TcProgramInfo{
						Links: []bpfmaniov1alpha1.TcAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Priority:          nil, // nil priority should yield 1000.
								ProceedOn:         tcProceedOn,
							},
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-%d", testTcBpfFunctionName, 1),
					Type: bpfmaniov1alpha1.ProgTypeTC,
					TC: &bpfmaniov1alpha1.TcProgramInfo{
						Links: []bpfmaniov1alpha1.TcAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Priority:          ptr.To(int32(999)),
								ProceedOn:         tcProceedOn,
							},
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-%d", testTcBpfFunctionName, 2),
					Type: bpfmaniov1alpha1.ProgTypeTC,
					TC: &bpfmaniov1alpha1.TcProgramInfo{
						Links: []bpfmaniov1alpha1.TcAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Priority:          ptr.To(int32(0)), // 0 priority should be allowed.
								ProceedOn:         tcProceedOn,
							},
						},
					},
				},
			},
		},
		{
			testName: "link priority test TCX",
			programs: []bpfmaniov1alpha1.BpfApplicationProgram{
				// TCX Program
				{
					Name: fmt.Sprintf("%s-%d", testTcxBpfFunctionName, 0),
					Type: bpfmaniov1alpha1.ProgTypeTCX,
					TCX: &bpfmaniov1alpha1.TcxProgramInfo{
						Links: []bpfmaniov1alpha1.TcxAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Direction:         testDirectionIngress,
								Priority:          nil, // nil priority should yield 1000.
							},
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-%d", testTcxBpfFunctionName, 1),
					Type: bpfmaniov1alpha1.ProgTypeTCX,
					TCX: &bpfmaniov1alpha1.TcxProgramInfo{
						Links: []bpfmaniov1alpha1.TcxAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Direction:         testDirectionIngress,
								Priority:          ptr.To(int32(999)),
							},
						},
					},
				},
				{
					Name: fmt.Sprintf("%s-%d", testTcxBpfFunctionName, 2),
					Type: bpfmaniov1alpha1.ProgTypeTCX,
					TCX: &bpfmaniov1alpha1.TcxProgramInfo{
						Links: []bpfmaniov1alpha1.TcxAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: fakeNetNamespaces,
								Direction:         testDirectionIngress,
								Priority:          ptr.To(int32(0)), // 0 priority should be allowed.
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		bpfApp := &bpfmaniov1alpha1.BpfApplication{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testAppProgramName,
				Namespace: testNamespace,
			},
			Spec: bpfmaniov1alpha1.BpfApplicationSpec{
				BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
					NodeSelector: metav1.LabelSelector{},
					ByteCode: bpfmaniov1alpha1.ByteCodeSelector{
						Path: ptr.To(testBytecodePath),
					},
				},
				Programs: tc.programs,
			},
		}

		testContainers := FakeContainerGetter{
			containerList: &[]ContainerInfo{
				{
					podName:       fakePodName,
					containerName: fakeContainerName,
					pid:           fakePid,
				},
			},
		}

		// Objects to track in the fake client.
		objs := []runtime.Object{fakeNode, bpfApp}
		r := createFakeNamespaceReconciler(objs, bpfApp, fakeNode, &testContainers)

		// Mock request to simulate Reconcile() being called on an event for a
		// watched resource.
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testAppProgramName,
				Namespace: testNamespace,
			},
		}

		// First reconcile should create the BpfApplicationState object.
		r.Logger.Info("First reconcile")
		runReconciler(t, ctx, r, req, r.Logger)
		// Check if the BpfApplicationState Object was created successfully.
		bpfAppState, err := r.getBpfAppState(ctx)
		require.NoError(t, err)
		verifyBpfApplicationState(t, bpfAppState, fakeNode, testAppProgramName, bpfmaniov1alpha1.BpfAppStateCondPending)

		// Do a second reconcile to make sure that the programs are loaded and attached.
		r.Logger.Info("Second reconcile")
		runReconciler(t, ctx, r, req, r.Logger)
		// Check if the BpfApplicationState Object was created successfully.
		bpfAppState2, err := r.getBpfAppState(ctx)
		require.NoError(t, err)
		verifyBpfApplicationState(t, bpfAppState2, fakeNode, testAppProgramName, bpfmaniov1alpha1.BpfAppStateCondSuccess)
		verifyNamespaceBpfProgramState(t, bpfAppState2, tc.programs)

		// Do a 3rd reconcile and make sure it doesn't change.
		r.Logger.Info("Third reconcile")
		runReconciler(t, ctx, r, req, r.Logger)
		// Check if the BpfApplicationState Object was created successfully.
		bpfAppState3, err := r.getBpfAppState(ctx)
		require.NoError(t, err)
		verifyBpfApplicationState(t, bpfAppState3, fakeNode, testAppProgramName, bpfmaniov1alpha1.BpfAppStateCondSuccess)
		// Check that the bpfAppState was not updated.
		require.True(t, reflect.DeepEqual(bpfAppState2, bpfAppState3))
	}
}

// createFakeNamespaceReconciler creates a fake NsBpfApplicationReconciler for testing purposes.
// It initializes the scheme with BPF application types, creates a fake Kubernetes client with
// a container getter, and returns a configured reconciler instance.
func createFakeNamespaceReconciler(objs []runtime.Object, bpfApp *bpfmaniov1alpha1.BpfApplication, fakeNode *v1.Node,
	testContainers *FakeContainerGetter) *NsBpfApplicationReconciler {
	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	registerBpfApplicationScheme(s, false, bpfApp)

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(bpfApp).WithStatusSubresource(
		&bpfmaniov1alpha1.BpfApplicationState{}).WithRuntimeObjects(objs...).Build()
	cli := agenttestutils.NewBpfmanClientFake()

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
		ourNode:      fakeNode,
		Containers:   testContainers,
		NetNsCache:   MockNetNsCache{"/host/proc/1000/ns/net": ptr.To(uint64(12345))},
	}
	r := &NsBpfApplicationReconciler{
		ReconcilerCommon: rc,
	}
	return r
}

// verifyNamespaceBpfProgramState checks that all BPF programs in the application state have
// successfully attached and that the number of programs matches the expected count.
func verifyNamespaceBpfProgramState(t *testing.T, bpfAppState *bpfmaniov1alpha1.BpfApplicationState,
	programs []bpfmaniov1alpha1.BpfApplicationProgram) {
	require.Equal(t, len(programs), len(bpfAppState.Status.Programs))

	numMatches := 0
	for _, program := range bpfAppState.Status.Programs {
		require.Equal(t, bpfmaniov1alpha1.ProgAttachSuccess, program.ProgramLinkStatus)
		for _, expectedProgram := range programs {
			if program.Name != expectedProgram.Name {
				continue
			}
			switch {
			case program.XDP != nil:
				require.NotNil(t, program.XDP.Links[0].Priority)
				if *program.XDP.Links[0].Priority != helpers.GetPriority(expectedProgram.XDP.Links[0].Priority) {
					continue
				}
			case program.TC != nil:
				require.NotNil(t, program.TC.Links[0].Priority)
				if *program.TC.Links[0].Priority != helpers.GetPriority(expectedProgram.TC.Links[0].Priority) {
					continue
				}
			case program.TCX != nil:
				require.NotNil(t, program.TCX.Links[0].Priority)
				if *program.TCX.Links[0].Priority != helpers.GetPriority(expectedProgram.TCX.Links[0].Priority) {
					continue
				}
			}
			numMatches++
		}
	}
	require.Equal(t, len(programs), numMatches)
}
