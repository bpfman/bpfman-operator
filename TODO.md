# Intro

The code has support for XDP, TCX, and Fentry programs in a BpfApplication.

It's written so that Dave's load/attach split code should drop in pretty easily,
but it's not using it yet.  I'm simulating the attachments by reloading the code
for each attachment (like we do today).

# Observation/Question
Fentry and Fexit programs break the mold.

Fentry and Fexit programs need to be loaded separately for each attach point, so
the user must specify the BPF function name and attach point together for each
attachment.  The user can then attach or detach that program later, but if the
user wants to attach the same Fentry/Fexit program to a different attach point,
the program must be loaded again with the new attach point.

For other program types, the user can load a program, and then attach or detach
the program to/from multiple attach points at any time after it has been loaded.

Some differences that result from these requirements:
- Each Fentry/Fexit attach point results in a unique bpf program ID (even if
  they all use the same bpf function)
- For other types, a given bpf program can have one bpf program ID (assigned
  when it's loaded), plus multiple attach IDs (assigened when it is attached)
- We don't need an attach ID for Fentry/Fexit programs.

We need to do one of the following:
- Not support the user modifying Fentry/Fexit attach points after the initial
  BpfApplication load.
- Load the program if they add an attach point (which would result in an
  independent set of global data), and unload the program if they remove an
  attachment.

Yaml options:

**Option 1:**  The current design uses a map indexed by the bpffunction name, so
we can only list a given bpffunction name once followed by a list of attach
points as shown below.  This represents Fentry/Fexit programs the same way as
others, but they would need to behave differently as outlined above.

```yaml
programs:
  tcx_stats:
    type: TCX
    tcx:
      attach_points:
        - interfaceselector:
            primarynodeinterface: true
          priority: 500
          direction: ingress
        - interfaceselector:
            interfaces:
              - eth1
          priority: 100
          direction: egress
  test_fentry:
    type: Fentry
    fentry:
      attach_points:
        - function_name: do_unlinkat
          attach: true
        - function_name: tcp_connect
          attach: false
```

**Options 2:**  Use a slice, and allow the same Fentry/Fexit functions to be
included multiple times. The is more like the bpfman api, but potentially more
cumbersome for Fentry/Fexit programs.

```yaml
  programs:
    - type: TCX
      tcx:
        bpffunctionname: tcx_stats
        attach_points:
          - interfaceselector:
              primarynodeinterface: true
            priority: 500
            direction: ingress
          - interfaceselector:
              interfaces:
                - eth0
            priority: 100
            direction: egress
            containers:
              namespace: bpfman
              pods:
                matchLabels:
                  name: bpfman-daemon
              containernames:
                - bpfman
                - bpfman-agent
    - type: Fentry
      fentry:
        bpffunctionname: tcx_stats
        function_name: do_unlinkat
        attach: true
    - type: Fentry
      fentry:
        bpffunctionname: tcx_stats
        function_name: tcp_connect
        attach: true
```

# New Code

The new code is mainly in these directories:

## Updated & working APIs:

- apis/v1alpha1/fentryProgram_types.go
- apis/v1alpha1/xdpProgram_types.go
- apis/v1alpha1/tcxProgram_types.go
- apis/v1alpha1/bpfApplication_types.go
- apis/v1alpha1/bpfApplicationState_types.go

Note: the rest are partially updated.

## New Agent:

- controllers/app-agent

## New Operator:

- controllers/app-operator

Note: I left the old bpfman-agent and bpfman-operator code unchanged (except as
needed due to CRD changed).  It should work, but it's not being initialized when
we run the operator.

# Testing:

- Unit tests for the agent and the operator
- The following working samples:
    - config/samples/bpfman.io_v1alpha1_bpfapplication.yaml (XDP & TCX)
    - config/samples/bpfman.io_v1alpha1_fentry_bpfapplication.yaml (Fentry)

# TODO:

(In no particular order.)

- Implement Fentry/Fexit solution.
- Integrate with the new bpfman code with load/attach split (of course)
- Create a bpf.o file with all the application types for both cluster and
  namespace scoped BpfApplicaitons.
- Redo the status/condition values.  I’m currently using the existing framework
  and some values for status/conditions, but I intend to create a new set of
  conditions/status values that make more sense for the new design.
- Review all comments and logs.
- Maybe make more code common.
- Support the rest of the program types (including namespace-scoped CRDs).
- Delete old directories.
- Lots more testing and code cleanup.
- Lots of other stuff (I'm sure).
