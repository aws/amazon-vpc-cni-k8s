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

VERSION ?= $(shell git describe --tags --always || echo "unknown")
IMAGE_NAME = amazon/amazon-k8s-cni/test/agent
REPO=$(AWS_ACCOUNT).dkr.ecr.$(AWS_REGION).amazonaws.com/$(IMAGE_NAME)
IMAGE ?= $(REPO):$(VERSION)

fmt:
	go fmt .

# Run go vet against code
vet:
	go vet .

docker-build: check-env
	docker build . -t ${IMAGE}

docker-push: check-env
	docker push ${IMAGE}

check-env:
	@:$(call check_var, AWS_ACCOUNT, AWS account ID for publishing docker images)
	@:$(call check_var, AWS_REGION, AWS region for publishing docker images)

check_var = \
    $(strip $(foreach 1,$1, \
        $(call __check_var,$1,$(strip $(value 2)))))
__check_var = \
    $(if $(value $1),, \
      $(error Undefined variable $1$(if $2, ($2))))