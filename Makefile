# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You
# may not use this file except in compliance with the License. A copy of
# the License is located at
#
#       http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is
# distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF
# ANY KIND, either express or implied. See the License for the specific
# language governing permissions and limitations under the License.
#

.PHONY: all dist check clean \
		lint format check-format vet docker-vet \
		build-linux docker docker-init \
		unit-test unit-test-race build-docker-test docker-func-test \
		build-metrics docker-metrics \
		metrics-unit-test docker-metrics-test

# VERSION is the source revision that executables and images are built from.
VERSION ?= $(shell git describe --tags --always --dirty || echo "unknown")

# DESTDIR is where distribution output (container images) is placed.
DESTDIR = .

# IMAGE is the primary AWS VPC CNI plugin container image.
IMAGE = amazon/amazon-k8s-cni
IMAGE_NAME = $(IMAGE)$(IMAGE_ARCH_SUFFIX):$(VERSION)
IMAGE_DIST = $(DESTDIR)/$(subst /,_,$(IMAGE_NAME)).tar.gz
# INIT_IMAGE is the init container for AWS VPC CNI.
INIT_IMAGE = amazon/amazon-k8s-cni-init
INIT_IMAGE_NAME = $(INIT_IMAGE)$(IMAGE_ARCH_SUFFIX):$(VERSION)
INIT_IMAGE_DIST = $(DESTDIR)/$(subst /,_,$(INIT_IMAGE_NAME)).tar.gz
MAKEFILE_PATH = $(dir $(realpath -s $(firstword $(MAKEFILE_LIST))))
# METRICS_IMAGE is the CNI metrics publisher sidecar container image.
METRICS_IMAGE = amazon/cni-metrics-helper
METRICS_IMAGE_NAME = $(METRICS_IMAGE)$(IMAGE_ARCH_SUFFIX):$(VERSION)
METRICS_IMAGE_DIST = $(DESTDIR)/$(subst /,_,$(METRICS_IMAGE_NAME)).tar.gz
REPO_FULL_NAME=aws/amazon-vpc-cni-k8s
HELM_CHART_NAME ?= "aws-vpc-cni"
# TEST_IMAGE is the testing environment container image.
TEST_IMAGE = amazon-k8s-cni-test
TEST_IMAGE_NAME = $(TEST_IMAGE)$(IMAGE_ARCH_SUFFIX):$(VERSION)
# These values derive ARCH and DOCKER_ARCH which are needed by dependencies in
# image build defaulting to system's architecture when not specified.
#
# UNAME_ARCH is the runtime architecture of the building host.
UNAME_ARCH = $(shell uname -m)
# ARCH is the target architecture which is being built for.
#
# These are pairs of input_arch to derived_arch separated by colons:
ARCH = $(lastword $(subst :, ,$(filter $(UNAME_ARCH):%,x86_64:amd64 aarch64:arm64)))
# DOCKER_ARCH is the docker specific architecture specifier used for building on
# multiarch container images.
DOCKER_ARCH = $(lastword $(subst :, ,$(filter $(ARCH):%,amd64:amd64 arm64:arm64v8)))
# IMAGE_ARCH_SUFFIX is the `-arch` suffix included in the container image name.
#
# This is only applied to the arm64 container image by default. Override to
# provide an alternate suffix or to omit.
IMAGE_ARCH_SUFFIX = $(addprefix -,$(filter $(ARCH),arm64))
# GOLANG_IMAGE is the building golang container image used.
GOLANG_IMAGE = golang:1.14-stretch
# For the requested build, these are the set of Go specific build environment variables.
export GOARCH ?= $(ARCH)
export GOOS = linux
export CGO_ENABLED = 0
# NOTE: Provided for local toolchains that require explicit module feature flag.
export GO111MODULE = on
export GOPROXY = direct

VENDOR_OVERRIDE_FLAG =
# aws-sdk-go override in case we need to build against a custom version
EC2_SDK_OVERRIDE ?= "y"

