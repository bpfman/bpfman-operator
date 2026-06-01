# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
ifeq ($(origin VERSION), undefined)
  VERSION := $(shell cat VERSION 2>/dev/null || echo "0.0.0-unknown")
endif


# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# bpfman.io/bpfman-operator-bundle:$VERSION and bpfman.io/bpfman-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/bpfman/bpfman-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif
IMAGE_TAG ?= latest
# Default upstream image references. Override via environment or .envrc
# for local development (for example, to point at a private registry).
BPFMAN_IMG ?= quay.io/bpfman/bpfman:$(IMAGE_TAG)
BPFMAN_AGENT_IMG ?= quay.io/bpfman/bpfman-agent:$(IMAGE_TAG)
BPFMAN_OPERATOR_IMG ?= quay.io/bpfman/bpfman-operator:$(IMAGE_TAG)
KIND_CLUSTER_NAME ?= bpfman-deployment
KIND_REGISTRY_NAME ?= kind-registry
KIND_REGISTRY_PORT ?= 5001
KIND_BUNDLE_IMG ?= localhost:$(KIND_REGISTRY_PORT)/bpfman-operator-bundle:latest
KIND_BUNDLE_PULL_FLAGS ?= --use-http

# These environment variables must be exported so they are
# available to subprocesses (integration tests, run-local, etc.).
export BPFMAN_IMG
export BPFMAN_AGENT_IMG
export BPFMAN_OPERATOR_IMG

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.35.0

.DEFAULT_GOAL := help

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Image building tool (docker / podman) - docker is preferred in CI
OCI_BIN_PATH := $(shell which docker 2>/dev/null || which podman)
OCI_BIN ?= $(shell basename ${OCI_BIN_PATH})
export OCI_BIN

GOARCH ?= $(shell go env GOHOSTARCH)
PLATFORM ?= $(shell go env GOHOSTOS)/$(shell go env GOHOSTARCH)

## Version ldflags
VERSION_PKG := github.com/bpfman/bpfman-operator/internal/version
BUILD_VERSION ?= $(shell git describe --tags --dirty --always --long 2>/dev/null || echo 0.0.0-unknown)
BUILD_COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_LDFLAGS := -X '$(VERSION_PKG).buildVersion=$(BUILD_VERSION)' -X '$(VERSION_PKG).buildCommit=$(BUILD_COMMIT)' -X '$(VERSION_PKG).buildDate=$(BUILD_DATE)'

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: version
version: ## Display the current VERSION, IMAGE_TAG, and image paths being used
	@echo "BPFMAN_AGENT_IMG: $(BPFMAN_AGENT_IMG)"
	@echo "BPFMAN_IMG: $(BPFMAN_IMG)"
	@echo "BPFMAN_OPERATOR_IMG: $(BPFMAN_OPERATOR_IMG)"
	@echo "BUILD_COMMIT: $(BUILD_COMMIT)"
	@echo "BUILD_DATE: $(BUILD_DATE)"
	@echo "BUILD_VERSION: $(BUILD_VERSION)"
	@echo "BUNDLE_IMG: $(BUNDLE_IMG)"
	@echo "CATALOG_IMG: $(CATALOG_IMG)"
	@echo "IMAGE_TAG: $(IMAGE_TAG)"
	@echo "VERSION: $(VERSION)"

##@ Local Dependencies

## Location to install dependencies to
LOCALBIN ?= bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Allows building bundles in Mac replacing BSD 'sed' command by GNU-compatible 'gsed'
ifeq (,$(shell which gsed 2>/dev/null))
SED ?= sed
else
SED ?= gsed
endif

## Tool Binaries
KUSTOMIZE ?= go run sigs.k8s.io/kustomize/kustomize/v5
CONTROLLER_GEN ?= go run sigs.k8s.io/controller-tools/cmd/controller-gen
REGISTER_GEN ?= go run k8s.io/code-generator/cmd/register-gen
INFORMER_GEN ?= go run k8s.io/code-generator/cmd/informer-gen
LISTER_GEN ?= go run k8s.io/code-generator/cmd/lister-gen
CLIENT_GEN ?= go run k8s.io/code-generator/cmd/client-gen
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
KIND ?= $(LOCALBIN)/kind
OPM ?= $(LOCALBIN)/opm

