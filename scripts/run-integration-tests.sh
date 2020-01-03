#!/usr/bin/env bash

set -Euo pipefail

trap 'on_error $LINENO' ERR

DIR=$(cd "$(dirname "$0")"; pwd)
source $DIR/lib/common.sh
source $DIR/lib/aws.sh
source $DIR/lib/cluster.sh

OS=$(go env GOOS)
ARCH=$(go env GOARCH)
AWS_REGION=${AWS_REGION:-us-west-2}
K8S_VERSION=${K8S_VERSION:-1.14.6}
PROVISION=${PROVISION:-true}
DEPROVISION=${DEPROVISION:-true}
BUILD=${BUILD:-true}

__cluster_created=0
__cluster_deprovisioned=0

on_error() {
    # Make sure we destroy any cluster that was created if we hit run into an
    # error when attempting to run tests against the cluster
    if [[ $__cluster_created -eq 1 && $__cluster_deprovisioned -eq 0 && "$DEPROVISION" = true ]]; then
        # prevent double-deprovisioning with ctrl-c during deprovisioning...
        __cluster_deprovisioned=1
        echo "Cluster was provisioned already. Deprovisioning it..."
        down-test-cluster
    fi
    exit 1
}

# test specific config, results location
TEST_ID=${TEST_ID:-$RANDOM}
TEST_DIR=/tmp/cni-test/$(date "+%Y%M%d%H%M%S")-$TEST_ID
REPORT_DIR=${TEST_DIR}/report

# test cluster config location
# Pass in CLUSTER_ID to reuse a test cluster
CLUSTER_ID=${CLUSTER_ID:-$RANDOM}
CLUSTER_NAME=cni-test-$CLUSTER_ID
TEST_CLUSTER_DIR=/tmp/cni-test/cluster-$CLUSTER_NAME
CLUSTER_MANAGE_LOG_PATH=$TEST_CLUSTER_DIR/cluster-manage.log
CLUSTER_CONFIG=${CLUSTER_CONFIG:-${TEST_CLUSTER_DIR}/${CLUSTER_NAME}.yaml}
SSH_KEY_PATH=${SSH_KEY_PATH:-${TEST_CLUSTER_DIR}/id_rsa}
KUBECONFIG_PATH=${KUBECONFIG_PATH:-${TEST_CLUSTER_DIR}/kubeconfig}

# shared binaries
TESTER_DIR=${TESTER_DIR:-/tmp/aws-k8s-tester}
TESTER_PATH=${TESTER_PATH:-$TESTER_DIR/aws-k8s-tester}
AUTHENTICATOR_PATH=${AUTHENTICATOR_PATH:-$TESTER_DIR/aws-iam-authenticator}
KUBECTL_PATH=${KUBECTL_PATH:-$TESTER_DIR/kubectl}

# double-check all our preconditions and requirements have been met
check_is_installed docker
check_is_installed aws
check_aws_credentials
ensure_aws_k8s_tester

AWS_ACCOUNT_ID=${AWS_ACCOUNT_ID:-$(aws sts get-caller-identity --query Account --output text)}
AWS_ECR_REGISTRY=${AWS_ECR_REGISTRY:-"$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com"}
AWS_ECR_REPO_NAME=${AWS_ECR_REPO_NAME:-"amazon-k8s-cni"}
IMAGE_NAME=${IMAGE_NAME:-"$AWS_ECR_REGISTRY/$AWS_ECR_REPO_NAME"}
IMAGE_VERSION=${IMAGE_VERSION:-$(git describe --tags --always --dirty)}

if [[ "$BUILD" = true ]]; then
    # `aws ec2 get-login` returns a docker login string, which we eval here to
    # login to the ECR registry
    eval $(aws ecr get-login --region $AWS_REGION --no-include-email) >/dev/null 2>&1
    ensure_ecr_repo "$AWS_ACCOUNT_ID" "$AWS_ECR_REPO_NAME"
    make docker IMAGE=$IMAGE_NAME VERSION=$IMAGE_VERSION
    docker push $IMAGE_NAME:$IMAGE_VERSION
fi

# The version substituted in ./config/X/aws-k8s-cni.yaml
CNI_TEMPLATE_VERSION=${CNI_TEMPLATE_VERSION:-v1.5}

echo "*******************************************************************************"
echo "Running $TEST_ID on $CLUSTER_NAME in $AWS_REGION"
echo "+ Cluster config dir: $TEST_CLUSTER_DIR"
echo "+ Result dir:         $TEST_DIR"
echo "+ Tester:             $TESTER_PATH"
echo "+ Kubeconfig:         $KUBECONFIG_PATH"
echo "+ Node SSH key:       $SSH_KEY_PATH"
echo "+ Cluster config:     $CLUSTER_CONFIG"
echo "+ AWS Account ID:     $AWS_ACCOUNT_ID"
echo "+ CNI image to test:  $IMAGE_NAME:$IMAGE_VERSION"

mkdir -p $TEST_DIR
mkdir -p $REPORT_DIR
mkdir -p $TEST_CLUSTER_DIR

if [[ "$PROVISION" = true ]]; then
    up-test-cluster
    __cluster_created=1
fi

if [[ "$BUILD" = true ]]; then
    echo "Using ./config/$CNI_TEMPLATE_VERSION/aws-k8s-cni.yaml as a template"
    if [[ ! -f "./config/$CNI_TEMPLATE_VERSION/aws-k8s-cni.yaml" ]]; then
        echo "./config/$CNI_TEMPLATE_VERSION/aws-k8s-cni.yaml DOES NOT exist. Set \$CNI_TEMPLATE_VERSION to an existing directory in ./config/"
        exit
    fi

    sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni,$IMAGE_NAME," ./config/$CNI_TEMPLATE_VERSION/aws-k8s-cni.yaml
    sed -i'.bak' "s,v1.5.3,$IMAGE_VERSION," ./config/$CNI_TEMPLATE_VERSION/aws-k8s-cni.yaml
fi

echo "*******************************************************************************"
echo "Deploying CNI with image $IMAGE_NAME"
export KUBECONFIG=$KUBECONFIG_PATH
kubectl apply -f ./config/$CNI_TEMPLATE_VERSION/aws-k8s-cni.yaml

echo "*******************************************************************************"
echo "Running integration tests"
echo ""
pushd ./test/integration
GO111MODULE=on go test -v -timeout 0 ./... --kubeconfig=$KUBECONFIG --ginkgo.focus="\[cni-integration\]" --ginkgo.skip="\[Disruptive\]" \
    --assets=./assets
TEST_PASS=$?
popd

if [[ "$DEPROVISION" = true ]]; then
    down-test-cluster
fi

if [[ $TEST_PASS -ne 0 ]]; then
  exit 1
fi