ifeq ($(EC2_SDK_OVERRIDE), "y")
VENDOR_OVERRIDE_FLAG = -mod=mod
endif

# LDFLAGS is the set of flags used when building golang executables.
LDFLAGS = -X main.version=$(VERSION) -X pkg/awsutils/awssession.version=$(VERSION)
# ALLPKGS is the set of packages provided in source.
ALLPKGS = $(shell go list ./... | grep -v cmd/packet-verifier)
# BINS is the set of built command executables.
BINS = aws-k8s-agent aws-cni grpc-health-probe cni-metrics-helper
# Plugin binaries
# Not copied: bridge dhcp firewall flannel host-device host-local ipvlan macvlan ptp sbr static tuning vlan
# For gnu tar, the full path in the tar file is required
PLUGIN_BINS = ./loopback ./portmap ./bandwidth

# DOCKER_ARGS is extra arguments passed during container image build.
DOCKER_ARGS =
# DOCKER_RUN_FLAGS is set the flags passed during runs of containers.
DOCKER_RUN_FLAGS = --rm -ti $(DOCKER_ARGS)
# DOCKER_BUILD_FLAGS is the set of flags passed during container image builds
# based on the requested build.
DOCKER_BUILD_FLAGS = --build-arg GOARCH="$(ARCH)" \
					  --build-arg docker_arch="$(DOCKER_ARCH)" \
					  --build-arg golang_image="$(GOLANG_IMAGE)" \
					  --network=host \
	  		          $(DOCKER_ARGS)

# Default to building an executable using the host's Go toolchain.
.DEFAULT_GOAL = build-linux

# Build both CNI and metrics helper container images.
all: docker docker-init docker-metrics   ## Builds Init, CNI and metrics helper container images.

dist: all
	mkdir -p $(DESTDIR)
	docker save $(IMAGE_NAME) | gzip > $(IMAGE_DIST)
	docker save $(INIT_IMAGE_NAME) | gzip > $(INIT_IMAGE_DIST)
	docker save $(METRICS_IMAGE_NAME) | gzip > $(METRICS_IMAGE_DIST)

# Build the VPC CNI plugin agent using the host's Go toolchain.
BUILD_MODE ?= -buildmode=pie
build-linux: BUILD_FLAGS = $(BUILD_MODE) -ldflags '-s -w $(LDFLAGS)'
build-linux:    ## Build the VPC CNI plugin agent using the host's Go toolchain.
	go build $(VENDOR_OVERRIDE_FLAG) $(BUILD_FLAGS) -o aws-k8s-agent     ./cmd/aws-k8s-agent
	go build $(VENDOR_OVERRIDE_FLAG) $(BUILD_FLAGS) -o aws-cni           ./cmd/routed-eni-cni-plugin
	go build $(VENDOR_OVERRIDE_FLAG) $(BUILD_FLAGS) -o grpc-health-probe ./cmd/grpc-health-probe

# Build VPC CNI plugin & agent container image.
docker:	setup-ec2-sdk-override	   ## Build VPC CNI plugin & agent container image.
	docker build $(DOCKER_BUILD_FLAGS) \
		-f scripts/dockerfiles/Dockerfile.release \
		-t "$(IMAGE_NAME)" \
		.
	@echo "Built Docker image \"$(IMAGE_NAME)\""

docker-init:     ## Build VPC CNI plugin Init container image.
	docker build $(DOCKER_BUILD_FLAGS) \
		-f scripts/dockerfiles/Dockerfile.init \
		-t "$(INIT_IMAGE_NAME)" \
		.
	@echo "Built Docker image \"$(INIT_IMAGE_NAME)\""

# Run the built CNI container image to use in functional testing
docker-func-test: docker     ## Run the built CNI container image to use in functional testing
	docker run $(DOCKER_RUN_FLAGS) \
		"$(IMAGE_NAME)"