## Tool Versions
OPERATOR_SDK_VERSION ?= v1.37.0
KIND_VERSION ?= v0.31.0
OPM_VERSION ?= v1.45.0
GOLANGCI_LINT_VERSION = v2.12.2


OPERATOR_SDK_DL_NAME=operator-sdk_$(shell go env GOOS)_$(shell go env GOARCH)
OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/$(OPERATOR_SDK_DL_NAME)
$(OPERATOR_SDK): | $(LOCALBIN)
	curl -fLo $@.tmp $(OPERATOR_SDK_DL_URL) && \
	  chmod +x $@.tmp && \
	  mv $@.tmp $@

KIND_DL_NAME=kind-$(shell go env GOOS)-$(shell go env GOARCH)
KIND_DL_URL=https://github.com/kubernetes-sigs/kind/releases/download/$(KIND_VERSION)/$(KIND_DL_NAME)
$(KIND): | $(LOCALBIN)
	curl -fLo $@.tmp $(KIND_DL_URL) && \
	  chmod +x $@.tmp && \
	  mv $@.tmp $@

OPM_DL_NAME=$(shell go env GOOS)-$(shell go env GOARCH)-opm
OPM_DL_URL=https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$(OPM_DL_NAME)
$(OPM): | $(LOCALBIN)
	curl -fLo $@.tmp $(OPM_DL_URL) && \
	  chmod +x $@.tmp && \
	  mv $@.tmp $@

##@ Development

VERIFY_CODEGEN ?= false
ifeq ($(VERIFY_CODEGEN), true)
VERIFY_FLAG=--verify-only
endif

PKG ?= github.com/bpfman/bpfman-operator
COMMON_FLAGS ?= ${VERIFY_FLAG} --go-header-file $(shell pwd)/hack/boilerplate.go.txt

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=agent-role paths="./controllers/bpfman-agent/..." output:rbac:artifacts:config=config/rbac/bpfman-agent
	$(CONTROLLER_GEN) rbac:roleName=operator-role paths="./controllers/bpfman-operator" output:rbac:artifacts:config=config/rbac/bpfman-operator

.PHONY: generate
generate: manifests generate-register generate-deepcopy generate-typed-clients generate-typed-listers generate-typed-informers ## Generate ALL auto-generated code.

.PHONY: generate-register
generate-register: ## Generate register code see all `zz_generated.register.go` files.
	$(REGISTER_GEN) \
		"${PKG}/apis/v1alpha1" \
		--output-file zz_generated.register.go \
		--go-header-file ./hack/boilerplate.go.txt \
		${COMMON_FLAGS}

.PHONY: generate-deepcopy
generate-deepcopy: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations see all `zz_generated.register.go` files.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-typed-clients
generate-typed-clients: ## Generate typed client code
	$(CLIENT_GEN) \
		--clientset-name "clientset" \
		--input-base "" \
		--input "${PKG}/apis/v1alpha1" \
		--output-pkg "${PKG}/pkg/client" \
		--output-dir "./pkg/client" \
		${COMMON_FLAGS}


.PHONY: generate-typed-listers
generate-typed-listers: ## Generate typed listers code
	$(LISTER_GEN) \
		"${PKG}/apis/v1alpha1" \
		--output-pkg "${PKG}/pkg/client" \
		--output-dir "./pkg/client" \
		${COMMON_FLAGS}


.PHONY: generate-typed-informers
generate-typed-informers: ## Generate typed informers code
	$(INFORMER_GEN) \
		"${PKG}/apis/v1alpha1" \
		--versioned-clientset-package "${PKG}/pkg/client/clientset" \
		--listers-package "${PKG}/pkg/client" \
		--output-pkg "${PKG}/pkg/client" \
		--output-dir "./pkg/client" \
		${COMMON_FLAGS}

.PHONY: vendors
vendors: ## Refresh vendors directory.
	@echo "### Checking vendors"
	go mod tidy && go mod vendor

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: verify
verify: ## Verify all the autogenerated code
	./hack/verify-codegen.sh

.PHONY: prereqs
prereqs:
	@echo "### Test if prerequisites are met, and installing missing dependencies"
	GOFLAGS="" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}

.PHONY: lint
lint: prereqs ## Run linter (golangci-lint).
	@echo "### Linting code"
	golangci-lint run --timeout 5m ./...

# Use bpfman/scripts/verify-golint.sh for local linting verification
# .PHONY: lint
# lint: ## Run golang-ci linter
# 	./hack/verify-golint.sh

