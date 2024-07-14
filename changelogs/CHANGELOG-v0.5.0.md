
The v0.5.0 release is a minor release and is the first official release with
bpfman-operator in a separate repo.

The key new feature in this release is support for the BpfApplication CRD, which
allows an application developer to deploy a set of required eBPF programs using
a single BpfApplication CRD rather than multiple separate *Program CRDs.

## What's Changed
* SDN-5007: introducing bfpapplication object by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/6
* Fixups for pr #6 by @anfredette in https://github.com/bpfman/bpfman-operator/pull/31
* build(deps): bump the production-dependencies group with 2 updates by @dependabot in https://github.com/bpfman/bpfman-operator/pull/32
* Add app integration test by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/23
* remove api doc gen by @astoycos in https://github.com/bpfman/bpfman-operator/pull/34
* fix broken link the readme and few minor edits by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/33
* build(deps): bump google.golang.org/grpc from 1.64.0 to 1.65.0 in the production-dependencies group by @dependabot in https://github.com/bpfman/bpfman-operator/pull/39
* handle update app object by removing an existing program by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/38
* More concise naming scheme for BpfPrograms by @anfredette in https://github.com/bpfman/bpfman-operator/pull/37
* update build-release-yamls target by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/42
* Add make catalog-deploy to allow loading dev operator from console by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/43
* Add missing schema for BpfApplication object by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/46

**Full Changelog**: https://github.com/bpfman/bpfman-operator/compare/v0.4.2...v0.5.0

