package bpfmanagent

import (
	"testing"

	bpfmaniov1alpha1 "github.com/bpfman/bpfman-operator/apis/v1alpha1"
	testutils "github.com/bpfman/bpfman-operator/internal/test-utils"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestBpfApplicationControllerCreate(t *testing.T) {
	var (
		name                      = "fakeAppProgram"
		bytecodePath              = "/tmp/hello.o"
		bpfFentryFunctionName     = "fentry_test"
		bpfKprobeFunctionName     = "kprobe_test"
		bpfTracepointFunctionName = "tracepoint-test"
		fakeNode                  = testutils.NewNode("fake-control-plane")
		functionFentryName        = "do_unlinkat"
		functionKprobeName        = "try_to_wake_up"
		tracepointName            = "syscalls/sys_enter_setitimer"
		offset                    = 0
		retprobe                  = false
	)

	// A AppProgram object with metadata and spec.
	App := &bpfmaniov1alpha1.BpfApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: bpfmaniov1alpha1.BpfApplicationSpec{
			BpfAppCommon: bpfmaniov1alpha1.BpfAppCommon{
				NodeSelector: metav1.LabelSelector{},
			},
			Programs: []bpfmaniov1alpha1.BpfApplicationProgram{
				{
					Type: bpfmaniov1alpha1.ProgTypeFentry,
					Fentry: &bpfmaniov1alpha1.FentryProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfFentryFunctionName,
							ByteCode: bpfmaniov1alpha1.BytecodeSelector{
								Path: &bytecodePath,
							},
						},
						FunctionName: functionFentryName,
					},
				},
				{
					Type: bpfmaniov1alpha1.ProgTypeKprobe,
					Kprobe: &bpfmaniov1alpha1.KprobeProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfKprobeFunctionName,
							ByteCode: bpfmaniov1alpha1.BytecodeSelector{
								Path: &bytecodePath,
							},
						},
						FunctionName: functionKprobeName,
						Offset:       uint64(offset),
						RetProbe:     retprobe,
					},
				},
				{
					Type: bpfmaniov1alpha1.ProgTypeTracepoint,
					Tracepoint: &bpfmaniov1alpha1.TracepointProgramInfo{
						BpfProgramCommon: bpfmaniov1alpha1.BpfProgramCommon{
							BpfFunctionName: bpfTracepointFunctionName,
							ByteCode: bpfmaniov1alpha1.BytecodeSelector{
								Path: &bytecodePath,
							},
						},
						Names: []string{tracepointName},
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	_ = []runtime.Object{fakeNode, App}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, App)
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplicationList{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfApplication{})
	s.AddKnownTypes(bpfmaniov1alpha1.SchemeGroupVersion, &bpfmaniov1alpha1.BpfProgramList{})

}
