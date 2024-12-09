# Intro

The operator is integrated with Dave's load/attach split code.

# Testing:

- Unit tests for the agent and the operator
- All sample yamls work (except for bpfman.io_v1alpha1_test1_clusterbpfapplication.yaml which is a negative test.)

# TODO:

(In no particular order.)

- Scrub logs (need to remove some and make some Info logs Debug)
- Figure out why `k apply -f
  config/samples/bpfman.io_v1alpha1_test1_clusterbpfapplication.yaml` works for
  kprobe, fentry and fexit, but is rejected for the other program types.
- Delete the "test" sample programs when I'm done using them for testing.
- Get the bpfman examples and regression tests working (probably after)

# TODO (After merge)
- Possibly more of the scrubbing mentioned above.
- Make more of the bpfman-agent controller code common.
- Change ContainerSelector to PodSelector for xdp, tc and tcx programs. xdp, tc
  and tcx programs can't attach to multiple containers in a pod because they are
  all in the same namespace.  Technically, xdp and tc can because of the way we
  implemented the dispatchers, but it probably doesn't make sense.  Tcx errors
  out if you try to do it.
- Update documentation