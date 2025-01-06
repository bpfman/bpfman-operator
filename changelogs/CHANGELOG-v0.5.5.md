The v0.5.5 release is a patch release that introduced the TCX program type, support for attaching TCX, TC and XDP programs inside containers, improved logs, and Namespace scoped CRDs, which are a subset of the existing Cluster scoped CRDs.

## What's Changed
* TCX ebpf hook support by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/102
* panic in bpfman-agent due to err not being returned by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/330
* OCPBUGS-42593: hardening service account automount by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/327
* Create scorecard.yml by @anfredette in https://github.com/bpfman/bpfman-operator/pull/334
* Fix scorecard badge URLs in README by @anfredette in https://github.com/bpfman/bpfman-operator/pull/335
* interface's based hooks don't allow more than one function per intf per direction by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/338
* logs: simplify the bpfman-agent logs by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/336
* Add support for attaching TCX, TC and XDP programs inside containers. by @anfredette in https://github.com/bpfman/bpfman-operator/pull/345
* Add unit test for Uprobe in container by @anfredette in https://github.com/bpfman/bpfman-operator/pull/349
* Add core code to support namespace based CRDs by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/344
* Makefile: Export BPFMAN_AGENT_IMG and BPFMAN_OPERATOR_IMG by @frobware in https://github.com/bpfman/bpfman-operator/pull/70
* Add sample kprobe yaml with 9 global variables by @anfredette in https://github.com/bpfman/bpfman-operator/pull/339
* Fix intermittent error when deleting programs by @anfredette in https://github.com/bpfman/bpfman-operator/pull/354

**Full Changelog**: https://github.com/bpfman/bpfman-operator/compare/v0.5.4...v0.5.5