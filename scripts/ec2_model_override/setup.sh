#!/bin/bash
 
set -e

mkdir -p ./vendor/github.com/aws

SDK_MODEL_SOURCE=/home/varavaj/AWS/tmp/release-automation/staging-sdk211076960/sdk/src/github.com/aws/aws-sdk-go/models/apis/ec2/2016-11-15
SDK_VENDOR_PATH=./vendor/github.com/aws/aws-sdk-go
API_VERSION=2016-11-15
API_PATH=$SDK_VENDOR_PATH/models/apis/ec2/$API_VERSION
SERVICE_NAME=$([ "$EC2_PREVIEW" == "y" ] && echo "pd-preview" || echo "pdmesh" )

# Clone the SDK to the vendor path (removing an old one if necessary)
rm -rf $SDK_VENDOR_PATH
git clone --depth 1 https://github.com/aws/aws-sdk-go.git $SDK_VENDOR_PATH

# Override the SDK models for App Mesh
#curl -s $SDK_MODEL_SOURCE/api.json |\
    # Always use the "vanilla" flavors of UID, Service ID, and Service Name.
    # This ensures the SDK we generate is always the same interface objects, only changing
    # the endpoint and signing name when using preview.
#    jq "(.metadata.uid) |= \"appmesh-$API_VERSION\"" |\
#    jq "(.metadata.serviceId) |= \"App Mesh\"" |\
#    jq "(.metadata.serviceFullName) |= \"AWS App Mesh\"" |\
    # Set the endpoint prefix and signing name to the desired value, based on
    # whether or not we're using the preview endpoint
#    jq "(.metadata | .endpointPrefix, .signingName) |= \"$SERVICE_NAME\"" \
#    > $API_PATH/api-2.json
#curl -s $SDK_MODEL_SOURCE/docs.json > $API_PATH/docs-2.json
#curl -s $SDK_MODEL_SOURCE/examples.json > $API_PATH/examples-1.json
#curl -s $SDK_MODEL_SOURCE/paginators.json > $API_PATH/paginators-1.json

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
