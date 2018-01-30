# Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved
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

$oldPref = $ErrorActionPreference
$ErrorActionPreference = 'Stop'

Invoke-Expression ${PSScriptRoot}\..\windows-deploy\hostsetup.ps1

# Create amazon/amazon-ecs-iamrolecontainer for tests
$buildscript = @"
mkdir C:\IAM
cp C:\ecs\ec2.go C:\IAM
go get -u  github.com/aws/aws-sdk-go
go get -u  github.com/aws/aws-sdk-go/aws
go build -o C:\IAM\ec2.exe C:\IAM\ec2.go
cp C:\IAM\ec2.exe C:\ecs
"@

$buildimage="golang:1.7-windowsservercore"
docker pull $buildimage

docker run `
  --volume ${PSScriptRoot}:C:\ecs `
  $buildimage `
  powershell ${buildscript}

Invoke-Expression "docker build -t amazon/amazon-ecs-iamrolecontainer --file ${PSScriptRoot}\iamroles.dockerfile ${PSScriptRoot}"
$ErrorActionPreference = $oldPref
