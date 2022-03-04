#!/usr/bin/env bash

set -Euo pipefail

trap 'on_error $? $LINENO' ERR

DIR=$(cd "$(dirname "$0")"; pwd)
source "$DIR"/lib/common.sh
source "$DIR"/lib/aws.sh
source "$DIR"/lib/cluster.sh
source "$DIR"/lib/integration.sh
source "$DIR"/lib/performance_tests.sh

# Variables used in /lib/aws.sh
OS=$(go env GOOS)
ARCH=$(go env GOARCH)

: "${AWS_DEFAULT_REGION:=us-west-2}"
: "${K8S_VERSION:=1.21.2}"
: "${EKS_CLUSTER_VERSION:=1.21}"
: "${PROVISION:=true}"
: "${DEPROVISION:=true}"
: "${BUILD:=true}"
: "${RUN_CONFORMANCE:=false}"
: "${RUN_TESTER_LB_ADDONS:=false}"
: "${RUN_KOPS_TEST:=false}"
: "${RUN_BOTTLEROCKET_TEST:=false}"
: "${RUN_PERFORMANCE_TESTS:=false}"
: "${RUNNING_PERFORMANCE:=false}"
: "${RUN_CALICO_TEST:=false}"


__cluster_created=0
__cluster_deprovisioned=0

on_error() {
    echo "Error with exit code $1 occurred on line $2"
    emit_cloudwatch_metric "error_occurred" "1"
    # Make sure we destroy any cluster that was created if we hit run into an
    # error when attempting to run tests against the 
    if [[ $RUNNING_PERFORMANCE == false ]]; then
        if [[ $__cluster_created -eq 1 && $__cluster_deprovisioned -eq 0 && "$DEPROVISION" == true ]]; then
            # prevent double-deprovisioning with ctrl-c during deprovisioning...
            __cluster_deprovisioned=1
            echo "Cluster was provisioned already. Deprovisioning it..."
            if [[ $RUN_KOPS_TEST == true ]]; then
                down-kops-cluster
            else
                down-test-cluster
            fi
        fi
        exit 1
    fi
}

# test specific config, results location
: "${TEST_ID:=$RANDOM}"
: "${TEST_BASE_DIR:=${DIR}/cni-test}"
TEST_DIR=${TEST_BASE_DIR}/$(date "+%Y%M%d%H%M%S")-$TEST_ID
REPORT_DIR=${TEST_DIR}/report
TEST_CONFIG_DIR="$TEST_DIR/config"

# test cluster config location
# Pass in CLUSTER_ID to reuse a test cluster
: "${CLUSTER_ID:=$RANDOM}"
CLUSTER_NAME=cni-test-$CLUSTER_ID
TEST_CLUSTER_DIR=${TEST_BASE_DIR}/cluster-$CLUSTER_NAME
CLUSTER_MANAGE_LOG_PATH=$TEST_CLUSTER_DIR/cluster-manage.log
: "${CLUSTER_CONFIG:=${TEST_CLUSTER_DIR}/${CLUSTER_NAME}.yaml}"
: "${KUBECONFIG_PATH:=${TEST_CLUSTER_DIR}/kubeconfig}"
: "${ADDONS_CNI_IMAGE:=""}"

# shared binaries
: "${TESTER_DIR:=${DIR}/aws-k8s-tester}"
: "${TESTER_PATH:=$TESTER_DIR/aws-k8s-tester}"
: "${KUBECTL_PATH:=kubectl}"
export PATH=${PATH}:$TESTER_DIR

LOCAL_GIT_VERSION=$(git rev-parse HEAD)
echo "Testing git repository at commit $LOCAL_GIT_VERSION"
TEST_IMAGE_VERSION=${IMAGE_VERSION:-$LOCAL_GIT_VERSION}
# We perform an upgrade to this manifest, with image replaced
: "${MANIFEST_CNI_VERSION:=master}"
BASE_CONFIG_PATH="$DIR/../config/$MANIFEST_CNI_VERSION/aws-k8s-cni.yaml"
TEST_CONFIG_PATH="$TEST_CONFIG_DIR/aws-k8s-cni.yaml"
TEST_CALICO_PATH="$DIR/../config/$MANIFEST_CNI_VERSION/calico.yaml"
# The manifest image version is the image tag we need to replace in the
# aws-k8s-cni.yaml manifest
MANIFEST_IMAGE_VERSION=`grep "image:" $BASE_CONFIG_PATH | cut -d ":" -f3 | cut -d "\"" -f1 | head -1`

