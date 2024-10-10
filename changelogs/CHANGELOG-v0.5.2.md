The bpfman-operator v0.5.2 release is a patch release that contains several
minor internal updates and is being made primarily to keep in sync with the
bpfman v0.5.2 release.

## What's Changed
* Makefile: ensure `run-on-kind` works without local images by @frobware in https://github.com/bpfman/bpfman-operator/pull/111
* update containers/image golang pkg to fix a CVE by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/123
* Bump crictl to v1.31.0 by @anfredette in https://github.com/bpfman/bpfman-operator/pull/126
* Fix BUILDPLATFORM redefinition issue in some Containerfiles by @frobware in https://github.com/bpfman/bpfman-operator/pull/121
* ci: make sure BUILDPLATFORM is set in github actions by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/136
* Makefile: Split build image tasks for better modularity by @frobware in https://github.com/bpfman/bpfman-operator/pull/129
* fix GH actions by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/147
* ci: run image-build.yaml when Containerfiles are updated by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/160
* RPM prefetch for the catalog build by @ralphbean in https://github.com/bpfman/bpfman-operator/pull/210
* ci: add Podman validation to the build-images step by @frobware in https://github.com/bpfman/bpfman-operator/pull/130

## New Contributors
* @ralphbean made their first contribution in https://github.com/bpfman/bpfman-operator/pull/210

**Full Changelog**: https://github.com/bpfman/bpfman-operator/compare/v0.5.1...v0.5.2
