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

GOVERSION=1.10.0
UNIQUE:=$(shell date +%s)
MAKEDIR:=$(strip $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))"))

# build binary
static:
	go build -o .build/aws-k8s-agent cmd/ipamd/main.go
	go build -o .build/aws-cni cmd/cni/main.go

# need to bundle certificates
certs: docker/certs/ca-certificates.crt
docker/certs/ca-certificates.crt:
	docker build -t "amazon/amazon-k8s-cert-source:make" docker/certs/
	docker run "amazon/amazon-k8s-cert-source:make" cat /etc/ssl/certs/ca-certificates.crt > docker/certs/ca-certificates.crt


# build docker image
docker: certs
	@docker build -f docker/Dockerfile.release -t "amazon/amazon-k8s-cni:latest" .
	@echo "Built Docker image \"amazon/amazon-k8s-cni:latest\""

# unit-test
unit-test:
	go test -v -cover -race -timeout 60s ./ipamd/...
	go test -v -cover -race -timeout 10s ./cni/...

#golint
lint:
	golint cni/*.go
	golint cni/driver/*.go
	golint ipamd/*.go
	golint ipamd/*/*.go
	golint cmd/*/*.go

#go tool vet
vet:
	go tool vet ./ipamd
	go tool vet ./ipamd/datastore
	go tool vet ./ipamd/eni
	go tool vet ./ipamd/network
	go tool vet ./ipamd/metrics
	go tool vet ./cni
	go tool vet ./cni/driver
	go tool vet ./cmd/cni
	go tool vet ./cmd/ipamd

DOCKER_OPTS:=--name=amazon-vpc-cni-k8s-${UNIQUE} -e STATIC_BUILD=yes -e VERSION=${VERSION} -v ${MAKEDIR}:/go/src/github.com/aws/amazon-vpc-cni-k8s golang:${GOVERSION}

.PHONY: docker-static
docker-static:
	docker pull golang:${GOVERSION} # Keep golang image up to date
	docker run ${DOCKER_OPTS} make -C /go/src/github.com/aws/amazon-vpc-cni-k8s/ static
	docker cp amazon-vpc-cni-k8s-${UNIQUE}:/go/src/github.com/aws/amazon-vpc-cni-k8s/.build .

.PHONY: docker-unit-test
docker-unit-test:
	docker pull golang:${GOVERSION} # Keep golang image up to date
	docker run ${DOCKER_OPTS} make -C /go/src/github.com/aws/amazon-vpc-cni-k8s/ unit-test

mock:
	go get github.com/golang/mock/gomock
	go get github.com/golang/mock/mockgen
	mockgen -package=driver -destination=${GOPATH}/src/github.com/aws/amazon-vpc-cni-k8s/cni/driver/driver_mock.go github.com/aws/amazon-vpc-cni-k8s/cni/driver NetLink,NS,IP
	mockgen -package=driver -destination=${GOPATH}/src/github.com/aws/amazon-vpc-cni-k8s/cni/driver/netlink_mock.go github.com/vishvananda/netlink Link
	mockgen -package=driver -destination=${GOPATH}/src/github.com/aws/amazon-vpc-cni-k8s/cni/driver/cni_mock.go github.com/containernetworking/cni/pkg/ns NetNS

.PHONY: docker-mock
docker-mock:
	docker pull golang:${GOVERSION} # Keep golang image up to date
	docker run ${DOCKER_OPTS} make -C /go/src/github.com/aws/amazon-vpc-cni-k8s/ mock
