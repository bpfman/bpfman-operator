The v0.6.0 release is a minor release with significant changes to the bpfman-operator. The operator now supports the load/attach split introduced in bpfman v0.6.0, allowing finer-grained control over BPF programme lifecycle. A new Config custom resource has been added, enabling runtime configuration of operator and agent behaviour including component image overrides and health probe settings; the Config CR is now bootstrapped automatically by the operator on startup. The dependency on security-profiles-operator has been fully removed. Metrics collection has been decoupled from hostNetwork using a proxy DaemonSet, and kube-rbac-proxy has been replaced with controller-runtime's built-in TLS capabilities. Network interface autodiscovery with match and regex filtering has been added for TC, TCX, and XDP programmes. The operator now uses a native crictl implementation, removing the external dependency. BpfApplicationState spec fields have been moved to status, and ContainerSelector has been replaced with NetworkNamespaceSelector for network-attached programme types. Priority fields for TC, TCX, and XDP programmes have been changed from int32 to *int32. Build version information is now embedded in all binaries. OLM bundle deployment has been fixed for vanilla Kubernetes with runtime reconciliation of monitoring and RBAC resources. Container images have been switched from Docker Hub golang to Red Hat UBI go-toolset, and hard-coded imagePullPolicy values have been removed from deployment manifests. CI workflows have been updated to resolve Node.js 20 deprecation warnings and improve caching.

