#!/usr/bin/env bash

set -euo pipefail

DIR=$(cd "$(dirname "$0")"; pwd)
source $DIR/up-test-cluster.sh
source $DIR/down-test-cluster.sh

OS=$(go env GOOS)
ARCH=amd64
TEST_ID=$RANDOM
CLUSTER_NAME=test-cluster-$TEST_ID
BASE_DIR=$(dirname $0)
TEST_DIR=/tmp/cni-test
REPORT_DIR=${TEST_DIR}/report
REGION=${AWS_REGION:-us-west-2}
K8S_VERSION=${K8S_VERSION:-1.14.1}
PROVISION=${PROVISION:-true}
DEPROVISION=${DEPROVISION:-true}
BUILD=${BUILD:-true}

echo "Testing in region: $REGION"

TESTER_DOWNLOAD_URL=https://github.com/aws/aws-k8s-tester/releases/download/v0.4.3/aws-k8s-tester-v0.4.3-$OS-$ARCH
TESTER_PATH=$TEST_DIR/aws-k8s-tester

mkdir -p $TEST_DIR
mkdir -p $REPORT_DIR

# Download aws-k8s-tester if not yet
if [[ ! -e $TESTER_PATH ]]; then
  echo "Downloading aws-k8s-tester from $TESTER_DOWNLOAD_URL to $TESTER_PATH"
  curl -L -X GET $TESTER_DOWNLOAD_URL -o $TESTER_PATH
  chmod +x $TESTER_PATH
fi

if [[ "$PROVISION" = true ]]; then
    up-test-cluster
fi

if [[ "$BUILD" = true ]]; then
    # Push test image
    eval $(aws ecr get-login --region $REGION --no-include-email)
    AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
    IMAGE_NAME="$AWS_ACCOUNT_ID.dkr.ecr.$REGION.amazonaws.com/amazon/amazon-k8s-cni"
    IMAGE_VERSION=$(git describe --tags --always --dirty)

    make docker IMAGE=$IMAGE_NAME VERSION=$IMAGE_VERSION
    docker push $IMAGE_NAME:$IMAGE_VERSION

    sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni,$IMAGE_NAME," ./config/v1.5/aws-k8s-cni.yaml
    sed -i'.bak' "s,v1.5.3,$IMAGE_VERSION," ./config/v1.5/aws-k8s-cni.yaml
fi

echo "Deploying CNI"
export KUBECONFIG=/tmp/aws-k8s-tester/kubeconfig
kubectl apply -f ./config/v1.5/aws-k8s-cni.yaml

# Run the test
pushd ./test/integration
go test -v -timeout 0 ./... --kubeconfig=$KUBECONFIG --ginkgo.focus="\[cni-integration\]" --ginkgo.skip="\[Disruptive\]" \
    --assets=${DIR}/../test/integration/assets
TEST_PASS=$?
popd

if [[ "$DEPROVISION" = true ]]; then
    down-test-cluster
fi

rm -rf $TEST_DIR

if [[ $TEST_PASS -ne 0 ]]; then
  exit 1
fi
