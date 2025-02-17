The v0.5.6 release is a patch release. The primary reason for the release is to move the version of sigstore-rs because of a breaking change which caused all previous bpfman versions to no longer be able to verify images. The CSI driver container used by bpfman-operator was also updated to support multiple architectures.

## What's Changed
* post v0.5.5 cleanup by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/360
* update golang to 1.23 and build crictl from source by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/362
* ci: upgrade upload-artifact action to v4 in scorecard workflow by @frobware in https://github.com/bpfman/bpfman-operator/pull/367
* update csi-node-driver-registrar container for multiarch by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/370
* docs: move bpfman-operator details from README to docs by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/366
* Add "patch" to verbs for namespace-scoped CRDs in namespace_scoped.yaml by @anfredette in https://github.com/bpfman/bpfman-operator/pull/372
* Fix random failures in unit tests by @anfredette in https://github.com/bpfman/bpfman-operator/pull/371
* Add license scan report and status by @fossabot in https://github.com/bpfman/bpfman-operator/pull/374

**Full Changelog**: https://github.com/bpfman/bpfman-operator/compare/v0.5.5...v0.5.6

## New Contributors
* @fossabot made their first contribution in https://github.com/bpfman/bpfman-operator/pull/374