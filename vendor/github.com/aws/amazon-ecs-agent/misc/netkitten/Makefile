# Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#	http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.
.PHONY: all clean docker image

all: netkitten image

netkitten:
	docker run -v $(shell pwd):/out -v $(shell pwd)/netkitten.go:/in/netkitten.go -e CGO_ENABLED=0 golang:1.7 go build -a -installsuffix cgo -ldflags '-s' -o /out/netkitten /in/netkitten.go

image: netkitten
	@./docker-image


clean:
	rm -f netkitten
	-docker rmi "amazon/amazon-ecs-netkitten:make"
