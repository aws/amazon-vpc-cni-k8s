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

.PHONY: build-linux clean docker docker-build lint unit-test vet

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
docker: docker-build
	@docker build -f scripts/dockerfiles/Dockerfile.release -t "amazon/amazon-k8s-cni:latest" .
	@echo "Built Docker image \"amazon/amazon-k8s-cni:latest\""

# unit-test
unit-test:
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 150s ./pkg/awsutils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./plugins/routed-eni/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./plugins/routed-eni/driver
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/k8sapi/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./pkg/networkutils/...
	GOOS=linux CGO_ENABLED=1 go test -v -cover -race -timeout 10s ./ipamd/...

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

#go tool vet
vet:
	go tool vet ./pkg/awsutils
	go tool vet ./plugins/routed-eni
	go tool vet ./pkg/k8sapi
	go tool vet ./pkg/networkutils

clean:
	rm -f aws-k8s-agent
	rm -f aws-cni