# Run unit tests
unit-test: export AWS_VPC_K8S_CNI_LOG_FILE=stdout
unit-test:    ## Run unit tests
	go test -v -coverprofile=coverage.txt -covermode=atomic $(ALLPKGS)

# Run unit tests with race detection (can only be run natively)
unit-test-race: export AWS_VPC_K8S_CNI_LOG_FILE=stdout
unit-test-race: CGO_ENABLED=1
unit-test-race: GOARCH=
unit-test-race:     ## Run unit tests with race detection (can only be run natively)
	go test -v -cover -race -timeout 10s  ./cmd/...
	go test -v -cover -race -timeout 150s ./pkg/awsutils/...
	go test -v -cover -race -timeout 10s  ./pkg/k8sapi/...
	go test -v -cover -race -timeout 10s  ./pkg/networkutils/...
	go test -v -cover -race -timeout 10s  ./pkg/utils/...
	go test -v -cover -race -timeout 10s  ./pkg/eniconfig/...
	go test -v -cover -race -timeout 10s  ./pkg/ipamd/...

# Build the unit test driver container image.
build-docker-test:     ## Build the unit test driver container image.
	docker build $(DOCKER_BUILD_FLAGS) \
		-f scripts/dockerfiles/Dockerfile.test \
		-t $(TEST_IMAGE_NAME) \
		.

# Run unit tests inside of the testing container image.
docker-unit-test: build-docker-test     ## Run unit tests inside of the testing container image.
	docker run $(DOCKER_RUN_ARGS) \
		$(TEST_IMAGE_NAME) \
		make unit-test

# Build metrics helper agent.
build-metrics:     ## Build metrics helper agent.
	go build -ldflags="-s -w" -o cni-metrics-helper ./cmd/cni-metrics-helper

# Build metrics helper agent Docker image.
docker-metrics:    ## Build metrics helper agent Docker image.
	docker build $(DOCKER_BUILD_FLAGS) \
		-f scripts/dockerfiles/Dockerfile.metrics \
		-t "$(METRICS_IMAGE_NAME)" \
		.
	@echo "Built Docker image \"$(METRICS_IMAGE_NAME)\""

# Run metrics helper unit test suite (must be run natively).
metrics-unit-test: CGO_ENABLED=1
metrics-unit-test: GOARCH=
metrics-unit-test:       ## Run metrics helper unit test suite (must be run natively).
	go test -v -cover -race -timeout 10s \
		./cmd/cni-metrics-helper/metrics/...

# Run metrics helper unit test suite in a container.
docker-metrics-test:     ## Run metrics helper unit test suite in a container.
	docker run $(DOCKER_RUN_FLAGS) \
		-v $(CURDIR):/src --workdir=/src \
		-e GOARCH -e GOOS -e GO111MODULE \
		$(GOLANG_IMAGE) \
		make metrics-unit-test

generate:
	PATH=$(CURDIR)/scripts:$(PATH) go generate -x ./...
	$(MAKE) format

# Generate limit file go code
# Generate eni-max-pods.txt file for EKS AMI
generate-limits: GOOS=
generate-limits:    ## Generate limit file go code
	go run scripts/gen_vpc_ip_limits.go

# Fetch the CNI plugins
plugins: FETCH_VERSION=0.9.0
plugins: FETCH_URL=https://github.com/containernetworking/plugins/releases/download/v$(FETCH_VERSION)/cni-plugins-$(GOOS)-$(GOARCH)-v$(FETCH_VERSION).tgz
plugins: VISIT_URL=https://github.com/containernetworking/plugins/tree/v$(FETCH_VERSION)/plugins/
plugins:   ## Fetch the CNI plugins
	@echo "Fetching Container networking plugins v$(FETCH_VERSION) from upstream release"
	@echo
	@echo "Visit upstream project for plugin details:"
	@echo "$(VISIT_URL)"
	@echo
	curl -L $(FETCH_URL) | tar -zx $(PLUGIN_BINS)

