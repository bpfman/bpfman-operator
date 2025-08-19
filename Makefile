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
# Image URL to use all building/pushing image targets
BPFMAN_IMG ?= quay.io/bpfman/bpfman:$(IMAGE_TAG)
BPFMAN_AGENT_IMG ?= quay.io/bpfman/bpfman-agent:$(IMAGE_TAG)
BPFMAN_OPERATOR_IMG ?= quay.io/bpfman/bpfman-operator:$(IMAGE_TAG)
KIND_CLUSTER_NAME ?= bpfman-deployment

# These environment variable keys need to be exported as the
# integration tests expect them to be defined.
export BPFMAN_AGENT_IMG
export BPFMAN_OPERATOR_IMG

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.25.0
K8S_CODEGEN_VERSION = v0.30.1

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
	@echo "BUNDLE_IMG: $(BUNDLE_IMG)"
	@echo "CATALOG_IMG: $(CATALOG_IMG)"
	@echo "IMAGE_TAG: $(IMAGE_TAG)"
	@echo "VERSION: $(VERSION)"

##@ Local Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Allows building bundles in Mac replacing BSD 'sed' command by GNU-compatible 'gsed'
ifeq (,$(shell which gsed 2>/dev/null))
SED ?= sed
else
SED ?= gsed
endif

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
REGISTER_GEN ?= $(LOCALBIN)/register-gen
INFORMER_GEN ?= $(LOCALBIN)/informer-gen
LISTER_GEN ?= $(LOCALBIN)/lister-gen
CLIENT_GEN ?= $(LOCALBIN)/client-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
CM_VERIFIER ?= $(LOCALBIN)/cm-verifier
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.15.0
OPERATOR_SDK_VERSION ?= v1.27.0
GOLANGCI_LINT_VERSION = v2.0.2

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

OPERATOR_SDK_DL_NAME=operator-sdk_$(shell go env GOOS)_$(shell go env GOARCH)
OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/$(OPERATOR_SDK_DL_NAME)
.PHONY: operator-sdk
operator-sdk: $(OPERATOR_SDK)
$(OPERATOR_SDK): $(LOCALBIN)
	test -s $(LOCALBIN)/operator_sdk || { curl -LO ${OPERATOR_SDK_DL_URL} && chmod +x ${OPERATOR_SDK_DL_NAME} &&\
	 mv ${OPERATOR_SDK_DL_NAME} $(LOCALBIN)/operator-sdk; }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: register-gen
