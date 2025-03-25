# ====================================================================================
# Setup Project
PROJECT_NAME := provider-terraform
PROJECT_REPO := github.com/upbound/$(PROJECT_NAME)

PLATFORMS ?= linux_amd64 linux_arm64
-include build/makelib/common.mk

# Setup Output
-include build/makelib/output.mk

# Setup Go
GO_REQUIRED_VERSION ?= 1.23
NPROCS ?= 1
# GOLANGCILINT_VERSION is inherited from build submodule by default.
# Uncomment below if you need to override the version.
# GOLANGCILINT_VERSION ?= 1.50.0
GO_TEST_PARALLEL := $(shell echo $$(( $(NPROCS) / 2 )))
GO_STATIC_PACKAGES = $(GO_PROJECT)/cmd/provider
GO_LDFLAGS += -X $(GO_PROJECT)/pkg/version.Version=$(VERSION)
GO_SUBDIRS += cmd internal apis
GO111MODULE = on

-include build/makelib/golang.mk

# ====================================================================================
# Setup Kubernetes tools

# Uncomment below to override the versions from the build module
#KIND_VERSION = v0.27.0
UP_VERSION = v0.34.2
UP_CHANNEL = stable
UPTEST_VERSION = v1.1.2
UPTEST_LOCAL_VERSION = v0.13.0
UPTEST_LOCAL_CHANNEL = stable
KUSTOMIZE_VERSION = v5.3.0
YQ_VERSION = v4.40.5
CROSSPLANE_VERSION = 1.17.1
CRDDIFF_VERSION = v0.12.1

export UP_VERSION := $(UP_VERSION)
export UP_CHANNEL := $(UP_CHANNEL)

-include build/makelib/k8s_tools.mk

# uptest download and install
UPTEST_LOCAL := $(TOOLS_HOST_DIR)/uptest-$(UPTEST_LOCAL_VERSION)

$(UPTEST_LOCAL):
	@$(INFO) installing uptest $(UPTEST_LOCAL)
	@mkdir -p $(TOOLS_HOST_DIR)
	@curl -fsSLo $(UPTEST_LOCAL) https://s3.us-west-2.amazonaws.com/crossplane.uptest.releases/$(UPTEST_LOCAL_CHANNEL)/$(UPTEST_LOCAL_VERSION)/bin/$(SAFEHOST_PLATFORM)/uptest || $(FAIL)
	@chmod +x $(UPTEST_LOCAL)
	@$(OK) installing uptest $(UPTEST_LOCAL)

# Setup Images
REGISTRY_ORGS ?= xpkg.upbound.io/upbound
IMAGES = provider-terraform
BATCH_PLATFORMS ?= linux_amd64,linux_arm64
export BATCH_PLATFORMS := $(BATCH_PLATFORMS)

-include build/makelib/imagelight.mk

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

# Update the submodules, such as the common build scripts.
submodules:
	@git submodule sync
	@git submodule update --init --recursive

# ====================================================================================
# Setup XPKG

XPKG_REG_ORGS ?= xpkg.upbound.io/upbound
# NOTE(hasheddan): skip promoting on xpkg.upbound.io as channel tags are
# inferred.
XPKG_REG_ORGS_NO_PROMOTE ?= xpkg.upbound.io/upbound
XPKGS = provider-terraform
-include build/makelib/xpkg.mk

# NOTE(hasheddan): we force image building to happen prior to xpkg build so that
# we ensure image is present in daemon.
xpkg.build.provider-terraform: do.build.images

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
	@$(KUBECTL) apply -k https://github.com/crossplane/crossplane/cluster?ref=master
	@$(INFO) Installing Provider CRDs
	@$(KUBECTL) apply -R -f package/crds
	@$(INFO) Starting Provider controllers
	@$(GO) run cmd/provider/main.go --debug

dev-clean: $(KIND) $(KUBECTL)
	@$(INFO) Deleting kind cluster
	@$(KIND) delete cluster --name=$(PROJECT_NAME)-dev

.PHONY: reviewable submodules fallthrough test-integration run dev dev-clean

# ====================================================================================
# End to End Testing
CROSSPLANE_NAMESPACE = upbound-system
-include build/makelib/local.xpkg.mk
-include build/makelib/controlplane.mk

# This target requires the following environment variables to be set:
# - UPTEST_EXAMPLE_LIST, a comma-separated list of examples to test
# - UPTEST_CLOUD_CREDENTIALS (optional), cloud credentials for the provider being tested, e.g. export UPTEST_CLOUD_CREDENTIALS=$(cat ~/.aws/credentials)
# - UPTEST_DATASOURCE_PATH (optional), see https://github.com/upbound/uptest#injecting-dynamic-values-and-datasource
uptest: $(UPTEST) $(KUBECTL) $(CHAINSAW) $(CROSSPLANE_CLI)
	@$(INFO) running automated tests
	@KUBECTL=$(KUBECTL) CHAINSAW=$(CHAINSAW) CROSSPLANE_CLI=$(CROSSPLANE_CLI) CROSSPLANE_NAMESPACE=$(CROSSPLANE_NAMESPACE) $(UPTEST) e2e "${UPTEST_EXAMPLE_LIST}" --data-source="${UPTEST_DATASOURCE_PATH}" --setup-script=cluster/test/setup.sh --skip-import || $(FAIL)
	@$(OK) running automated tests

local-deploy: build controlplane.up local.xpkg.deploy.provider.$(PROJECT_NAME)
	@$(INFO) running locally built provider
	@$(KUBECTL) wait provider.pkg $(PROJECT_NAME) --for condition=Healthy --timeout 5m
	@$(KUBECTL) -n upbound-system wait --for=condition=Available deployment --all --timeout=5m
	@$(OK) running locally built provider

# This target requires the following environment variables to be set:
# - UPTEST_CLOUD_CREDENTIALS, cloud credentials for the provider being tested, e.g. export UPTEST_CLOUD_CREDENTIALS=$(cat ~/.aws/credentials)
# - UPTEST_EXAMPLE_LIST, a comma-separated list of examples to test
# - UPTEST_DATASOURCE_PATH, see https://github.com/upbound/uptest#injecting-dynamic-values-and-datasource
e2e: local-deploy uptest

# TODO: please move this to the common build submodule
# once the use cases mature
crddiff: $(UPTEST)
	@$(INFO) Checking breaking CRD schema changes
	@for crd in $${MODIFIED_CRD_LIST}; do \
		if ! git cat-file -e "$${GITHUB_BASE_REF}:$${crd}" 2>/dev/null; then \
			echo "CRD $${crd} does not exist in the $${GITHUB_BASE_REF} branch. Skipping..." ; \
			continue ; \
		fi ; \
		echo "Checking $${crd} for breaking API changes..." ; \
		changes_detected=$$($(UPTEST) crddiff revision <(git cat-file -p "$${GITHUB_BASE_REF}:$${crd}") "$${crd}" 2>&1) ; \
		if [[ $$? != 0 ]] ; then \
			printf "\033[31m"; echo "Breaking change detected!"; printf "\033[0m" ; \
			echo "$${changes_detected}" ; \
			echo ; \
		fi ; \
	done
	@$(OK) Checking breaking CRD schema changes

go.lint.analysiskey-interval:
	@# cache is invalidated at least every 7 days
	@echo -n golangci-lint.cache-$$(( $$(date +%s) / (7 * 86400) ))-

go.lint.analysiskey:
	@echo $$(make go.lint.analysiskey-interval)$$(sha1sum go.sum | cut -d' ' -f1)

.PHONY: uptest e2e
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

vendor: modules.download
vendor.check: modules.check
