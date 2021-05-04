#!/usr/bin/env bash

set -Euo pipefail

trap 'on_error $LINENO' ERR

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
: "${K8S_VERSION:=1.16.10}"
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
    # Make sure we destroy any cluster that was created if we hit run into an
    # error when attempting to run tests against the 
    if [[ $RUNNING_PERFORMANCE == false ]]; then
        if [[ $__cluster_created -eq 1 && $__cluster_deprovisioned -eq 0 && "$DEPROVISION" == true ]]; then
            # prevent double-deprovisioning with ctrl-c during deprovisioning...
            __cluster_deprovisioned=1
            echo "Cluster was provisioned already. Deprovisioning it..."
            if [[ $RUN_KOPS_TEST == true ]]; then
                down-kops-cluster
            elif [[ $RUN_BOTTLEROCKET_TEST == true ]]; then
                eksctl delete cluster bottlerocket
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
: "${KUBECTL_PATH:=$TESTER_DIR/kubectl}"
export PATH=${PATH}:$TESTER_DIR

LOCAL_GIT_VERSION=$(git describe --tags --always --dirty)
# The manifest image version is the image tag we need to replace in the
# aws-k8s-cni.yaml manifest
: "${MANIFEST_IMAGE_VERSION:=latest}"
TEST_IMAGE_VERSION=${IMAGE_VERSION:-$LOCAL_GIT_VERSION}
# We perform an upgrade to this manifest, with image replaced
: "${MANIFEST_CNI_VERSION:=master}"
BASE_CONFIG_PATH="$DIR/../config/$MANIFEST_CNI_VERSION/aws-k8s-cni.yaml"
TEST_CONFIG_PATH="$TEST_CONFIG_DIR/aws-k8s-cni.yaml"
TEST_CALICO_PATH="$DIR/../config/$MANIFEST_CNI_VERSION/calico.yaml"

if [[ ! -f "$BASE_CONFIG_PATH" ]]; then
    echo "$BASE_CONFIG_PATH DOES NOT exist. Set \$MANIFEST_CNI_VERSION to an existing directory in ./config/"
    exit
fi

if [[ $RUN_CALICO_TEST == true && ! -f "$TEST_CALICO_PATH" ]]; then
    echo "$TEST_CALICO_PATH DOES NOT exist."
    exit
fi

# double-check all our preconditions and requirements have been met
check_is_installed docker
check_is_installed aws
check_aws_credentials
ensure_aws_k8s_tester

: "${AWS_ACCOUNT_ID:=$(aws sts get-caller-identity --query Account --output text)}"
: "${AWS_ECR_REGISTRY:="$AWS_ACCOUNT_ID.dkr.ecr.$AWS_DEFAULT_REGION.amazonaws.com"}"
: "${AWS_ECR_REPO_NAME:="amazon-k8s-cni"}"
: "${IMAGE_NAME:="$AWS_ECR_REGISTRY/$AWS_ECR_REPO_NAME"}"
: "${AWS_INIT_ECR_REPO_NAME:="amazon-k8s-cni-init"}"
: "${INIT_IMAGE_NAME:="$AWS_ECR_REGISTRY/$AWS_INIT_ECR_REPO_NAME"}"
: "${ROLE_CREATE:=true}"
: "${ROLE_ARN:=""}"
: "${MNG_ROLE_ARN:=""}"

# S3 bucket initialization
: "${S3_BUCKET_CREATE:=true}"
: "${S3_BUCKET_NAME:=""}"

echo "Logging in to docker repo"
aws ecr get-login-password --region $AWS_DEFAULT_REGION | docker login --username AWS --password-stdin ${AWS_ECR_REGISTRY}
ensure_ecr_repo "$AWS_ACCOUNT_ID" "$AWS_ECR_REPO_NAME"
ensure_ecr_repo "$AWS_ACCOUNT_ID" "$AWS_INIT_ECR_REPO_NAME"

# Check to see if the image already exists in the Docker repository, and if
# not, check out the CNI source code for that image tag, build the CNI
# image and push it to the Docker repository
if [[ $(docker images -q "$IMAGE_NAME:$TEST_IMAGE_VERSION" 2> /dev/null) ]]; then
    echo "CNI image $IMAGE_NAME:$TEST_IMAGE_VERSION already exists in repository. Skipping image build..."
    DOCKER_BUILD_DURATION=0
