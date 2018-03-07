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

# build binary

GOOS ?= linux
GOARCH ?= amd64 
COMMIT := $(shell git rev-parse --short HEAD)

static:
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o aws-k8s-agent main.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o aws-cni plugins/routed-eni/cni.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build verify-aws.go
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build verify-network.go

# need to bundle certificates
certs: misc/certs/ca-certificates.crt
misc/certs/ca-certificates.crt:
	docker build -t "amazon/amazon-k8s-cert-source:make" misc/certs/
	docker run "amazon/amazon-k8s-cert-source:make" cat /etc/ssl/certs/ca-certificates.crt > misc/certs/ca-certificates.crt


# build docker image
docker: static certs
	@docker build -f scripts/dockerfiles/Dockerfile.release -t "889883130442.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni:$(COMMIT)" .
	@echo "Built Docker image \"amazon/amazon-k8s-cni:$(COMMIT)\""

docker-push: docker
	docker push "889883130442.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni"

# unit-test
unit-test:
	go test -v -cover -race -timeout 60s ./pkg/awsutils/...
	go test -v -cover -race -timeout 10s ./plugins/routed-eni/...
	go test -v -cover -race -timeout 10s ./plugins/routed-eni/driver
	go test -v -cover -race -timeout 10s ./pkg/k8sapi/...
	go test -v -cover -race -timeout 10s ./pkg/networkutils/...
	go test -v -cover -race -timeout 10s ./ipamd/...

#golint
lint:
	golint pkg/awsutils/*.go
	golint plugins/routed-eni/*.go
	golint plugins/routed-eni/driver/*.go
	golint pkg/k8sapi/*.go
	golint pkg/networkutils/*.go
	golint ipamd/*.go
	golint ipamd/*/*.go

#go tool vet
vet:
	go tool vet ./pkg/awsutils
	go tool vet ./plugins/routed-eni
	go tool vet ./pkg/k8sapi
	go tool vet ./pkg/networkutils