.PHONY: test
test: fmt ## Run Unit tests.
	@set -e ; \
	KUBEBUILDER_ASSETS="$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(abspath $(LOCALBIN)) -p path)" ; \
	if [ -z "$$KUBEBUILDER_ASSETS" ]; then \
		echo "setup-envtest produced an empty KUBEBUILDER_ASSETS path" >&2 ; \
		exit 1 ; \
	fi ; \
	echo KUBEBUILDER_ASSETS=\"$$KUBEBUILDER_ASSETS\" go test ./... -coverprofile cover.out ; \
	KUBEBUILDER_ASSETS="$$KUBEBUILDER_ASSETS" go test ./... -coverprofile cover.out


.PHONY: test-integration
test-integration: patch-image-references ## Run Integration tests.
	GOFLAGS="-tags=integration_tests" go test -count=1 -race -v ./test/integration/...

.PHONY: test-integration-local
test-integration-local: ## Run Integration tests against existing deployment. Use TEST= to specify test pattern.
	USE_EXISTING_KIND_CLUSTER=$(shell kubectl config current-context | sed 's/kind-//') \
	SKIP_BPFMAN_DEPLOY=true \
	GOFLAGS="-tags=integration_tests" go test -count=1 -race -v ./test/integration $(if $(TEST),-run $(TEST),)

.PHONY: test-integration-openshift
test-integration-openshift: ## Run Integration tests against the OpenShift cluster in the current kubeconfig context (assumes bpfman is already deployed). Use TEST= to specify test pattern.
	USE_EXISTING_CLUSTER=true \
	SKIP_BPFMAN_DEPLOY=true \
	GOFLAGS="-tags=integration_tests" go test -count=1 -race -v ./test/integration $(if $(TEST),-run $(TEST),)

.PHONY: test-lifecycle-local
test-lifecycle-local: ## Run lifecycle tests against existing deployment.
	GOFLAGS="-tags=integration_tests" go test -count=1 -race -v ./test/lifecycle

## The physical bundle is no longer tracked in git since it should be considered
## and treated as a release artifact, rather than something that's updated
## as part of a pull request.
## See https://github.com/operator-framework/operator-sdk/issues/6285.
.PHONY: bundle
bundle: $(OPERATOR_SDK) generate manifests patch-image-references ## Generate bundle manifests and metadata, then validate generated files.
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	# Dependency on security-profiles-operator removed (file renamed to dependencies.yaml.disabled)
	# https://github.com/kubernetes-sigs/security-profiles-operator/issues/2699
	# cp config/manifests/dependencies.yaml bundle/metadata/
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: build-release-yamls
build-release-yamls: generate ## Generate the crd install bundle for a specific release version.
	VERSION=$(VERSION) ./hack/build-release-yamls.sh

.PHONY: default-config
default-config: ## Print the default Config CR as YAML using current BPFMAN_IMG and BPFMAN_AGENT_IMG.
	@BPFMAN_IMG=$(BPFMAN_IMG) BPFMAN_AGENT_IMG=$(BPFMAN_AGENT_IMG) go run ./cmd/bpfman-operator default-config

.PHONY: run-local
run-local: build ## Run the bpfman-operator locally for development purposes.
	kubectl scale deployment -n bpfman bpfman-operator --replicas=0 || true
	GO_LOG=debug bin/bpfman-operator

##@ Build

.PHONY: build
build: fmt ## Build bpfman-operator, bpfman-agent, and bpfman-crictl binaries.
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -ldflags "$(GO_LDFLAGS)" -o bin/bpfman-operator cmd/bpfman-operator/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -ldflags "$(GO_LDFLAGS)" -o bin/bpfman-agent cmd/bpfman-agent/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -ldflags "$(GO_LDFLAGS)" -o bin/metrics-proxy cmd/metrics-proxy/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -ldflags "$(GO_LDFLAGS)" -o bin/bpfman-crictl cmd/bpfman-crictl/main.go

# These paths map the host's GOCACHE location to the container's
# location. We want to mount the host's Go cache in the container to
# speed up builds, particularly during development. Only podman (i.e.,
# not Docker) permits volumes to be mapped for builds so we do this
# conditionally.
ifeq ($(OCI_BIN),podman)
LOCAL_GOCACHE_PATH ?= $(shell go env GOCACHE)
CONTAINER_GOCACHE_PATH ?= /root/.cache/go-build
$(shell mkdir -p $(LOCAL_GOCACHE_PATH))
endif