if [[ ! -f "$BASE_CONFIG_PATH" ]]; then
    echo "$BASE_CONFIG_PATH DOES NOT exist. Set \$MANIFEST_CNI_VERSION to an existing directory in ./config/"
    exit
fi

if [[ $RUN_CALICO_TEST == true && ! -f "$TEST_CALICO_PATH" ]]; then
    echo "$TEST_CALICO_PATH DOES NOT exist."
    exit 1
fi

# double-check all our preconditions and requirements have been met
check_is_installed docker
check_is_installed aws
check_aws_credentials

: "${AWS_ACCOUNT_ID:=$(aws sts get-caller-identity --query Account --output text)}"
: "${AWS_ECR_REGISTRY:="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_DEFAULT_REGION.amazonaws.com"}"
: "${AWS_ECR_REPO_NAME:="amazon-k8s-cni"}"
: "${IMAGE_NAME:="$AWS_ECR_REGISTRY/$AWS_ECR_REPO_NAME"}"
: "${AWS_INIT_ECR_REPO_NAME:="amazon-k8s-cni-init"}"
: "${INIT_IMAGE_NAME:="$AWS_ECR_REGISTRY/$AWS_INIT_ECR_REPO_NAME"}"
: "${ROLE_CREATE:=true}"
: "${ROLE_ARN:=""}"
: "${MNG_ROLE_ARN:=""}"
: "${BUILDX_BUILDER:="multi-arch-image-builder"}"

# S3 bucket initialization
: "${S3_BUCKET_CREATE:=true}"
: "${S3_BUCKET_NAME:=""}"

echo "Logging in to docker repo"
aws ecr get-login-password --region $AWS_DEFAULT_REGION | docker login --username AWS --password-stdin ${AWS_ECR_REGISTRY}
ensure_ecr_repo "$AWS_ACCOUNT_ID" "$AWS_ECR_REPO_NAME"
ensure_ecr_repo "$AWS_ACCOUNT_ID" "$AWS_INIT_ECR_REPO_NAME"

# Check to see if the image already exists in the ECR repository, and if
# not, check out the CNI source code for that image tag, build the CNI
# image and push it to the Docker repository
ecr_image_query_result=$(aws ecr batch-get-image --repository-name=amazon-k8s-cni --image-ids imageTag=$TEST_IMAGE_VERSION --query 'images[].imageId.imageTag' --region us-west-2)
if [[ $ecr_image_query_result != "[]" ]]; then
    echo "CNI image $IMAGE_NAME:$TEST_IMAGE_VERSION already exists in repository. Skipping image build..."
    DOCKER_BUILD_DURATION=0
else
    echo "CNI image $IMAGE_NAME:$TEST_IMAGE_VERSION does not exist in repository."
    START=$SECONDS
    # Refer to https://github.com/docker/buildx#building-multi-platform-images for the multi-arch image build process.
    # create the buildx container only if it doesn't exist already.
    docker buildx inspect "$BUILDX_BUILDER" >/dev/null 2<&1 || docker buildx create --name="$BUILDX_BUILDER" --buildkitd-flags '--allow-insecure-entitlement network.host' --use >/dev/null
    make multi-arch-cni-build-push IMAGE="$IMAGE_NAME" VERSION="$TEST_IMAGE_VERSION"
    DOCKER_BUILD_DURATION=$((SECONDS - START))
    echo "TIMELINE: Docker build took $DOCKER_BUILD_DURATION seconds."
    # Build matching init container
    make multi-arch-cni-init-build-push INIT_IMAGE="$INIT_IMAGE_NAME" VERSION="$TEST_IMAGE_VERSION"
    docker buildx rm "$BUILDX_BUILDER"
    if [[ $TEST_IMAGE_VERSION != "$LOCAL_GIT_VERSION" ]]; then
        popd
    fi
fi

