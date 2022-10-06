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
-include build/makelib/k8s_tools.mk

# Setup Images
DOCKER_REGISTRY ?= crossplane
IMAGES = $(PROJECT_NAME) $(PROJECT_NAME)-controller
-include build/makelib/image.mk

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

# This is for running out-of-cluster locally, and is for convenience. Running
# this make target will print out the command which was used. For more control,
# try running the binary directly with different arguments.
run: go.build
	@$(INFO) Running Crossplane locally out-of-cluster . . .
	@# To see other arguments that can be provided, run the command with --help instead
	$(GO_OUT_DIR)/$(PROJECT_NAME) --debug

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