else
    echo "CNI image $IMAGE_NAME:$TEST_IMAGE_VERSION does not exist in repository."
    if [[ $TEST_IMAGE_VERSION != "$LOCAL_GIT_VERSION" ]]; then
        __cni_source_tmpdir="${TEST_BASE_DIR}/cni-src-$IMAGE_VERSION"
        echo "Checking out CNI source code for $IMAGE_VERSION ..."

        git clone --depth=1 --branch "$TEST_IMAGE_VERSION" \
            https://github.com/aws/amazon-vpc-cni-k8s "$__cni_source_tmpdir" || exit 1
        pushd "$__cni_source_tmpdir"
    fi
    START=$SECONDS
    make docker IMAGE="$IMAGE_NAME" VERSION="$TEST_IMAGE_VERSION"
    docker push "$IMAGE_NAME:$TEST_IMAGE_VERSION"
    DOCKER_BUILD_DURATION=$((SECONDS - START))
    echo "TIMELINE: Docker build took $DOCKER_BUILD_DURATION seconds."
    # Build matching init container
    make docker-init INIT_IMAGE="$INIT_IMAGE_NAME" VERSION="$TEST_IMAGE_VERSION"
    docker push "$INIT_IMAGE_NAME:$TEST_IMAGE_VERSION"
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
    if [[ "$RUN_BOTTLEROCKET_TEST" == true ]]; then
        ensure_eksctl
        eksctl create cluster --config-file ./testdata/bottlerocket.yaml
    elif [[ "$RUN_KOPS_TEST" == true ]]; then
        up-kops-cluster
    else
        up-test-cluster
    fi
    UP_CLUSTER_DURATION=$((SECONDS - START))
    echo "TIMELINE: Upping test cluster took $UP_CLUSTER_DURATION seconds."
fi
__cluster_created=1

UP_CLUSTER_DURATION=$((SECONDS - START))
echo "TIMELINE: Upping test cluster took $UP_CLUSTER_DURATION seconds."

echo "Using $BASE_CONFIG_PATH as a template"
cp "$BASE_CONFIG_PATH" "$TEST_CONFIG_PATH"

# Daemonset template
echo "IMAGE NAME ${IMAGE_NAME} "
sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni,$IMAGE_NAME," "$TEST_CONFIG_PATH"
sed -i'.bak' "s,:$MANIFEST_IMAGE_VERSION,:$TEST_IMAGE_VERSION," "$TEST_CONFIG_PATH"
sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni-init,$INIT_IMAGE_NAME," "$TEST_CONFIG_PATH"
sed -i'.bak' "s,:$MANIFEST_IMAGE_VERSION,:$TEST_IMAGE_VERSION," "$TEST_CONFIG_PATH"

if [[ $RUN_KOPS_TEST == true || $RUN_BOTTLEROCKET_TEST == true ]]; then
    KUBECTL_PATH=kubectl
    export KUBECONFIG=~/.kube/config
else
    export KUBECONFIG=$KUBECONFIG_PATH
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
sleep 5
while [[ $($KUBECTL_PATH describe ds aws-node -n=kube-system | grep "Available Pods: 0") ]]
do
    sleep 5
    echo "Waiting for daemonset update"
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

if [[ $TEST_PASS -eq 0 && "$RUN_CONFORMANCE" == true ]]; then
  echo "Running conformance tests against cluster."
  START=$SECONDS

  GOPATH=$(go env GOPATH)
  echo "PATH: $PATH"

  echo "Downloading kubernetes test from: https://dl.k8s.io/v$K8S_VERSION/kubernetes-test.tar.gz"
  wget -qO- https://dl.k8s.io/v$K8S_VERSION/kubernetes-test.tar.gz | tar -zxvf - --strip-components=4 -C ${TEST_BASE_DIR}  kubernetes/platforms/linux/amd64/e2e.test

  echo "Running e2e tests: "
  ${TEST_BASE_DIR}/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.failFast --ginkgo.flakeAttempts 2 \
    --ginkgo.skip="(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"

  CONFORMANCE_DURATION=$((SECONDS - START))
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
        eksctl delete cluster bottlerocket
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
