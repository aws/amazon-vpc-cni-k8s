#!/usr/bin/env bash

set -Euo pipefail

trap 'on_error $? $LINENO' ERR

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
INTEGRATION_TEST_DIR="$SCRIPT_DIR"/../test/integration

source "$SCRIPT_DIR"/lib/common.sh
source "$SCRIPT_DIR"/lib/aws.sh
source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/integration.sh
source "$SCRIPT_DIR"/lib/k8s.sh
source "$SCRIPT_DIR"/lib/performance_tests.sh

# Variables used in /lib/aws.sh
OS=$(go env GOOS)
ARCH=$(go env GOARCH)

: "${AWS_DEFAULT_REGION:=us-west-2}"
: "${K8S_VERSION:=""}"
: "${EKS_CLUSTER_VERSION:=""}"
: "${PROVISION:=true}"
: "${DEPROVISION:=true}"
: "${BUILD:=true}"
: "${RUN_CNI_INTEGRATION_TESTS:=true}"
: "${RUN_CONFORMANCE:=false}"
: "${RUN_KOPS_TEST:=false}"
: "${RUN_BOTTLEROCKET_TEST:=false}"
: "${RUN_PERFORMANCE_TESTS:=false}"
: "${RUNNING_PERFORMANCE:=false}"
: "${KOPS_VERSION=v1.30.3}"

if [[ -z $EKS_CLUSTER_VERSION || -z $K8S_VERSION ]]; then
    CLUSTER_INFO=$(eksctl utils describe-cluster-versions --region $AWS_DEFAULT_REGION)
    [[ -z $EKS_CLUSTER_VERSION ]] && EKS_CLUSTER_VERSION=$(echo "$CLUSTER_INFO" | jq -r '.clusterVersions[0].ClusterVersion')
    [[ -z $K8S_VERSION ]] && K8S_VERSION=$(echo "$CLUSTER_INFO" | jq -r '.clusterVersions[0].KubernetesPatchVersion')
    echo "EKS_CLUSTER_VERSION: $EKS_CLUSTER_VERSION"
    echo "K8S_VERSION: $K8S_VERSION"
fi

__cluster_created=0
__cluster_deprovisioned=0

