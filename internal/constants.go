/*
Copyright 2022.

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

package internal

import "fmt"

const (
	FentryProgramFunction        = "bpfman.io.fentryprogramcontroller/function"
	FentryNoContainersOnNode     = "bpfman.io.fentryprogramcontroller/nocontainersonnode"
	FexitProgramFunction         = "bpfman.io.fexitprogramcontroller/function"
	FexitNoContainersOnNode      = "bpfman.io.fexitprogramcontroller/nocontainersonnode"
	KprobeProgramFunction        = "bpfman.io.kprobeprogramcontroller/function"
	KprobeNoContainersOnNode     = "bpfman.io.kprobeprogramcontroller/nocontainersonnode"
	TcProgramInterface           = "bpfman.io.tcprogramcontroller/interface"
	TcContainerPid               = "bpfman.io.tcprogramcontroller/containerpid"
	TcNoContainersOnNode         = "bpfman.io.tcprogramcontroller/nocontainersonnode"
	TcNsProgramInterface         = "bpfman.io.tcnsprogramcontroller/interface"
	TcNsContainerPid             = "bpfman.io.tcnsprogramcontroller/containerpid"
	TcNsNoContainersOnNode       = "bpfman.io.tcnsprogramcontroller/nocontainersonnode"
	TcxProgramInterface          = "bpfman.io.tcxprogramcontroller/interface"
	TcxContainerPid              = "bpfman.io.tcxprogramcontroller/containerpid"
	TcxNoContainersOnNode        = "bpfman.io.tcxprogramcontroller/nocontainersonnode"
	TcxNsProgramInterface        = "bpfman.io.tcxnsprogramcontroller/interface"
	TcxNsContainerPid            = "bpfman.io.tcxnsprogramcontroller/containerpid"
	TcxNsNoContainersOnNode      = "bpfman.io.tcxnsprogramcontroller/nocontainersonnode"
	TracepointProgramTracepoint  = "bpfman.io.tracepointprogramcontroller/tracepoint"
	TracepointNoContainersOnNode = "bpfman.io.tracepointprogramcontroller/nocontainersonnode"
	UprobeProgramTarget          = "bpfman.io.uprobeprogramcontroller/target"
	UprobeContainerPid           = "bpfman.io.uprobeprogramcontroller/containerpid"
	UprobeNoContainersOnNode     = "bpfman.io.uprobeprogramcontroller/nocontainersonnode"
	UprobeNsProgramTarget        = "bpfman.io.uprobensprogramcontroller/target"
	UprobeNsContainerPid         = "bpfman.io.uprobensprogramcontroller/containerpid"
	UprobeNsNoContainersOnNode   = "bpfman.io.uprobensprogramcontroller/nocontainersonnode"
	XdpProgramInterface          = "bpfman.io.xdpprogramcontroller/interface"
	XdpContainerPid              = "bpfman.io.xdpprogramcontroller/containerpid"
	XdpNoContainersOnNode        = "bpfman.io.xdpprogramcontroller/nocontainersonnode"
	XdpNsProgramInterface        = "bpfman.io.xdpnsprogramcontroller/interface"
	XdpNsContainerPid            = "bpfman.io.xdpnsprogramcontroller/containerpid"
	XdpNsNoContainersOnNode      = "bpfman.io.xdpnsprogramcontroller/nocontainersonnode"
	K8sHostLabel                 = "kubernetes.io/hostname"
	DiscoveredLabel              = "bpfman.io/discoveredProgram"
	IdAnnotation                 = "bpfman.io/ProgramId"
	UuidMetadataKey              = "bpfman.io/uuid"
	ProgramNameKey               = "bpfman.io/ProgramName"
	BpfmanNs                     = "bpfman"
	BpfmanOperatorName           = "bpfman-operator"
	BpfmanDsName                 = "bpfman-daemon"
	BpfmanConfigName             = "bpfman-config"
	BpfmanCsiDriverName          = "csi.bpfman.io"
	BpfmanRestrictedSccName      = "bpfman-restricted"
	BpfmanContainerName          = "bpfman"
	BpfmanAgentContainerName     = "bpfman-agent"
	BpfmanDaemonManifestPath     = "./config/bpfman-deployment/daemonset.yaml"
	BpfmanCsiDriverPath          = "./config/bpfman-deployment/csidriverinfo.yaml"
	BpfmanRestrictedSCCPath      = "./config/openshift/restricted-scc.yaml"
	BpfmanMapFs                  = "/run/bpfman/fs/maps"
	DefaultType                  = "tcp"
	DefaultPath                  = "/run/bpfman-sock/bpfman.sock"
	DefaultPort                  = 50051
	DefaultEnabled               = true
	// BpfProgramOwner is the name of the object that owns the BpfProgram
	// object. In the case of a *Program, it will be the name of the *Program
	// object. In the case of a BpfApplication, it will be the name of the
	// BpfApplication object.
	BpfProgramOwner = "bpfman.io/ownedByProgram"
	// AppProgramId is an identifier that is used to identify individual
	// programs that are part of a given BpfApplication object.  *Programs have
	// an AppProgramId of "".
	AppProgramId = "bpfman.io/appProgramId"
	// BpfProgramAttachPoint is the attach point for a given BpfProgram.
	BpfProgramAttachPoint = "bpfman.io/bpfProgramAttachPoint"
)

// -----------------------------------------------------------------------------
// Finalizers
// -----------------------------------------------------------------------------

const (
	// BpfmanOperatorFinalizer is the finalizer that holds a *Program from
	// deletion until cleanup can be performed.
	BpfmanOperatorFinalizer = "bpfman.io.operator/finalizer"
	// XdpProgramControllerFinalizer is the finalizer that holds an Xdp BpfProgram
	// object from deletion until cleanup can be performed.
	XdpProgramControllerFinalizer = "bpfman.io.xdpprogramcontroller/finalizer"
	// XdpNsProgramControllerFinalizer is the finalizer that holds a Namespaced Xdp BpfProgram
	// object from deletion until cleanup can be performed.
	XdpNsProgramControllerFinalizer = "bpfman.io.xdpnsprogramcontroller/finalizer"
	// TcProgramControllerFinalizer is the finalizer that holds an Tc BpfProgram
	// object from deletion until cleanup can be performed.
	TcProgramControllerFinalizer = "bpfman.io.tcprogramcontroller/finalizer"
	// TcNsProgramControllerFinalizer is the finalizer that holds a Namespaced Tc BpfProgram
	// object from deletion until cleanup can be performed.
	TcNsProgramControllerFinalizer = "bpfman.io.tcnsprogramcontroller/finalizer"
	// TcxProgramControllerFinalizer is the finalizer that holds an Tcx BpfProgram
	// object from deletion until cleanup can be performed.
	TcxProgramControllerFinalizer = "bpfman.io.tcxprogramcontroller/finalizer"
	// TcxNsProgramControllerFinalizer is the finalizer that holds a Namespaced Tcx BpfProgram
	// object from deletion until cleanup can be performed.
	TcxNsProgramControllerFinalizer = "bpfman.io.tcxnsprogramcontroller/finalizer"
	// TracepointProgramControllerFinalizer is the finalizer that holds an Tracepoint
	// BpfProgram object from deletion until cleanup can be performed.
	TracepointProgramControllerFinalizer = "bpfman.io.tracepointprogramcontroller/finalizer"
	// KprobeProgramControllerFinalizer is the finalizer that holds a Kprobe
	// BpfProgram object from deletion until cleanup can be performed.
	KprobeProgramControllerFinalizer = "bpfman.io.kprobeprogramcontroller/finalizer"
	// UprobeProgramControllerFinalizer is the finalizer that holds a Uprobe
	// BpfProgram object from deletion until cleanup can be performed.
	UprobeProgramControllerFinalizer = "bpfman.io.uprobeprogramcontroller/finalizer"
	// UprobeNsProgramControllerFinalizer is the finalizer that holds a Namespaced Uprobe
	// BpfProgram object from deletion until cleanup can be performed.
	UprobeNsProgramControllerFinalizer = "bpfman.io.uprobensprogramcontroller/finalizer"
	// FentryProgramControllerFinalizer is the finalizer that holds a Fentry
	// BpfProgram object from deletion until cleanup can be performed.
	FentryProgramControllerFinalizer = "bpfman.io.fentryprogramcontroller/finalizer"
	// FexitProgramControllerFinalizer is the finalizer that holds a Fexit
	// BpfProgram object from deletion until cleanup can be performed.
	FexitProgramControllerFinalizer = "bpfman.io.fexitprogramcontroller/finalizer"
	// BpfApplicationFinalizer is the finalizer that holds a BpfApplication
	BpfApplicationControllerFinalizer = "bpfman.io.bpfapplicationcontroller/finalizer"
	// BpfApplicationFinalizer is the finalizer that holds a BpfApplication
	BpfNsApplicationControllerFinalizer = "bpfman.io.bpfnsapplicationcontroller/finalizer"
)

// Must match the kernel's `bpf_prog_type` enum.
// https://elixir.bootlin.com/linux/v6.4.4/source/include/uapi/linux/bpf.h#L948
type ProgramType int32

const (
	Unspec ProgramType = iota
	SocketFilter
	Kprobe
	Tc
	SchedAct
	Tracepoint
	Xdp
	PerfEvent
	CgroupSkb
	CgroupSock
	LwtIn
	LwtOut
	LwtXmit
	SockOps
	SkSkb
	CgroupDevice
	SkMsg
	RawTracepoint
	CgroupSockAddr
	LwtSeg6Local
	LircMode2
	SkReuseport
	FlowDissector
	CgroupSysctl
	RawTracepointWritable
	CgroupSockopt
	Tracing
	StructOps
	Ext
	Lsm
	SkLookup
	Syscall
)

func (p ProgramType) Uint32() *uint32 {
	progTypeInt := uint32(p)
	return &progTypeInt
}

func FromString(p string) (*ProgramType, error) {
	var programType ProgramType
	switch p {
	case "tc":
		programType = Tc
	case "xdp":
		programType = Xdp
	case "tracepoint":
		programType = Tracepoint
	case "kprobe":
		programType = Kprobe
	case "uprobe":
		programType = Kprobe
	case "fentry":
		programType = Tracing
	case "fexit":
		programType = Tracing
	default:
		return nil, fmt.Errorf("unknown program type: %s", p)
	}

	return &programType, nil
}

func (p ProgramType) String() string {
	switch p {
	case Unspec:
		return "unspec"
	case SocketFilter:
		return "socket_filter"
	case Kprobe:
		return "kprobe"
	case Tc:
		return "tc"
	case SchedAct:
		return "sched_act"
	case Tracepoint:
		return "tracepoint"
	case Xdp:
		return "xdp"
	case PerfEvent:
		return "perf_event"
	case CgroupSkb:
		return "cgroup_skb"
	case CgroupSock:
		return "cgroup_sock"
	case LwtIn:
		return "lwt_in"
	case LwtOut:
		return "lwt_out"
	case LwtXmit:
		return "lwt_xmit"
	case SockOps:
		return "sock_ops"
	case SkSkb:
		return "sk_skb"
	case CgroupDevice:
		return "cgroup_device"
	case SkMsg:
		return "sk_msg"
	case RawTracepoint:
		return "raw_tracepoint"
	case CgroupSockAddr:
		return "cgroup_sock_addr"
	case LwtSeg6Local:
		return "lwt_seg6local"
	case LircMode2:
		return "lirc_mode2"
	case SkReuseport:
		return "sk_reuseport"
	case FlowDissector:
		return "flow_dissector"
	case CgroupSysctl:
		return "cgroup_sysctl"
	case RawTracepointWritable:
		return "raw_tracepoint_writable"
	case CgroupSockopt:
		return "cgroup_sockopt"
	case Tracing:
		return "tracing"
	case StructOps:
		return "struct_ops"
	case Ext:
		return "ext"
	case Lsm:
		return "lsm"
	case SkLookup:
		return "sk_lookup"
	case Syscall:
		return "syscall"
	default:
		return "INVALID_PROG_TYPE"
	}
}

// Define a constant strings for Uprobe, Fentry and Fexit. Uprobe has the same
// kernel ProgramType as Kprobe, and Fentry and Fexit both have the Tracing
// ProgramType, so we can't use the ProgramType String() method above.
const UprobeString = "uprobe"
const FentryString = "fentry"
const FexitString = "fexit"
const ApplicationString = "application"
const TcxString = "tcx"

type ReconcileResult uint8

const (
	// No changes were made to k8s objects, and rescheduling another reconcile
	// is not necessary. The calling code may continue reconciling other
	// programs in it's list.
	Unchanged ReconcileResult = 0
	// Changes were made to k8s objects that we know will trigger another
	// reconcile. Calling code should stop reconciling additional programs and
	// return immediately to avoid multiple concurrent reconcile threads.
	Updated ReconcileResult = 1
	// A retry should be scheduled. This should only be used when "Updated"
	// doesn't apply, but we want to trigger another reconcile anyway. For
	// example, there was a transient error. The calling code may continue
	// reconciling other programs in it's list.
	Requeue ReconcileResult = 2
)

func (r ReconcileResult) String() string {
	switch r {
	case Unchanged:
		return "Unchanged"
	case Updated:
		return "Updated"
	case Requeue:
		return "Requeue"
	default:
		return fmt.Sprintf("INVALID RECONCILE RESULT (%d)", r)
	}
}
