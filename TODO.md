The code has support for XDP, TCX, and Fentry programs in a BpfApplication.

It's written so that Dave's load/attach split code should drop in pretty easily,
but it's not using it yet.  I'm simulating the attachments by reloading the code
for each attachment (like we do today).

The new code is mainly in these directories:

Updated & working APIs:

- apis/v1alpha1/fentryProgram_types.go
- apis/v1alpha1/xdpProgram_types.go
- apis/v1alpha1/tcxProgram_types.go
- apis/v1alpha1/bpfApplication_types.go
- apis/v1alpha1/bpfApplicationState_types.go

Note: the rest are partially updated.

New Agent:

- controllers/app-agent

New Operator:

- controllers/app-operator

Note: I left the old bpfman-agent and bpfman-operator code unchanged (except as
needed due to CRD changed).  It should work, but it's not being initialized when
we run the operator.

Testing:

- Unit tests for the agent and the operator
- The following working samples:
    - config/samples/bpfman.io_v1alpha1_bpfapplication.yaml (XDP & TCX)
    - config/samples/bpfman.io_v1alpha1_fentry_bpfapplication.yaml (Fentry)

TODO:

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
