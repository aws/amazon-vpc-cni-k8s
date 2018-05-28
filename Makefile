# Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

.PHONY: build-linux clean docker docker-build lint unit-test vet download-portmap build-docker-test

IMAGE   ?= amazon/amazon-k8s-cni
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS ?= -X main.version=$(VERSION)

ARCH ?= $(shell uname -m)

ifeq ($(ARCH),aarch64)
  ARCH = arm64
else
endif
ifeq ($(ARCH),x86_64)
  ARCH = amd64
endif

# Download portmap plugin
download-portmap:
	mkdir -p tmp/downloads
	mkdir -p tmp/plugins
	curl -L -o tmp/downloads/cni-plugins-$(ARCH).tgz https://github.com/containernetworking/plugins/releases/download/v0.6.0/cni-plugins-$(ARCH)-v0.6.0.tgz
	tar -vxf tmp/downloads/cni-plugins-$(ARCH).tgz -C tmp/plugins
	cp tmp/plugins/portmap .
	rm -rf tmp

# Default to build the Linux binary
build-linux:
	GOOS=linux CGO_ENABLED=0 go build -o aws-k8s-agent
	GOOS=linux CGO_ENABLED=0 go build -o aws-cni ./plugins/routed-eni/

docker-build:
	docker run -v $(shell pwd):/usr/src/app/src/github.com/aws/amazon-vpc-cni-k8s \
		--workdir=/usr/src/app/src/github.com/aws/amazon-vpc-cni-k8s \
		--env GOPATH=/usr/src/app \
		golang:1.10 make build-linux

# Build docker image
docker:
	@docker build --build-arg arch="$(ARCH)" -f scripts/dockerfiles/Dockerfile.release -t "$(IMAGE):$(VERSION)" .
	@echo "Built Docker image \"$(IMAGE):$(VERSION)\""

docker-metrics:
	@docker build -f scripts/dockerfiles/Dockerfile.cni -t "amazon/cni-metrics-helper:latest" .
	@echo "Built Docker image \"amazon/cni-metrics-helper:latest\""

# unit-test
unit-test:
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 150s ./pkg/awsutils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./plugins/routed-eni/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/k8sapi/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/networkutils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/utils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/eniconfig/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./ipamd/...

build-docker-test:
	@docker build -f scripts/dockerfiles/Dockerfile.test -t amazon-k8s-cni-test:latest .

docker-unit-test: build-docker-test
	docker run -e GO111MODULE=on \
		amazon-k8s-cni-test:latest make unit-test

# golint
# To install: go get -u golang.org/x/lint/golint
lint:
	golint pkg/awsutils/*.go
	golint plugins/routed-eni/*.go
	golint plugins/routed-eni/driver/*.go
	golint pkg/k8sapi/*.go
	golint pkg/networkutils/*.go
	golint ipamd/*.go
	golint ipamd/*/*.go

# go vet
vet:
	GOOS=linux go vet

docker-vet: build-docker-test
	docker run -e GO111MODULE=on \
		amazon-k8s-cni-test:latest make vet

clean:
	rm -f aws-k8s-agent
	rm -f aws-cni
	rm -f portmap