## What's Changed
* bpfman-operator: Support Load/Attach Split by @anfredette in https://github.com/bpfman/bpfman-operator/pull/347
* remove dependency on security-profiles-operator by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/373
* update openshift bundle containerfile by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/377
* rename bundle containerfile to Containerfile.bundle to be consistent by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/378
* revert #378 by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/379
* fix some warnings in community-operators-prod by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/380
* chore: Bump Go to 1.24 by @dave-tucker in https://github.com/bpfman/bpfman-operator/pull/383
* Make sure changing bpfman configmap restart daemonset pods by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/389
* remove xdp go counter intg test case by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/406
* Add back xdp go counter by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/407
* Add interfaces discovery to bpf agent process by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/408
* Make the bpfman-operator report status of "Pending" instead of "Error" when appropriate by @anfredette in https://github.com/bpfman/bpfman-operator/pull/409
* Change ContainerSelector to NetworkNamespaceSelector for XDP, TC, and TCX by @anfredette in https://github.com/bpfman/bpfman-operator/pull/411
* Intial APIs review for bpfman-operator by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/413
* revert back to go1.23 and pin cri-tool to v1.32.0 by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/416
* 2nd round of APIs reviews by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/417
* docs: fix broken link in README by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/418
* api review: update field descriptions by @Billy99 in https://github.com/bpfman/bpfman-operator/pull/419
* Fix validation for EBPFProgType in BpfApplication/BpfApplicationState objects by @anfredette in https://github.com/bpfman/bpfman-operator/pull/420
* Add intefaces match and regex filter to autodiscovery configs by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/421
* Move BpfApplicationState Spec to Status by @anfredette in https://github.com/bpfman/bpfman-operator/pull/423
* split out interface discovery processing by @msherif1234 in https://github.com/bpfman/bpfman-operator/pull/424
* Check for Duplicate Network Namespace Inodes by @anfredette in https://github.com/bpfman/bpfman-operator/pull/426
* Remove OpenShift-specific Containerfiles by @frobware in https://github.com/bpfman/bpfman-operator/pull/430
* chore: Update to golang-ci-lint v2 by @dave-tucker in https://github.com/bpfman/bpfman-operator/pull/431
* Makefile: wait for ConfigMap deletion before undeploy by @frobware in https://github.com/bpfman/bpfman-operator/pull/436
* Replace kube-rbac-proxy with controller-runtime's built-in TLS capabilities by @frobware in https://github.com/bpfman/bpfman-operator/pull/437
* Switch to Go 1.24 (again) by @frobware in https://github.com/bpfman/bpfman-operator/pull/439
* Improve bundle versioning using VERSION file by @frobware in https://github.com/bpfman/bpfman-operator/pull/440
* Add DeepWiki badge by @anfredette in https://github.com/bpfman/bpfman-operator/pull/441
* Decouple metrics from hostNetwork using proxy DaemonSet by @frobware in https://github.com/bpfman/bpfman-operator/pull/443
* BpfApplicationReconcilers: wrap loadRequest error by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/448
* Complete removal of security-profiles-operator dependency by @frobware in https://github.com/bpfman/bpfman-operator/pull/451
* Fix SEGV (nil pointer access) in getBpfAppState logging functions by @frobware in https://github.com/bpfman/bpfman-operator/pull/452
* Replace external crictl dependency with native bpfman-crictl implementation by @frobware in https://github.com/bpfman/bpfman-operator/pull/453
* Disable controller-runtime metrics server in bpfman-agent by @frobware in https://github.com/bpfman/bpfman-operator/pull/455
* Disable profiling port in bpfman-agent to avoid host network conflicts by @frobware in https://github.com/bpfman/bpfman-operator/pull/458
* bpfman-operator: Add Config resource and update reconciler and tests by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/461
* Add make target to run operator locally by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/466
* Daemonset: Move bidirectional mount from /run/bpfman to /run/bpfman/csi by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/469
* Config follow-up: Implement component override functionality for Config resource by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/475
* Makefile: Target run-local, ignore scaling issues by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/476
* Fix overlaying bpf mount by @nocturo in https://github.com/bpfman/bpfman-operator/pull/477
* Config: Change healthProbeAddr to healthProbePort by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/478
* DaemonSet: Remove /var/lib/bpfman from mounts by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/479
* Work around sigstore-rs rekor issue in integration tests by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/482
* Revert "Work around sigstore-rs rekor issue in integration tests" by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/483
* Convert the Priority field from int32 to *int32 across all BPF program types by @andreaskaris in https://github.com/bpfman/bpfman-operator/pull/485
* Add configurable daemon images to Config API by @frobware in https://github.com/bpfman/bpfman-operator/pull/488
* Add bpffs mount capability to bpfman-agent for init container use by @frobware in https://github.com/bpfman/bpfman-operator/pull/490
* Remove BpffsInitImage configuration option by @frobware in https://github.com/bpfman/bpfman-operator/pull/491
* Reverse program unload order to respect map ownership by @frobware in https://github.com/bpfman/bpfman-operator/pull/493
* chore(deps): bump the production-dependencies group with 16 updates (with fixes) by @frobware in https://github.com/bpfman/bpfman-operator/pull/494
* Wait for BPF program success before expecting userspace pods by @frobware in https://github.com/bpfman/bpfman-operator/pull/495
* Bootstrap Config CR from operator on startup by @frobware in https://github.com/bpfman/bpfman-operator/pull/498
* Fix OLM bundle deployment on vanilla Kubernetes by @frobware in https://github.com/bpfman/bpfman-operator/pull/501
* Fix operator-sdk download check to use correct binary name by @frobware in https://github.com/bpfman/bpfman-operator/pull/502
* Remove hard-coded imagePullPolicy from deployment manifests by @frobware in https://github.com/bpfman/bpfman-operator/pull/503
* Manage kind as a versioned local dependency by @frobware in https://github.com/bpfman/bpfman-operator/pull/505
* Embed build version information in all binaries by @frobware in https://github.com/bpfman/bpfman-operator/pull/506
* Switch build images from Docker Hub golang to Red Hat UBI go-toolset by @frobware in https://github.com/bpfman/bpfman-operator/pull/507
* Bump actions/* to resolve Node.js 20 deprecation warnings by @frobware in https://github.com/bpfman/bpfman-operator/pull/508
* Improve CI workflow caching and job ordering by @frobware in https://github.com/bpfman/bpfman-operator/pull/509

**Full Changelog**: https://github.com/bpfman/bpfman-operator/compare/v0.5.6...v0.6.0

## New Contributors
* @andreaskaris made their first contribution in https://github.com/bpfman/bpfman-operator/pull/448
* @nocturo made their first contribution in https://github.com/bpfman/bpfman-operator/pull/477
