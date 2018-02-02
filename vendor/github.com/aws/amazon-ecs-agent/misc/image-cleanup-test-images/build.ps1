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


# TODO be smart about this and only rebuild if necessary
docker build -t "amazon/image-cleanup-test-image1:make" -f "${PSScriptRoot}/windows0.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image2:make" -f "${PSScriptRoot}/windows1.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image3:make" -f "${PSScriptRoot}/windows2.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image4:make" -f "${PSScriptRoot}/windows3.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image5:make" -f "${PSScriptRoot}/windows4.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image6:make" -f "${PSScriptRoot}/windows5.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image7:make" -f "${PSScriptRoot}/windows6.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image8:make" -f "${PSScriptRoot}/windows7.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image9:make" -f "${PSScriptRoot}/windows8.dockerfile" ${PSScriptRoot}
docker build -t "amazon/image-cleanup-test-image10:make" -f "${PSScriptRoot}/windows9.dockerfile" ${PSScriptRoot}
