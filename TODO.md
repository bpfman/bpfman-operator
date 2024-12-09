# Intro

The code has support for:
- XDP, TCX, and Fentry programs in the cluster-scoped BpfApplication, and 
- XDP and TCX in the namespace-scoped BpfNsApplication

It's written so that Dave's load/attach split code should drop in pretty easily,
but it's not using it yet.  I'm simulating the attachments by reloading the code
for each attachment (like we do today).

# Testing:

- Unit tests for the agent and the operator
- The following working samples contain all program types:
    - config/samples/bpfman.io_v1alpha1_bpfapplication.yaml
    - config/samples/bpfman.io_v1alpha1_fentry_bpfnsapplication.yaml

# TODO:

(In no particular order.)

- ~~Implement Fentry/Fexit solution.~~
- Integrate with the new bpfman code with load/attach split (of course)
- ~~Create a bpf.o file with all the application types for both cluster and~~
  ~~namespace scoped BpfApplicaitons.~~
- Review the status/condition values.  I redid most of the *ApplicationState
  item status values, but the actual overall status is still using an existing
  ReconcileSuccess value.
- Review all comments and logs.
- ~~Support namespace-scoped BPF Application CRD~~
- ~~Support the rest of the program types.~~
- ~~Delete old directories.~~
- Rename cluster-scoped CRDs and structures to "Cluster*", and remove "ns" from
  namespace-scoped CRDs and structures.
- Make sure we have all the old sample programs covered by BpfApplication-based
  sample programs and then delete the *Program-based ones.
- More cleanup and testing.