echo "*******************************************************************************"
echo "Running $TEST_ID on $CLUSTER_NAME in $AWS_DEFAULT_REGION"
echo "+ Cluster config dir: $TEST_CLUSTER_DIR"
echo "+ Result dir:         $TEST_DIR"
echo "+ Tester:             $TESTER_PATH"
echo "+ Kubeconfig:         $KUBECONFIG_PATH"
echo "+ Cluster config:     $CLUSTER_CONFIG"
echo "+ AWS Account ID:     $AWS_ACCOUNT_ID"
echo "+ CNI image to test:  $IMAGE_NAME:$TEST_IMAGE_VERSION"
echo "+ CNI init container: $INIT_IMAGE_NAME:$TEST_IMAGE_VERSION"

mkdir -p "$TEST_DIR"
mkdir -p "$REPORT_DIR"
mkdir -p "$TEST_CLUSTER_DIR"
mkdir -p "$TEST_CONFIG_DIR"

START=$SECONDS
if [[ "$PROVISION" == true ]]; then
    START=$SECONDS
    if [[ "$RUN_KOPS_TEST" == true ]]; then
        up-kops-cluster
    else
        up-test-cluster
    fi
fi
__cluster_created=1

UP_CLUSTER_DURATION=$((SECONDS - START))
echo "TIMELINE: Upping test cluster took $UP_CLUSTER_DURATION seconds."

echo "Using $BASE_CONFIG_PATH as a template"
cp "$BASE_CONFIG_PATH" "$TEST_CONFIG_PATH"

# Daemonset template
# Replace image value and tag in cni manifest and grep (to verify that replacement was successful)
echo "IMAGE NAME ${IMAGE_NAME} "
sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni,$IMAGE_NAME," "$TEST_CONFIG_PATH"
grep -r -q $IMAGE_NAME $TEST_CONFIG_PATH
echo "Replacing manifest image tag $MANIFEST_IMAGE_VERSION with image version of $TEST_IMAGE_VERSION"
sed -i'.bak' "s,:$MANIFEST_IMAGE_VERSION,:$TEST_IMAGE_VERSION," "$TEST_CONFIG_PATH"
grep -r -q $TEST_IMAGE_VERSION $TEST_CONFIG_PATH
sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni-init,$INIT_IMAGE_NAME," "$TEST_CONFIG_PATH"
grep -r -q $INIT_IMAGE_NAME $TEST_CONFIG_PATH


if [[ $RUN_KOPS_TEST == true ]]; then
    export KUBECONFIG=~/.kube/config
fi

if [[ $RUN_KOPS_TEST == true ]]; then
    run_kops_conformance
fi
ADDONS_CNI_IMAGE=$($KUBECTL_PATH describe daemonset aws-node -n kube-system | grep Image | cut -d ":" -f 2-3 | tr -d '[:space:]')

echo "*******************************************************************************"
echo "Running integration tests on default CNI version, $ADDONS_CNI_IMAGE"
echo ""
START=$SECONDS
pushd ./test/integration
GO111MODULE=on go test -v -timeout 0 ./... --kubeconfig=$KUBECONFIG --ginkgo.focus="\[cni-integration\]" --ginkgo.skip="\[Disruptive\]" \
    --assets=./assets
TEST_PASS=$?
popd
DEFAULT_INTEGRATION_DURATION=$((SECONDS - START))
echo "TIMELINE: Default CNI integration tests took $DEFAULT_INTEGRATION_DURATION seconds."

echo "*******************************************************************************"
echo "Updating CNI to image $IMAGE_NAME:$TEST_IMAGE_VERSION"
echo "Using init container $INIT_IMAGE_NAME:$TEST_IMAGE_VERSION"
START=$SECONDS
$KUBECTL_PATH apply -f "$TEST_CONFIG_PATH"
NODE_COUNT=$($KUBECTL_PATH get nodes --no-headers=true | wc -l)
echo "Number of nodes in the test cluster is $NODE_COUNT"
UPDATED_PODS_COUNT=0
MAX_RETRIES=20
RETRY_ATTEMPT=0
sleep 5
while [[ $UPDATED_PODS_COUNT -lt $NODE_COUNT && $RETRY_ATTEMPT -lt $MAX_RETRIES ]]; do
    UPDATED_PODS_COUNT=$($KUBECTL_PATH get pods -A --field-selector=status.phase=Running -l k8s-app=aws-node --no-headers=true | wc -l)
    let RETRY_ATTEMPT=RETRY_ATTEMPT+1
    sleep 5
    echo "Waiting for cni daemonset update. $UPDATED_PODS_COUNT are in ready state in retry attempt $RETRY_ATTEMPT"
