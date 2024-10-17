The v0.5.1 release is a patch release which mainly contains bug fixes and enhances the developer build and test process.

## What's Changed
* Red Hat Konflux update bpfman-operator by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/35
* Update Konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/48
* Update Konflux references to 0dc3087 by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/49
* Update Konflux references to 9eee3cf by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/51
* Add pprof port to bpfagent controller by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/40
* Red Hat Konflux update bpfman-agent by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/52
* Update Konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/53
* Update Konflux references to 71270c3 by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/54
* Update Konflux references to 72e4ddd by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/55
* build(deps): bump the production-dependencies group with 4 updates by @dependabot in https://github.com/bpfman/bpfman-operator/pull/58
* Update Konflux references to v0.2 by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/57
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/60
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/63
* Fix OOM because of watching configmap resource by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/62
* Makefile: gracefully handle kubectl delete by @frobware in https://github.com/bpfman/bpfman-operator/pull/61
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/64
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/66
* chore(deps): update konflux references to 3806116 by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/67
* Red Hat Konflux update bpfman-operator-bundle by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/68
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/69
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/71
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/72
* chore(deps): update konflux references to f93024e by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/73
* bpfman-agent: don't try to unload bpf program that isn't loaded by @anfredette in https://github.com/bpfman/bpfman-operator/pull/75
* Konflux catalog by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/76
* Handle deletes of BpfPrograms for BpfApplications by @anfredette in https://github.com/bpfman/bpfman-operator/pull/78
* Update TestBpfmanConfigReconcileAndDelete unit test to verify that the OpenShift SCC is deployed by @frobware in https://github.com/bpfman/bpfman-operator/pull/65
* Red Hat Konflux update bpfman-operator-catalog by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/77
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/79
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/80
* Fix FromAsCasing warnings in container files by @frobware in https://github.com/bpfman/bpfman-operator/pull/81
* Add catalog index file and script to generate it by @OlivierCazade in https://github.com/bpfman/bpfman-operator/pull/82
* Update maintainers in yaml files by @anfredette in https://github.com/bpfman/bpfman-operator/pull/83
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/84
* chore(deps): update konflux references by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/89
* Speed Up `bpfman-agent` and `bpfman-operator` Build by @frobware in https://github.com/bpfman/bpfman-operator/pull/95
* Add downstream container files and update the konflux pipeline by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/90
* Change Andrew's email back to redhat by @anfredette in https://github.com/bpfman/bpfman-operator/pull/100
* chore(deps): update registry.access.redhat.com/ubi9/ubi-minimal docker tag to v9.4-1194 by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/99
* Enable Integration Tests to Use Podman-Built Images by @frobware in https://github.com/bpfman/bpfman-operator/pull/98
* Makefile: Disable test caching by using go test -count=1 by @frobware in https://github.com/bpfman/bpfman-operator/pull/97
* chore(deps): update quay.io/operator-framework/opm docker tag to v1.46.0 by @red-hat-konflux in https://github.com/bpfman/bpfman-operator/pull/107
* build(deps): bump the production-dependencies group with 4 updates by @dependabot in https://github.com/bpfman/bpfman-operator/pull/108

## New Contributors
* @frobware made their first contribution in https://github.com/bpfman/bpfman-operator/pull/61
* @OlivierCazade made their first contribution in https://github.com/bpfman/bpfman-operator/pull/82

**Full Changelog**: https://github.com/bpfman/bpfman-operator/compare/v0.5.0...v0.5.1

## Known Issues
* Release v0.5.1 and earlier do not work due to a change in the Cosign API
  ([Issue 1241](https://github.com/bpfman/bpfman/issues/1241)).  In follow-on
  releases, whether to use Cosign was made configurable, but for release v0.5.1
  and earlier, there is no workaround other than to use a later release.