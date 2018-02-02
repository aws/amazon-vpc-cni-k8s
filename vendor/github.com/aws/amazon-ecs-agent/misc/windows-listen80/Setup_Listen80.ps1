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

docker pull microsoft/windowsservercore
Invoke-Expression "go build -o ${PSScriptRoot}\listen80.exe ${PSScriptRoot}\listen80.go"
Invoke-Expression "docker build -t amazon/amazon-ecs-listen80 --file ${PSScriptRoot}\listen80.dockerfile ${PSScriptRoot}"