.PHONY: build-images
build-images: build-operator-image build-agent-image ## Build bpfman-agent and bpfman-operator images.

.PHONY: build-operator-image
build-operator-image: ## Build bpfman-operator image.
	$(if $(filter $(OCI_BIN),podman), \
	  @echo "Adding GOCACHE volume mount $(LOCAL_GOCACHE_PATH):$(CONTAINER_GOCACHE_PATH).")
	$(OCI_BIN) version
	$(OCI_BIN) buildx build --load -t ${BPFMAN_OPERATOR_IMG} \
	  --build-arg TARGETPLATFORM=linux/$(GOARCH) \
	  --build-arg TARGETARCH=$(GOARCH) \
	  --build-arg BUILDPLATFORM=linux/amd64 \
	  --build-arg BUILD_VERSION=$(BUILD_VERSION) \
	  --build-arg BUILD_COMMIT=$(BUILD_COMMIT) \
	  --build-arg BUILD_DATE=$(BUILD_DATE) \
	  $(if $(filter $(OCI_BIN),podman),--volume "$(LOCAL_GOCACHE_PATH):$(CONTAINER_GOCACHE_PATH):z") \
	  -f Containerfile.bpfman-operator .

.PHONY: build-agent-image
build-agent-image: ## Build bpfman-agent image.
	$(OCI_BIN) buildx build --load -t ${BPFMAN_AGENT_IMG} \
	  --build-arg TARGETPLATFORM=linux/$(GOARCH) \
	  --build-arg TARGETARCH=$(GOARCH) \
	  --build-arg BUILDPLATFORM=linux/amd64 \
	  --build-arg BUILD_VERSION=$(BUILD_VERSION) \
	  --build-arg BUILD_COMMIT=$(BUILD_COMMIT) \
	  --build-arg BUILD_DATE=$(BUILD_DATE) \
	  $(if $(filter $(OCI_BIN),podman),--volume "$(LOCAL_GOCACHE_PATH):$(CONTAINER_GOCACHE_PATH):z") \
	  -f Containerfile.bpfman-agent .

.PHONY: push-images
push-images: ## Push bpfman-agent and bpfman-operator images.
	$(OCI_BIN) push ${BPFMAN_OPERATOR_IMG}
	$(OCI_BIN) push ${BPFMAN_AGENT_IMG}

.PHONY: load-images-kind
load-images-kind: $(KIND) ## Load bpfman-agent, and bpfman-operator images into the running local kind devel cluster.
	KIND=$(KIND) ./hack/kind-load-image.sh ${KIND_CLUSTER_NAME} ${BPFMAN_OPERATOR_IMG} ${BPFMAN_AGENT_IMG}

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(OCI_BIN) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(OCI_BIN) push $(BUNDLE_IMG)

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif


# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: $(OPM) ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)
# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	docker push $(CATALOG_IMG)

##@ CRD Deployment

ignore-not-found ?= true

.PHONY: install
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Image Patching

.PHONY: patch-image-references
patch-image-references: ## Update all image references with environment variables
	cd config/bpfman-operator-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-operator=${BPFMAN_OPERATOR_IMG}
# Patch the env var values in the deployment manifest by matching
# on the env var name rather than the current value, so the
# substitution is idempotent across repeated runs:
#   1. Match the line containing the env var name (e.g., "name: BPFMAN_IMG")
#   2. Advance to the next line (the "value:" line)
#   3. Replace everything after "value:" with the new image reference
# This means consecutive runs with different image values work
# without needing to restore deployment.yaml from git first.
#
# Also reset any imagePullPolicy injection from a previous "make
# deploy" so we always start from a clean baseline before deciding
# whether BPFMAN_IMAGE_PULL_POLICY needs to be honoured below.
	$(SED) -i -e '/name: BPFMAN_IMG/{n;s|^\([[:space:]]*value:[[:space:]]*\).*|\1$(BPFMAN_IMG)|;}' \
	       -e '/name: BPFMAN_AGENT_IMG/{n;s|^\([[:space:]]*value:[[:space:]]*\).*|\1$(BPFMAN_AGENT_IMG)|;}' \
	       -e '/imagePullPolicy/d' \
	       config/bpfman-operator-deployment/deployment.yaml
