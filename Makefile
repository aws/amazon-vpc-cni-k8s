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

.PHONY: all build-linux clean format check-format docker docker-build lint unit-test vet download-portmap build-docker-test build-metrics docker-metrics metrics-unit-test docker-metrics-test docker-vet

IMAGE   ?= amazon/amazon-k8s-cni
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS ?= -X main.version=$(VERSION)
DOCKER_ARGS ?=
ALLPKGS := $(shell go list ./...)

ARCH ?= $(shell uname -m)

ifeq ($(ARCH),aarch64)
  ARCH = arm64
else
endif
ifeq ($(ARCH),x86_64)
  ARCH = amd64
endif

# Default to build the Linux binary
build-linux:
	GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 go build -o aws-k8s-agent -ldflags "-s -w $(LDFLAGS)" ./cmd/aws-k8s-agent/
	GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 go build -o aws-cni -ldflags " -s -w $(LDFLAGS)" ./cmd/routed-eni-cni-plugin/
	GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 go build -o grpc-health-probe -ldflags "-s -w $(LDFLAGS)" ./cmd/grpc-health-probe/

# Download portmap plugin
download-portmap:
	mkdir -p tmp/downloads
	mkdir -p tmp/plugins
	curl -L -o tmp/downloads/cni-plugins-$(ARCH).tgz https://github.com/containernetworking/plugins/releases/download/v0.7.5/cni-plugins-$(ARCH)-v0.7.5.tgz
	tar -vxf tmp/downloads/cni-plugins-$(ARCH).tgz -C tmp/plugins
	cp tmp/plugins/portmap .
	rm -rf tmp

# Build CNI Docker image
docker:
	@docker build $(DOCKER_ARGS) --build-arg arch="$(ARCH)" -f scripts/dockerfiles/Dockerfile.release -t "$(IMAGE):$(VERSION)" .
	@echo "Built Docker image \"$(IMAGE):$(VERSION)\""

docker-func-test: docker
	docker run $(DOCKER_ARGS) -it "$(IMAGE):$(VERSION)"

# unit-test
unit-test:
	GOOS=linux CGO_ENABLED=1 go test -v -cover $(ALLPKGS)

# unit-test-race
unit-test-race:
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./cmd/*/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 150s ./pkg/awsutils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/k8sapi/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/networkutils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/utils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/eniconfig/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/ipamd/...

build-docker-test:
	@docker build -f scripts/dockerfiles/Dockerfile.test -t amazon-k8s-cni-test:latest .

docker-unit-test: build-docker-test
	docker run -e GO111MODULE=on \
		amazon-k8s-cni-test:latest make unit-test

# Build metrics
build-metrics:
	GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 go build -ldflags="-s -w" -o cni-metrics-helper ./cmd/cni-metrics-helper/

# Build metrics Docker image
docker-metrics:
	@docker build --build-arg arch="$(ARCH)" -f scripts/dockerfiles/Dockerfile.metrics -t "amazon/cni-metrics-helper:$(VERSION)" .
	@echo "Built Docker image \"amazon/cni-metrics-helper:$(VERSION)\""

metrics-unit-test:
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./cmd/cni-metrics-helper/metrics/...

docker-metrics-test:
	docker run -v $(shell pwd):/usr/src/app/src/github.com/aws/amazon-vpc-cni-k8s \
		--workdir=/usr/src/app/src/github.com/aws/amazon-vpc-cni-k8s \
		--env GOPATH=/usr/src/app \
		golang:1.10 make metrics-unit-test

# Build both CNI and metrics helper
all: docker docker-metrics

generate:
	go generate -x ./...
	$(MAKE) format

generate-limits:
	go run pkg/awsutils/gen_vpc_ip_limits.go

# golint
# To install: go get -u golang.org/x/lint/golint
lint:
	golint pkg/awsutils/*.go
	golint cmd/routed-eni-cni-plugin/*.go
	golint cmd/routed-eni-cni-plugin/driver/*.go
	golint pkg/k8sapi/*.go
	golint pkg/networkutils/*.go
	golint pkg/ipamd/*.go
	golint pkg/ipamd/*/*.go

# go vet
vet:
	GOOS=linux go vet ./...

docker-vet: build-docker-test
	docker run -e GO111MODULE=on \
		amazon-k8s-cni-test:latest make vet

clean:
	rm -f aws-k8s-agent
	rm -f aws-cni
	rm -f grpc-health-probe
	rm -f cni-metrics-helper
	rm -f portmap

files := $(shell find . -not -name 'mock_publisher.go' -not -name 'rpc.pb.go' -not -name 'integration_test.go' -name '*.go' -print)
unformatted = $(shell goimports -l $(files))

format:
	@echo "== format"
	@goimports -w $(files)
	@sync

check-format:
	@echo "== check formatting"
ifneq "$(unformatted)" ""
	@echo "needs formatting: $(unformatted)"
	@echo "run 'make format'"
	@exit 1
endif
