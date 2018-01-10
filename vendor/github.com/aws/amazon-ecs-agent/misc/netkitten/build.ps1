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

$buildscript = @"
mkdir C:\nk
cp C:\netkitten\netkitten.go C:\nk
go build -o C:\nk\netkitten.exe C:\nk\netkitten.go
cp C:\nk\netkitten.exe C:\netkitten
"@

docker run `
  --volume ${PSScriptRoot}:C:\netkitten `
  golang:1.7-windowsservercore `
  powershell ${buildscript}

docker build -t "amazon/amazon-ecs-netkitten:make" -f "${PSScriptRoot}/windows.dockerfile" ${PSScriptRoot}