register-gen: $(REGISTER_GEN) ## Download register-gen locally if necessary.
$(REGISTER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/register-gen || GOBIN=$(LOCALBIN) go install k8s.io/code-generator/cmd/register-gen@$(K8S_CODEGEN_VERSION)

.PHONY: informer-gen
informer-gen: $(INFORMER_GEN) ## Download informer-gen locally if necessary.
$(INFORMER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/informer-gen || GOBIN=$(LOCALBIN) go install k8s.io/code-generator/cmd/informer-gen@$(K8S_CODEGEN_VERSION)

.PHONY: lister-gen
lister-gen: $(LISTER_GEN) ## Download lister-gen locally if necessary.
$(LISTER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/lister-gen || GOBIN=$(LOCALBIN) go install k8s.io/code-generator/cmd/lister-gen@$(K8S_CODEGEN_VERSION)

.PHONY: client-gen
client-gen: $(CLIENT_GEN) ## Download client-gen locally if necessary.
$(CLIENT_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/client-gen || GOBIN=$(LOCALBIN) go install k8s.io/code-generator/cmd/client-gen@$(K8S_CODEGEN_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.45.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

##@ Development

VERIFY_CODEGEN ?= false
ifeq ($(VERIFY_CODEGEN), true)
VERIFY_FLAG=--verify-only
endif

PKG ?= github.com/bpfman/bpfman-operator
COMMON_FLAGS ?= ${VERIFY_FLAG} --go-header-file $(shell pwd)/hack/boilerplate.go.txt

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=agent-role paths="./controllers/bpfman-agent/..." output:rbac:artifacts:config=config/rbac/bpfman-agent
	$(CONTROLLER_GEN) rbac:roleName=operator-role paths="./controllers/bpfman-operator" output:rbac:artifacts:config=config/rbac/bpfman-operator

.PHONY: generate
generate: manifests generate-register generate-deepcopy generate-typed-clients generate-typed-listers generate-typed-informers ## Generate ALL auto-generated code.

.PHONY: generate-register
generate-register: register-gen ## Generate register code see all `zz_generated.register.go` files.
	$(REGISTER_GEN) \
		"${PKG}/apis/v1alpha1" \
		--output-file zz_generated.register.go \
		--go-header-file ./hack/boilerplate.go.txt \
		${COMMON_FLAGS}

.PHONY: generate-deepcopy
generate-deepcopy: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations see all `zz_generated.register.go` files.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-typed-clients
generate-typed-clients: client-gen ## Generate typed client code
	$(CLIENT_GEN) \
		--clientset-name "clientset" \
		--input-base "" \
		--input "${PKG}/apis/v1alpha1" \
		--output-pkg "${PKG}/pkg/client" \
		--output-dir "./pkg/client" \
		${COMMON_FLAGS}


.PHONY: generate-typed-listers
generate-typed-listers: lister-gen ## Generate typed listers code
	$(LISTER_GEN) \
		"${PKG}/apis/v1alpha1" \
		--output-pkg "${PKG}/pkg/client" \
		--output-dir "./pkg/client" \
		${COMMON_FLAGS}


.PHONY: generate-typed-informers
generate-typed-informers: informer-gen ## Generate typed informers code
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
test: fmt envtest ## Run Unit tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out


.PHONY: test-integration
test-integration: ## Run Integration tests.
	cd config/bpfman-deployment && \
	  $(SED) -e 's@bpfman\.image=.*@bpfman.image=$(BPFMAN_IMG)@' \
	      -e 's@bpfman\.agent\.image=.*@bpfman.agent.image=$(BPFMAN_AGENT_IMG)@' \
		  kustomization.yaml.env > kustomization.yaml
	GOFLAGS="-tags=integration_tests" go test -count=1 -race -v ./test/integration/...

.PHONY: test-integration-local
test-integration-local: ## Run Integration tests against existing deployment. Use TEST= to specify test pattern.
	USE_EXISTING_KIND_CLUSTER=$(shell kubectl config current-context | sed 's/kind-//') \
	SKIP_BPFMAN_DEPLOY=true \
	GOFLAGS="-tags=integration_tests" go test -count=1 -race -v ./test/integration $(if $(TEST),-run $(TEST),)

## The physical bundle is no longer tracked in git since it should be considered
## and treated as a release artifact, rather than something that's updated
## as part of a pull request.
## See https://github.com/operator-framework/operator-sdk/issues/6285.
.PHONY: bundle
bundle: operator-sdk generate kustomize manifests ## Generate bundle manifests and metadata, then validate generated files.
	cd config/bpfman-operator-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-operator=${BPFMAN_OPERATOR_IMG}
	cd config/bpfman-deployment && \
	  $(SED) -e 's@bpfman\.image=.*@bpfman.image=$(BPFMAN_IMG)@' \
	      -e 's@bpfman\.agent\.image=.*@bpfman.agent.image=$(BPFMAN_AGENT_IMG)@' \
		  kustomization.yaml.env > kustomization.yaml
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	# Dependency on security-profiles-operator removed (file renamed to dependencies.yaml.disabled)
	# https://github.com/kubernetes-sigs/security-profiles-operator/issues/2699
	# cp config/manifests/dependencies.yaml bundle/metadata/
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: build-release-yamls
build-release-yamls: generate kustomize ## Generate the crd install bundle for a specific release version.
	VERSION=$(VERSION) ./hack/build-release-yamls.sh

##@ Build

.PHONY: build
build: fmt ## Build bpfman-operator, bpfman-agent, and bpfman-crictl binaries.
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -o bin/bpfman-operator cmd/bpfman-operator/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -o bin/bpfman-agent cmd/bpfman-agent/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -o bin/metrics-proxy cmd/metrics-proxy/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -mod vendor -o bin/bpfman-crictl cmd/bpfman-crictl/main.go

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
	  $(if $(filter $(OCI_BIN),podman),--volume "$(LOCAL_GOCACHE_PATH):$(CONTAINER_GOCACHE_PATH):z") \
	  -f Containerfile.bpfman-operator .

.PHONY: build-agent-image
build-agent-image: ## Build bpfman-agent image.
	$(OCI_BIN) buildx build --load -t ${BPFMAN_AGENT_IMG} \
	  --build-arg TARGETPLATFORM=linux/$(GOARCH) \
	  --build-arg TARGETARCH=$(GOARCH) \
	  --build-arg BUILDPLATFORM=linux/amd64 \
	  $(if $(filter $(OCI_BIN),podman),--volume "$(LOCAL_GOCACHE_PATH):$(CONTAINER_GOCACHE_PATH):z") \
	  -f Containerfile.bpfman-agent .

.PHONY: push-images
push-images: ## Push bpfman-agent and bpfman-operator images.
	$(OCI_BIN) push ${BPFMAN_OPERATOR_IMG}
	$(OCI_BIN) push ${BPFMAN_AGENT_IMG}

.PHONY: load-images-kind
load-images-kind: ## Load bpfman-agent, and bpfman-operator images into the running local kind devel cluster.
	./hack/kind-load-image.sh ${KIND_CLUSTER_NAME} ${BPFMAN_OPERATOR_IMG} ${BPFMAN_AGENT_IMG}

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	docker push $(BUNDLE_IMG)

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
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)
# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	docker push $(CATALOG_IMG)

##@ CRD Deployment

ignore-not-found ?= true

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Vanilla K8s Deployment

.PHONY: setup-kind
setup-kind: ## Setup Kind cluster
	kind delete cluster --name ${KIND_CLUSTER_NAME} && kind create cluster --config hack/kind-config.yaml --name ${KIND_CLUSTER_NAME}

.PHONY: destroy-kind
destroy-kind: ## Destroy Kind cluster
	kind delete cluster --name ${KIND_CLUSTER_NAME}

## Default deploy target is KIND based with its CSI driver initialized.
.PHONY: deploy
deploy: install ## Deploy bpfman-operator to the K8s cluster specified in ~/.kube/config with the csi driver initialized.
	cd config/bpfman-operator-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-operator=${BPFMAN_OPERATOR_IMG}
	cd config/bpfman-deployment && \
	 $(SED)  -e 's@bpfman\.image=.*@bpfman.image=$(BPFMAN_IMG)@' \
	      -e 's@bpfman\.agent\.image=.*@bpfman.agent.image=$(BPFMAN_AGENT_IMG)@' \
		  kustomization.yaml.env > kustomization.yaml
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy bpfman-operator from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	kubectl delete --ignore-not-found=$(ignore-not-found) configs.bpfman.io bpfman-config
	kubectl wait --for=delete configs.bpfman.io/bpfman-config --timeout=60s
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: kind-reload-images
kind-reload-images: load-images-kind ## Reload locally build images into a kind cluster and restart the ds and deployment so they're picked up.
	kubectl rollout restart daemonset bpfman-daemon -n bpfman
	kubectl rollout restart deployment bpfman-operator -n bpfman

.PHONY: run-on-kind
run-on-kind: kustomize setup-kind build-images load-images-kind install deploy ## Kind Deploy runs the bpfman-operator on a local kind cluster using local builds of bpfman, bpfman-agent, and bpfman-operator

##@ Openshift Deployment

.PHONY: deploy-openshift
deploy-openshift: install ## Deploy bpfman-operator to the Openshift cluster specified in ~/.kube/config.
	cd config/bpfman-operator-deployment && $(KUSTOMIZE) edit set image quay.io/bpfman/bpfman-operator=${BPFMAN_OPERATOR_IMG}
	cd config/bpfman-deployment && \
	  $(SED) -e 's@bpfman\.image=.*@bpfman.image=$(BPFMAN_IMG)@' \
	      -e 's@bpfman\.agent\.image=.*@bpfman.agent.image=$(BPFMAN_AGENT_IMG)@' \
		  kustomization.yaml.env > kustomization.yaml
	$(KUSTOMIZE) build config/openshift | kubectl apply -f -

.PHONY: undeploy-openshift
undeploy-openshift: ## Undeploy bpfman-operator from the Openshift cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	kubectl delete --ignore-not-found=$(ignore-not-found) configs.bpfman.io bpfman-config
	kubectl wait --for=delete configs.bpfman.io/bpfman-config --timeout=60s
	$(KUSTOMIZE) build config/openshift | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

# Deploy the catalog.
.PHONY: catalog-deploy
catalog-deploy: ## Deploy a catalog image.
	$(SED) -e 's~<IMAGE>~$(CATALOG_IMG)~' ./config/catalog/catalog.yaml | kubectl apply -f -

# Undeploy the catalog.
.PHONY: catalog-undeploy
catalog-undeploy: ## Undeploy a catalog image.
	kubectl delete --ignore-not-found=$(ignore-not-found) -f ./config/catalog/catalog.yaml
