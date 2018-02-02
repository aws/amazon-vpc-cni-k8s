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

all: gremlin image

gremlin: gremlin.go
	docker run -v $(shell pwd):/out -v $(shell pwd)/gremlin.go:/in/gremlin.go -e CGO_ENABLED=0 golang:1.7 go build -a -x -ldflags '-s' -o /out/gremlin /in/gremlin.go

image: gremlin
	@./docker-image


clean:
	rm -f gremlin
	-docker rmi -f "amazon/amazon-ecs-gremlin:make"
