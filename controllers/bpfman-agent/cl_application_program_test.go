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
	"reflect"
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	agenttestutils "github.com/bpfman/bpfman-operator/controllers/bpfman-agent/internal/test-utils"
	"github.com/bpfman/bpfman-operator/internal"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestClBpfApplicationReconcilerGetBpfAppState(t *testing.T) {
	// Create a mock ClusterBpfApplicationState that will be found.
	mockAppState := &bpfmaniov1alpha1.ClusterBpfApplicationState{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-app-state",
			Labels: map[string]string{
				internal.BpfAppStateOwner: "test-app",
				internal.K8sHostLabel:     "test-node",
			},
		},
	}
	objs := []runtime.Object{mockAppState}

	bpfApp := &bpfmaniov1alpha1.ClusterBpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-app",
		},
	}
	fakeNode := testutils.NewNode("test-node")
	r := createFakeClusterReconciler(objs, bpfApp, fakeNode)
	// Set currentApp (required for getBpfAppState).
	r.currentApp = bpfApp

	// Test getBpfAppState when currentAppState is nil.
	ctx := context.Background()
	result, err := r.getBpfAppState(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "test-app-state", result.Name)
}

func TestClBpfApplicationControllerCreate(t *testing.T) {
	var (
		// global config
		fakePid           = int32(4490)
		fakeNode          = testutils.NewNode("fake-control-plane")
		interfaceSelector = bpfmaniov1alpha1.InterfaceSelector{
			Interfaces: []string{fakeInt0},
		}
		proceedOn = []bpfmaniov1alpha1.XdpProceedOnValue{
			bpfmaniov1alpha1.XdpProceedOnValue("pass"),
			bpfmaniov1alpha1.XdpProceedOnValue("dispatcher_return"),
		}
		ctx = context.TODO()
	)

	// Set development Logger, so we can see all logs in tests.
	logf.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

	tcs := []struct {
		testName string
		programs []bpfmaniov1alpha1.ClBpfApplicationProgram
	}{
		{
			testName: "application test",
			programs: []bpfmaniov1alpha1.ClBpfApplicationProgram{
				// XDP Program
				{
					Name: testXdpBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeXDP,
					XDP: &bpfmaniov1alpha1.ClXdpProgramInfo{
						Links: []bpfmaniov1alpha1.ClXdpAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: nil,
								Priority:          ptr.To(int32(testPriority)),
								ProceedOn:         proceedOn,
							},
						},
					},
				},
				// Fentry Program
				{
					Name: testFentryBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeFentry,
					FEntry: &bpfmaniov1alpha1.ClFentryProgramInfo{
						ClFentryLoadInfo: bpfmaniov1alpha1.ClFentryLoadInfo{
							Function: testAttachName,
						},
						Links: []bpfmaniov1alpha1.ClFentryAttachInfo{{}},
					},
				},
				// Fexit Program
				{
					Name: testFexitBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeFexit,
					FExit: &bpfmaniov1alpha1.ClFexitProgramInfo{
						ClFexitLoadInfo: bpfmaniov1alpha1.ClFexitLoadInfo{
							Function: testAttachName,
						},
						Links: []bpfmaniov1alpha1.ClFexitAttachInfo{
							{Mode: bpfmaniov1alpha1.Attach},
						},
					},
				},
				// Kprobe Program
				{
					Name: testKprobeBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeKprobe,
					KProbe: &bpfmaniov1alpha1.ClKprobeProgramInfo{
						Links: []bpfmaniov1alpha1.ClKprobeAttachInfo{
							{
								Function: testAttachName,
								Offset:   200,
							},
						},
					},
				},
				// Kretprobe Program
				{
					Name: testKretprobeBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeKretprobe,
					KRetProbe: &bpfmaniov1alpha1.ClKretprobeProgramInfo{
						Links: []bpfmaniov1alpha1.ClKretprobeAttachInfo{
							{
								Function: testAttachName,
							},
						},
					},
				},
				// Uprobe Program
				{
					Name: testUprobeBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeUprobe,
					UProbe: &bpfmaniov1alpha1.ClUprobeProgramInfo{
						Links: []bpfmaniov1alpha1.ClUprobeAttachInfo{
							{
								Function: testAttachName,
								Offset:   200,
								Target:   "/bin/bash",
								Pid:      &fakePid,
							},
						},
					},
				},
				// Uretprobe Program
				{
					Name: testUretprobeBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeUretprobe,
					URetProbe: &bpfmaniov1alpha1.ClUprobeProgramInfo{
						Links: []bpfmaniov1alpha1.ClUprobeAttachInfo{
							{
								Function: testAttachName,
								Offset:   100,
								Target:   "/bin/bash",
								Pid:      &fakePid,
							},
						},
					},
				},
				// Tracepoint Program
				{
					Name: testTracepointBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeTracepoint,
					TracePoint: &bpfmaniov1alpha1.ClTracepointProgramInfo{
						Links: []bpfmaniov1alpha1.ClTracepointAttachInfo{
							{
								Name: testAttachName,
							},
						},
					},
				},
				// TC Program
				{
					Name: testTcBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeTC,
					TC: &bpfmaniov1alpha1.ClTcProgramInfo{
						Links: []bpfmaniov1alpha1.ClTcAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: nil,
								Direction:         testDirectionIngress,
								Priority:          ptr.To(int32(testPriority)),
							},
						},
					},
				},
				// TCX Program
				{
					Name: testTcxBpfFunctionName,
					Type: bpfmaniov1alpha1.ProgTypeTCX,
					TCX: &bpfmaniov1alpha1.ClTcxProgramInfo{
						Links: []bpfmaniov1alpha1.ClTcxAttachInfo{
							{
								InterfaceSelector: interfaceSelector,
								NetworkNamespaces: nil,
								Direction:         testDirectionIngress,
								Priority:          ptr.To(int32(testPriority)),
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		bpfApp := &bpfmaniov1alpha1.ClusterBpfApplication{
			ObjectMeta: metav1.ObjectMeta{
				Name: testAppProgramName,
			},
			Spec: bpfmaniov1alpha1.ClBpfApplicationSpec{
				BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
					NodeSelector: metav1.LabelSelector{},
					ByteCode: bpfmaniov1alpha1.ByteCodeSelector{
						Path: ptr.To(testBytecodePath),
					},
				},
				Programs: tc.programs,
			},
		}

		// Objects to track in the fake client.
		objs := []runtime.Object{fakeNode, bpfApp}
		r := createFakeClusterReconciler(objs, bpfApp, fakeNode)

		// Mock request to simulate Reconcile() being called on an event for a
		// watched resource.
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      testAppProgramName,
				Namespace: testNamespace,
			},
		}

		// First reconcile should create the ClusterBpfApplicationState object.
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
		verifyClusterBpfProgramState(t, bpfAppState2, tc.programs)

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

// createFakeClusterReconciler creates a fake ClBpfApplicationReconciler for testing purposes.
// It initializes the scheme with BPF application types, creates a fake Kubernetes client,
// and returns a configured reconciler instance.
func createFakeClusterReconciler(objs []runtime.Object, bpfApp *bpfmaniov1alpha1.ClusterBpfApplication,
	fakeNode *v1.Node) *ClBpfApplicationReconciler {
	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	registerBpfApplicationScheme(s, true, bpfApp)

	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(bpfApp).WithStatusSubresource(
		&bpfmaniov1alpha1.ClusterBpfApplicationState{}).WithRuntimeObjects(objs...).Build()
	cli := agenttestutils.NewBpfmanClientFake()

	rc := ReconcilerCommon{
		Client:       cl,
		Scheme:       s,
		BpfmanClient: cli,
		NodeName:     fakeNode.Name,
		ourNode:      fakeNode,
	}
	r := &ClBpfApplicationReconciler{
		ReconcilerCommon: rc,
	}
	return r
}

// verifyClusterBpfProgramState checks that all BPF programs in the application state have
// successfully attached and that the number of programs matches the expected count.
func verifyClusterBpfProgramState(t *testing.T, bpfAppState *bpfmaniov1alpha1.ClusterBpfApplicationState,
	programs []bpfmaniov1alpha1.ClBpfApplicationProgram) {
	for _, program := range bpfAppState.Status.Programs {
		require.Equal(t, bpfmaniov1alpha1.ProgAttachSuccess, program.ProgramLinkStatus)
	}
	require.Equal(t, len(programs), len(bpfAppState.Status.Programs))
}