done
echo "Updated!"

CNI_IMAGE_UPDATE_DURATION=$((SECONDS - START))
echo "TIMELINE: Updating CNI image took $CNI_IMAGE_UPDATE_DURATION seconds."

if [[ $RUN_CALICO_TEST == true ]]; then
    $KUBECTL_PATH apply -f "$TEST_CALICO_PATH"
    attempts=60
    while [[ $($KUBECTL_PATH describe ds calico-node -n=kube-system | grep "Available Pods: 0") ]]; do
        if [ "${attempts}" -eq 0 ]; then
            echo "Calico pods seems to be down check the config"
            exit 1
        fi
        
        let attempts--
        sleep 5
        echo "Waiting for calico daemonset update"
    done
    echo "Updated calico daemonset!"
    emit_cloudwatch_metric "calico_test_status" "1"
    sleep 5
fi

echo "*******************************************************************************"
echo "Running integration tests on current image:"
echo ""
START=$SECONDS
pushd ./test/integration
GO111MODULE=on go test -v -timeout 0 ./... --kubeconfig=$KUBECONFIG --ginkgo.focus="\[cni-integration\]" --ginkgo.skip="\[Disruptive\]" \
    --assets=./assets
TEST_PASS=$?
popd
CURRENT_IMAGE_INTEGRATION_DURATION=$((SECONDS - START))
echo "TIMELINE: Current image integration tests took $CURRENT_IMAGE_INTEGRATION_DURATION seconds."
if [[ $TEST_PASS -eq 0 ]]; then
  emit_cloudwatch_metric "integration_test_status" "1"
fi

if [[ $TEST_PASS -eq 0 && "$RUN_CONFORMANCE" == true ]]; then
  echo "Running conformance tests against cluster."
  START=$SECONDS

  GOPATH=$(go env GOPATH)
  echo "PATH: $PATH"

  echo "Downloading kubernetes test from: https://dl.k8s.io/v$K8S_VERSION/kubernetes-test-linux-amd64.tar.gz"
  wget -qO- https://dl.k8s.io/v$K8S_VERSION/kubernetes-test-linux-amd64.tar.gz | tar -zxvf - --strip-components=3 -C ${TEST_BASE_DIR}  kubernetes/test/bin/e2e.test

  echo "Running e2e tests: "
  ${TEST_BASE_DIR}/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
    --ginkgo.skip="(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"

  CONFORMANCE_DURATION=$((SECONDS - START))
  emit_cloudwatch_metric "conformance_test_status" "1"
  echo "TIMELINE: Conformance tests took $CONFORMANCE_DURATION seconds."
fi

if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
    echo "*******************************************************************************"
    echo "Running performance tests on current image:"
    echo ""
    START=$SECONDS
    run_performance_test_130_pods
    run_performance_test_730_pods
    run_performance_test_5000_pods
    PERFORMANCE_DURATION=$((SECONDS - START))
fi

if [[ "$DEPROVISION" == true ]]; then
    START=$SECONDS

    if [[ "$RUN_KOPS_TEST" == true ]]; then
        down-kops-cluster
    elif [[ "$RUN_BOTTLEROCKET_TEST" == true ]]; then
        eksctl delete cluster $CLUSTER_NAME
        emit_cloudwatch_metric "bottlerocket_test_status" "1"
    elif [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        eksctl delete cluster $CLUSTER_NAME
        emit_cloudwatch_metric "performance_test_status" "1"
    else
        down-test-cluster
    fi

    DOWN_DURATION=$((SECONDS - START))
    echo "TIMELINE: Down processes took $DOWN_DURATION seconds."
    display_timelines
fi

if [[ $TEST_PASS -ne 0 ]]; then
    exit 1
fi
emit_cloudwatch_metric "error_occurred" "0"