# When BPFMAN_IMAGE_PULL_POLICY is set, propagate it as the env var
# value the operator reads when stamping out the daemon and agent,
# and inject imagePullPolicy on the operator container so kubelet
# honours it when pulling the operator image itself. When unset,
# leave the env value empty and the imagePullPolicy off the manifest
# entirely. This is what lets bundle-deploy bake IfNotPresent into
# the bundle CSV without affecting non-kind paths.
	@if [ -n "$(BPFMAN_IMAGE_PULL_POLICY)" ]; then \
	    $(SED) -i -e '/name: BPFMAN_IMAGE_PULL_POLICY/{n;s|^\([[:space:]]*value:[[:space:]]*\).*|\1$(BPFMAN_IMAGE_PULL_POLICY)|;}' \
	           -e '/^[[:space:]]*image:[[:space:]].*$$/a\          imagePullPolicy: $(BPFMAN_IMAGE_PULL_POLICY)' \
	           config/bpfman-operator-deployment/deployment.yaml; \
	else \
	    $(SED) -i -e '/name: BPFMAN_IMAGE_PULL_POLICY/{n;s|^\([[:space:]]*value:[[:space:]]*\).*|\1""|;}' \
	           config/bpfman-operator-deployment/deployment.yaml; \
	fi

##@ Vanilla K8s Deployment

.PHONY: setup-kind
setup-kind: $(KIND) ## Setup Kind cluster
	$(KIND) delete cluster --name ${KIND_CLUSTER_NAME} && $(KIND) create cluster --config hack/kind-config.yaml --name ${KIND_CLUSTER_NAME}

.PHONY: setup-kind-registry
setup-kind-registry: $(KIND) ## Start a local registry for KIND and connect it to the KIND network.
	$(OCI_BIN) inspect $(KIND_REGISTRY_NAME) >/dev/null 2>&1 || \
	  $(OCI_BIN) run -d --restart=always -p "$(KIND_REGISTRY_PORT):5000" --name $(KIND_REGISTRY_NAME) registry:2
	$(OCI_BIN) network connect kind $(KIND_REGISTRY_NAME) 2>/dev/null || true
	for node in $$($(KIND) get nodes --name $(KIND_CLUSTER_NAME)); do \
	  $(OCI_BIN) exec $$node mkdir -p /etc/containerd/certs.d/localhost:$(KIND_REGISTRY_PORT); \
	  $(OCI_BIN) exec $$node sh -c "echo '[host.\"http://$(KIND_REGISTRY_NAME):5000\"]' > /etc/containerd/certs.d/localhost:$(KIND_REGISTRY_PORT)/hosts.toml"; \
	done

.PHONY: destroy-kind
destroy-kind: $(KIND) ## Destroy Kind cluster
	$(KIND) delete cluster --name ${KIND_CLUSTER_NAME}
	$(OCI_BIN) rm -f $(KIND_REGISTRY_NAME) 2>/dev/null || true

## Default deploy target is KIND based with its CSI driver initialized.
.PHONY: deploy
deploy: install patch-image-references ## Deploy bpfman-operator to the K8s cluster specified in ~/.kube/config with the csi driver initialized.
	$(SED) -i -e '/name: BPFMAN_IMAGE_PULL_POLICY/{n;s|^\([[:space:]]*value:[[:space:]]*\).*|\1IfNotPresent|;}' \
	       config/bpfman-operator-deployment/deployment.yaml
	@if ! grep -q 'imagePullPolicy' config/bpfman-operator-deployment/deployment.yaml; then \
		$(SED) -i '/^\([[:space:]]*\)image:.*/a\          imagePullPolicy: IfNotPresent' \
		       config/bpfman-operator-deployment/deployment.yaml; \
	fi
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy bpfman-operator from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@if kubectl get crd configs.bpfman.io >/dev/null 2>&1; then \
		kubectl delete --ignore-not-found=$(ignore-not-found) configs.bpfman.io bpfman-config; \
		kubectl wait --for=delete configs.bpfman.io/bpfman-config --timeout=60s; \
	fi
	-$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: kind-reload-images
kind-reload-images: load-images-kind ## Reload locally build images into a kind cluster and restart the ds and deployment so they're picked up.
	kubectl rollout restart deployment bpfman-operator -n bpfman
	kubectl rollout restart daemonset bpfman-daemon -n bpfman
	@if kubectl get daemonset -n bpfman bpfman-metrics-proxy >/dev/null 2>&1; then \
		kubectl rollout restart daemonset bpfman-metrics-proxy -n bpfman; \
	fi