debug-script: FETCH_URL=https://raw.githubusercontent.com/awslabs/amazon-eks-ami/master/log-collector-script/linux/eks-log-collector.sh
debug-script: VISIT_URL=https://github.com/awslabs/amazon-eks-ami/tree/master/log-collector-script/linux
debug-script:    ## Fetching debug script from awslabs/amazon-eks-ami
	@echo "Fetching debug script from awslabs/amazon-eks-ami"
	@echo
	@echo "Visit upstream project for debug script details:"
	@echo "$(VISIT_URL)"
	@echo
	curl -L $(FETCH_URL) -o ./aws-cni-support.sh
	chmod +x ./aws-cni-support.sh

# Run all source code checks.
check: check-format lint vet   ## Run all source code checks.

# Run golint on source code.
#
# To install:
#
#   go get -u golang.org/x/lint/golint
#
lint: LINT_FLAGS = -set_exit_status
lint:   ## Run golint on source code.
	@command -v golint >/dev/null || { echo "ERROR: golint not installed"; exit 1; }
	find . \
	  -type f -name '*.go' \
	  -not -name 'mock_*' -not -name '*mocks.go' -not -name "cni.go" -not -name "eniconfig.go" \
	  -print0 | sort -z | xargs -0 -L1 -- golint $(LINT_FLAGS) 2>/dev/null

helm-lint:
	@${MAKEFILE_PATH}test/helm/helm-lint.sh

# Run go vet on source code.
vet:    ## Run go vet on source code.
	go vet $(ALLPKGS)


docker-vet: build-docker-test   ## Run go vet inside of a container.
	docker run $(DOCKER_RUN_FLAGS) \
		$(TEST_IMAGE_NAME) make vet

format:       ## Format all Go source code files. (Note! integration_test.go has an upstream import dependency that doesn't match)
	@command -v goimports >/dev/null || { echo "ERROR: goimports not installed"; exit 1; }
	@exit $(shell find ./* \
	  -type f \
	  -not -name 'integration_test.go' \
	  -not -name 'mock_publisher.go' \
	  -not -name 'rpc.pb.go' \
	  -name '*.go' \
	  -print0 | sort -z | xargs -0 -- goimports $(or $(FORMAT_FLAGS),-w) | wc -l | bc)

# Check formatting of source code files without modification.
check-format: FORMAT_FLAGS = -l
check-format: format

version:
	@echo ${VERSION}

ekscharts-sync:
	${MAKEFILE_PATH}/scripts/sync-to-eks-charts.sh -b ${HELM_CHART_NAME} -r ${REPO_FULL_NAME}

ekscharts-sync-release:
	${MAKEFILE_PATH}/scripts/sync-to-eks-charts.sh -b ${HELM_CHART_NAME} -r ${REPO_FULL_NAME} -n


upload-resources-to-github:
	${MAKEFILE_PATH}/scripts/upload-resources-to-github.sh

generate-cni-yaml:
	${MAKEFILE_PATH}/scripts/generate-cni-yaml.sh

release: generate-cni-yaml upload-resources-to-github

setup-ec2-sdk-override:
	@if [ "$(EC2_SDK_OVERRIDE)" = "y" ] ; then \
	    ./scripts/ec2_model_override/setup.sh ; \
	fi

cleanup-ec2-sdk-override:
	@if [ "$(EC2_SDK_OVERRIDE)" = "y" ] ; then \
	    ./scripts/ec2_model_override/cleanup.sh ; \
	fi

# Clean temporary files and build artifacts from the project.
clean:    ## Clean temporary files and build artifacts from the project.
	@rm -f -- $(BINS)
	@rm -f -- $(PLUGIN_BINS)
	@rm -f -- coverage.txt

help:           ## Show this help.
	@grep -F -h "##" $(MAKEFILE_LIST) | grep -F -v grep | sed -e 's/\\$$//' \
		| awk -F'[:#]' '{print $$1 = sprintf("%-30s", $$1), $$4}'
