#!/bin/bash
 
set -e

mkdir -p ./vendor/github.com/aws

SDK_MODEL_SOURCE=./hack/ec2_preview_models
SDK_VENDOR_PATH=./vendor/github.com/aws/aws-sdk-go
API_VERSION=2016-11-15
API_PATH=$SDK_VENDOR_PATH/models/apis/ec2/$API_VERSION

# Clone the SDK to the vendor path (removing an old one if necessary)
rm -rf $SDK_VENDOR_PATH
git clone --depth 1 https://github.com/aws/aws-sdk-go.git $SDK_VENDOR_PATH

# Override the SDK models for AWS VPC CNI 
cp $SDK_MODEL_SOURCE/api-2.json $API_PATH/api-2.json
cp $SDK_MODEL_SOURCE/docs-2.json $API_PATH/docs-2.json
cp $SDK_MODEL_SOURCE/examples-1.json $API_PATH/examples-1.json
cp $SDK_MODEL_SOURCE/paginators-1.json $API_PATH/paginators-1.json

# Generate the SDK
pushd ./vendor/github.com/aws/aws-sdk-go
make generate
popd

# Use the vendored version of aws-sdk-go
go mod edit -replace github.com/aws/aws-sdk-go=./vendor/github.com/aws/aws-sdk-go
go mod tidy
