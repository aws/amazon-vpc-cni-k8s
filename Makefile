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
static:
	go build -o aws-k8s-agent main.go
	go build -o aws-cni plugins/routed-eni/cni.go
	go build verify-aws.go
	go build verify-network.go

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
