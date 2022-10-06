GOLANGCILINT_VERSION ?= 1.50.0
GO_REQUIRED_VERSION ?= 1.19
# ====================================================================================
# Setup Project
PROJECT_NAME := provider-terraform
PROJECT_REPO := github.com/upbound/$(PROJECT_NAME)

PLATFORMS ?= linux_amd64 linux_arm64
-include build/makelib/common.mk

# Setup Output
-include build/makelib/output.mk

# Setup Go
NPROCS ?= 1
GO_TEST_PARALLEL := $(shell echo $$(( $(NPROCS) / 2 )))
GO_STATIC_PACKAGES = $(GO_PROJECT)/cmd/provider
GO_LDFLAGS += -X $(GO_PROJECT)/pkg/version.Version=$(VERSION)
GO_SUBDIRS += cmd internal apis
GO111MODULE = on
-include build/makelib/golang.mk

# Setup Kubernetes tools

UP_VERSION = v0.13.0
UP_CHANNEL = stable
-include build/makelib/k8s_tools.mk

# Setup Images
IMAGES = provider-terraform
-include build/makelib/imagelight.mk

# ====================================================================================
# Setup XPKG

XPKG_REGISTRY ?= xpkg.upbound.io
XPKG_ORG ?= upbound
XPKG_REPO ?= $(PROJECT_NAME)
XPKG_REG_ORGS ?= xpkg.upbound.io/crossplane-contrib index.docker.io/crossplanecontrib
# NOTE(hasheddan): skip promoting on xpkg.upbound.io as channel tags are
# inferred.
XPKG_REG_ORGS_NO_PROMOTE ?= xpkg.upbound.io/crossplane-contrib
XPKGS = provider-terraform
-include build/makelib/xpkg.mk

# NOTE(hasheddan): we force image building to happen prior to xpkg build so that
# we ensure image is present in daemon.
xpkg.build.provider-terraform: do.build.images

# ====================================================================================
# Targets

# run `make help` to see the targets and options

# We want submodules to be set up the first time `make` is run.
# We manage the build/ folder and its Makefiles as a submodule.
# The first time `make` is run, the includes of build/*.mk files will
# all fail, and this target will be run. The next time, the default as defined
# by the includes will be run instead.
fallthrough: submodules
	@echo Initial setup complete. Running make again . . .
	@make

# integration tests
e2e.run: test-integration

# Run integration tests.
test-integration: $(KIND) $(KUBECTL) $(HELM3)
	@$(INFO) running integration tests using kind $(KIND_VERSION)
	@$(ROOT_DIR)/cluster/local/integration_tests.sh || $(FAIL)
	@$(OK) integration tests passed

# Update the submodules, such as the common build scripts.
submodules:
	@git submodule sync
	@git submodule update --init --recursive

# NOTE(hasheddan): we must ensure up is installed in tool cache prior to build
# as including the k8s_tools machinery prior to the xpkg machinery sets UP to
# point to tool cache.
build.init: $(UP)

# This is for running out-of-cluster locally, and is for convenience. Running
# this make target will print out the command which was used. For more control,
# try running the binary directly with different arguments.
run: go.build
	@$(INFO) Running Crossplane locally out-of-cluster . . .
	@# To see other arguments that can be provided, run the command with --help instead
	@# KUBE_CONFIG_PATH explained at  https://developer.hashicorp.com/terraform/language/settings/backends/kubernetes
	@# XP_TF_DIR is to override default tf work dir which is usually /tf and unreadable locally
	KUBE_CONFIG_PATH=~/.kube/config XP_TF_DIR=./tf $(GO_OUT_DIR)/provider --debug

dev: $(KIND) $(KUBECTL)
	@$(INFO) Creating kind cluster
	@$(KIND) create cluster --name=$(PROJECT_NAME)-dev
	@$(KUBECTL) cluster-info --context kind-$(PROJECT_NAME)-dev
	@$(INFO) Installing Crossplane CRDs
	@$(KUBECTL) apply -k https://github.com/crossplane/crossplane//cluster?ref=master
	@$(INFO) Installing Provider SQL CRDs
	@$(KUBECTL) apply -R -f package/crds
	@$(INFO) Starting Provider SQL controllers
	@$(GO) run cmd/provider/main.go --debug

dev-clean: $(KIND) $(KUBECTL)
	@$(INFO) Deleting kind cluster
	@$(KIND) delete cluster --name=$(PROJECT_NAME)-dev

.PHONY: reviewable submodules fallthrough test-integration run dev dev-clean

# ====================================================================================
# Special Targets

define CROSSPLANE_MAKE_HELP
Crossplane Targets:
    submodules            Update the submodules, such as the common build scripts.
    run                   Run crossplane locally, out-of-cluster. Useful for development.

endef
# The reason CROSSPLANE_MAKE_HELP is used instead of CROSSPLANE_HELP is because the crossplane
# binary will try to use CROSSPLANE_HELP if it is set, and this is for something different.
export CROSSPLANE_MAKE_HELP

crossplane.help:
	@echo "$$CROSSPLANE_MAKE_HELP"

help-special: crossplane.help

.PHONY: crossplane.help help-special

go.cachedir:
	@go env GOCACHE

.PHONY: go.cachedir

go.mod.cachedir:
	@go env GOMODCACHE

.PHONY: go.mod.cachedir

xpkg.build: $(UP) do.build.images
	@$(INFO) Building package $(PROJECT_NAME)-$(VERSION).xpkg for $(PLATFORM)
	@mkdir -p $(OUTPUT_DIR)/xpkg/$(PLATFORM)
	@$(UP) xpkg build  --controller $(BUILD_REGISTRY)/$(PROJECT_NAME)-$(ARCH)  --package-root ./package  --examples-root ./examples  --output ./_output/xpkg/$(PLATFORM)/$(PROJECT_NAME)-$(VERSION).xpkg || $(FAIL)
	@$(OK) Built package $(PROJECT_NAME)-$(VERSION).xpkg for $(PLATFORM)

build.artifacts.platform: xpkg.build


xpkg.push: $(UP)
	@$(INFO) Pushing package $(PROJECT_NAME)-$(VERSION).xpkg
	@$(UP) xpkg push  --package $(OUTPUT_DIR)/xpkg/linux_amd64/$(PROJECT_NAME)-$(VERSION).xpkg  --package $(OUTPUT_DIR)/xpkg/linux_arm64/$(PROJECT_NAME)-$(VERSION).xpkg  $(XPKG_REGISTRY)/$(XPKG_ORG)/$(XPKG_REPO):$(VERSION) || $(FAIL)
	@$(OK) Pushed package $(PROJECT_NAME)-$(VERSION).xpkg


xpkg.load: $(UP)
	@$(INFO) Loading package $(PROJECT_NAME)-$(VERSION).xpkg for $(PLATFORM) into Docker daemon
	@docker load -i $(OUTPUT_DIR)/xpkg/$(PLATFORM)/$(PROJECT_NAME)-$(VERSION).xpkg
	@$(OK) Loaded package $(PROJECT_NAME)-$(VERSION).xpkg for $(PLATFORM) into Docker daemon