on_error() {
    echo "Error with exit code $1 occurred on line $2"
    emit_cloudwatch_metric "error_occurred" "1"

    #Emit test specific error metric 
    if [[ $RUN_KOPS_TEST == true ]]; then
        emit_cloudwatch_metric "kops_test_status" "0"
    fi
    if [[ $RUN_BOTTLEROCKET_TEST == true ]]; then
        emit_cloudwatch_metric "bottlerocket_test_status" "0"
    fi
    if [[ $RUN_PERFORMANCE_TESTS == true ]]; then
        emit_cloudwatch_metric "performance_test_status" "0"
    fi
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
: "${TEST_BASE_DIR:=${SCRIPT_DIR}/cni-test}"
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

# shared binaries
: "${TESTER_DIR:=${SCRIPT_DIR}/aws-k8s-tester}"
: "${TESTER_PATH:=$TESTER_DIR/aws-k8s-tester}"
: "${KUBECTL_PATH:=kubectl}"
export PATH=${PATH}:$TESTER_DIR

LOCAL_GIT_VERSION=$(git rev-parse HEAD)
echo "Testing git repository at commit $LOCAL_GIT_VERSION"
TEST_IMAGE_VERSION=${IMAGE_VERSION:-$LOCAL_GIT_VERSION}
# We perform an upgrade to this manifest, with image replaced
: "${MANIFEST_CNI_VERSION:=master}"
BASE_CONFIG_PATH="$SCRIPT_DIR/../config/$MANIFEST_CNI_VERSION/aws-k8s-cni.yaml"
TEST_CONFIG_PATH="$TEST_CONFIG_DIR/aws-k8s-cni.yaml"
# The manifest image version is the image tag we need to replace in the
# aws-k8s-cni.yaml manifest
MANIFEST_IMAGE_VERSION=`grep "image:" $BASE_CONFIG_PATH | cut -d ":" -f3 | cut -d "\"" -f1 | head -1`

if [[ ! -f "$BASE_CONFIG_PATH" ]]; then
    echo "$BASE_CONFIG_PATH DOES NOT exist. Set \$MANIFEST_CNI_VERSION to an existing directory in ./config/"
    exit
fi

# double-check all our preconditions and requirements have been met
check_is_installed ginkgo
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

echo "Logging in to docker repo"
aws ecr get-login-password --region $AWS_DEFAULT_REGION | docker login --username AWS --password-stdin ${AWS_ECR_REGISTRY}
ensure_ecr_repo "$AWS_ACCOUNT_ID" "$AWS_ECR_REPO_NAME" "$AWS_DEFAULT_REGION"
ensure_ecr_repo "$AWS_ACCOUNT_ID" "$AWS_INIT_ECR_REPO_NAME" "$AWS_DEFAULT_REGION"

# Check to see if the image already exists in the ECR repository, and if
# not, check out the CNI source code for that image tag, build the CNI
# image and push it to the Docker repository

CNI_IMAGES_BUILD=false
if [[ $(aws ecr batch-get-image --repository-name=$AWS_ECR_REPO_NAME --image-ids imageTag=$TEST_IMAGE_VERSION \
--query 'images[].imageId.imageTag' --region "$AWS_DEFAULT_REGION") != "[]" ]]; then
    echo "Image $AWS_ECR_REPO_NAME:$TEST_IMAGE_VERSION already exists. Skipping image build."
else
    echo "Image $AWS_ECR_REPO_NAME:$TEST_IMAGE_VERSION does not exist in repository."
    ## $1=command, $2=args
    build_and_push_image "multi-arch-cni-build-push" "IMAGE=$IMAGE_NAME VERSION=$TEST_IMAGE_VERSION"
    CNI_IMAGES_BUILD=true
fi

if [[ $(aws ecr batch-get-image --repository-name=$AWS_INIT_ECR_REPO_NAME --image-ids imageTag=$TEST_IMAGE_VERSION \
--query 'images[].imageId.imageTag' --region "$AWS_DEFAULT_REGION") != "[]" ]]; then
    echo "Image $AWS_INIT_ECR_REPO_NAME:$TEST_IMAGE_VERSION already exists. Skipping image build."
else
    echo "Image $AWS_INIT_ECR_REPO_NAME:$TEST_IMAGE_VERSION does not exist in repository."
    ## $1=command, $2=args
    build_and_push_image "multi-arch-cni-init-build-push" "INIT_IMAGE=$INIT_IMAGE_NAME VERSION=$TEST_IMAGE_VERSION"
    CNI_IMAGES_BUILD=true
fi

# cleanup if we make docker build and push images
if [[ "$CNI_IMAGES_BUILD" == true ]]; then
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

# Fetch VPC_ID from created cluster
if [[ "$RUN_KOPS_TEST" == true ]]; then
    INSTANCE_ID=$(kubectl get nodes -l node-role.kubernetes.io/node -o jsonpath='{range .items[*]}{@.metadata.name}{"\n"}' | head -1)
    VPC_ID=$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" | jq -r '.Reservations[].Instances[].VpcId' )
else
    DESCRIBE_CLUSTER_OP=$(aws eks describe-cluster --name "$CLUSTER_NAME" --region "$AWS_DEFAULT_REGION")
    VPC_ID=$(echo "$DESCRIBE_CLUSTER_OP" | jq -r '.cluster.resourcesVpcConfig.vpcId')
fi

echo "Using VPC_ID: $VPC_ID"

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

echo "*******************************************************************************"
echo "Updating CNI to image $IMAGE_NAME:$TEST_IMAGE_VERSION"
echo "Updating CNI-INIT to image $INIT_IMAGE_NAME:$TEST_IMAGE_VERSION"
START=$SECONDS
$KUBECTL_PATH apply -f "$TEST_CONFIG_PATH"
check_ds_rollout "aws-node" "kube-system" "10m"

CNI_IMAGE_UPDATE_DURATION=$((SECONDS - START))
echo "TIMELINE: Updating CNI image took $CNI_IMAGE_UPDATE_DURATION seconds."

TEST_PASS=0
if [[ $RUN_CNI_INTEGRATION_TESTS == true ]]; then
    echo "*******************************************************************************"
    echo "Running integration tests against current image"
    echo ""
    START=$SECONDS
    focus="CANARY"
    skip="STATIC_CANARY"
    echo "Running ginkgo tests with focus: $focus"
    (cd "$INTEGRATION_TEST_DIR/cni" && CGO_ENABLED=0 ginkgo --focus="$focus" --skip="$skip" -v --timeout 60m --no-color --fail-on-pending -- --cluster-kubeconfig="$KUBECONFIG" --cluster-name="$CLUSTER_NAME" --aws-region="$AWS_DEFAULT_REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
    (cd "$INTEGRATION_TEST_DIR/ipamd" && CGO_ENABLED=0 ginkgo --focus="$focus" -v --timeout 60m --no-color --fail-on-pending -- --cluster-kubeconfig="$KUBECONFIG" --cluster-name="$CLUSTER_NAME" --aws-region="$AWS_DEFAULT_REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
    (cd "$INTEGRATION_TEST_DIR/custom-networking-sgpp" && CGO_ENABLED=0 ginkgo -v --timeout 60m --no-color --fail-on-pending -- --cluster-kubeconfig="$KUBECONFIG" --cluster-name="$CLUSTER_NAME" --aws-region="$AWS_DEFAULT_REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
    TEST_PASS=$?
    CURRENT_IMAGE_INTEGRATION_DURATION=$((SECONDS - START))
    echo "TIMELINE: Current image integration tests took $CURRENT_IMAGE_INTEGRATION_DURATION seconds."
    if [[ $TEST_PASS -eq 0 ]]; then
      emit_cloudwatch_metric "integration_test_status" "1"
    fi
fi

if [[ $TEST_PASS -eq 0 && "$RUN_CONFORMANCE" == true ]]; then
  echo "Running conformance tests against cluster."
  START=$SECONDS

  GOPATH=$(go env GOPATH)
  echo "PATH: $PATH"

  echo "Downloading kubernetes test from: https://dl.k8s.io/v$K8S_VERSION/kubernetes-test-linux-amd64.tar.gz"
  wget -qO- https://dl.k8s.io/v$K8S_VERSION/kubernetes-test-linux-amd64.tar.gz | tar -zxvf - --strip-components=3 -C ${TEST_BASE_DIR}  kubernetes/test/bin/e2e.test

  echo "Running e2e tests: "
  ${TEST_BASE_DIR}/e2e.test --ginkgo.focus="\[Serial\].*Conformance" --kubeconfig=$KUBECONFIG --ginkgo.fail-fast --ginkgo.no-color --ginkgo.flake-attempts 2 \
    --ginkgo.skip="(should support remote command execution over websockets)|(should support retrieving logs from the container over websockets)|\[Slow\]"

  CONFORMANCE_DURATION=$((SECONDS - START))
  emit_cloudwatch_metric "conformance_test_status" "1"
  echo "TIMELINE: Conformance tests took $CONFORMANCE_DURATION seconds."
fi

if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
    echo "*******************************************************************************"
    echo "Running performance tests on current image"
    echo ""
    install_cw_agent
    START=$SECONDS
    run_performance_test 130
    run_performance_test 730
    run_performance_test 5000
    PERFORMANCE_DURATION=$((SECONDS - START))
    uninstall_cw_agent
fi

if [[ $RUN_KOPS_TEST == true ]]; then
    run_kops_conformance
fi

if [[ "$DEPROVISION" == true ]]; then
    START=$SECONDS

    if [[ "$RUN_KOPS_TEST" == true ]]; then
        down-kops-cluster
    elif [[ "$RUN_BOTTLEROCKET_TEST" == true ]]; then
        eksctl delete cluster $CLUSTER_NAME --disable-nodegroup-eviction
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