.PHONY: run-on-kind
run-on-kind: setup-kind build-images load-images-kind install deploy ## Kind Deploy runs the bpfman-operator on a local kind cluster using local builds of bpfman, bpfman-agent, and bpfman-operator

##@ OLM Bundle Deployment

.PHONY: bundle-deploy
bundle-deploy: BPFMAN_IMAGE_PULL_POLICY := IfNotPresent
bundle-deploy: $(OPERATOR_SDK) build-images bundle bundle-build load-images-kind setup-kind-registry ## Deploy bpfman-operator via OLM bundle on the current cluster.
	$(OPERATOR_SDK) olm install 2>/dev/null || true
	kubectl delete catalogsource operatorhubio-catalog -n olm 2>/dev/null || true
	kubectl create namespace bpfman 2>/dev/null || true
	$(OCI_BIN) tag $(BUNDLE_IMG) $(KIND_BUNDLE_IMG)
	$(OCI_BIN) push $(KIND_BUNDLE_IMG)
	$(OPERATOR_SDK) run bundle $(KIND_BUNDLE_IMG) $(KIND_BUNDLE_PULL_FLAGS) -n bpfman --timeout 5m

.PHONY: bundle-run-on-kind
bundle-run-on-kind: setup-kind bundle-deploy ## Create a KIND cluster and deploy bpfman-operator via OLM bundle.

.PHONY: bundle-deploy-openshift
bundle-deploy-openshift: $(OPERATOR_SDK) build-images push-images bundle bundle-build bundle-push ## Deploy bpfman-operator via OLM bundle on OpenShift.
	kubectl create namespace bpfman 2>/dev/null || true
	$(OPERATOR_SDK) run bundle $(BUNDLE_IMG) -n bpfman --timeout 5m

.PHONY: bundle-undeploy
bundle-undeploy: $(OPERATOR_SDK) ## Remove the OLM bundle deployment.
	$(OPERATOR_SDK) cleanup bpfman-operator -n bpfman

##@ Openshift Deployment

.PHONY: deploy-openshift
deploy-openshift: install patch-image-references ## Deploy bpfman-operator to the Openshift cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: patch-image-pull-policy
patch-image-pull-policy: ## Patch imagePullPolicy on all bpfman resources. Set IMAGE_PULL_POLICY=Always|IfNotPresent|Never.
ifndef IMAGE_PULL_POLICY
	$(error IMAGE_PULL_POLICY is not set. Usage: make patch-image-pull-policy IMAGE_PULL_POLICY=IfNotPresent)
endif
	kubectl set env deployment/bpfman-operator -n bpfman BPFMAN_IMAGE_PULL_POLICY=$(IMAGE_PULL_POLICY)
	kubectl patch deployment -n bpfman bpfman-operator -p '{"spec":{"template":{"spec":{"containers":[{"name":"bpfman-operator","imagePullPolicy":"$(IMAGE_PULL_POLICY)"}]}}}}'
	@echo "Operator deployment patched to $(IMAGE_PULL_POLICY)."

.PHONY: patch-pull-always
patch-pull-always: ## Patch all bpfman deployments and daemonsets to use imagePullPolicy=Always.
	$(MAKE) patch-image-pull-policy IMAGE_PULL_POLICY=Always

.PHONY: undeploy-openshift
undeploy-openshift: ## Undeploy bpfman-operator from the Openshift cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@if kubectl get crd configs.bpfman.io >/dev/null 2>&1; then \
		kubectl delete --ignore-not-found=$(ignore-not-found) configs.bpfman.io bpfman-config; \
		kubectl wait --for=delete configs.bpfman.io/bpfman-config --timeout=60s; \
	fi
	-$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

# Deploy the catalog.
.PHONY: catalog-deploy
catalog-deploy: ## Deploy a catalog image.
	$(SED) -e 's~<IMAGE>~$(CATALOG_IMG)~' ./config/catalog/catalog.yaml | kubectl apply -f -

# Undeploy the catalog.
.PHONY: catalog-undeploy
catalog-undeploy: ## Undeploy a catalog image.
	kubectl delete --ignore-not-found=$(ignore-not-found) -f ./config/catalog/catalog.yaml
