#!/bin/bash

# Runs the IPv6 integration test suite against a freshly built CNI image.
#
# Unlike run-ipv6-integration-tests.sh (which tests whatever the latest EKS-managed addon ships),
# this script installs the latest managed addon to get a correctly IPv6-configured aws-node, then
# REPLACES the aws-node and aws-node-init container images with the images built by the pipeline
# run. This way the test exercises the image under test on a real IPv6 dataplane.
#
# Required environment (provided by the calling test harness):
#   CLUSTER_NAME, REGION, KUBECONFIG (or KUBE_CONFIG_PATH), K8S_VERSION,
#   IMAGE_TAG, REPLICATION_STACK_ACCOUNT_NUMBER, URL_SUFFIX
# Optional:
#   ENDPOINT (beta EKS endpoint), TEST_IMAGE_REGISTRY, EXTRA_GINKGO_FLAGS

# Match the other CNI test scripts: `set -e` only (no `-u`), since the sourced lib helpers reference
# optional vars like ENDPOINT_FLAG / SA_ROLE_ARN_ARG unguarded and rely on empty expansion.
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
IPV6_TEST_DIR="$SCRIPT_DIR/../test/integration/ipv6"

# TEST_IMAGE_REGISTRY is the registry in test-infra-* accounts where e2e test images are stored.
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-"617930562442.dkr.ecr.us-west-2.amazonaws.com"}

source "$SCRIPT_DIR"/lib/set_kubeconfig.sh
source "$SCRIPT_DIR"/lib/add-on.sh
source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/k8s.sh

# ENDPOINT is optional (set only for beta EKS). Mirror run-cni-release-tests.sh: derive the
# --endpoint flag when provided; add-on.sh / cluster.sh reference ENDPOINT_FLAG unguarded.
if [[ -n "${ENDPOINT}" ]]; then
  ENDPOINT_FLAG="--endpoint $ENDPOINT"
fi

# ECR repo that holds the images built by this pipeline run. The build stage replicates the
# amazon-k8s-cni / amazon-k8s-cni-init images into the replication (addon distribution) account.
BUILD_IMAGE_REPO="${REPLICATION_STACK_ACCOUNT_NUMBER}.dkr.ecr.${REGION}.${URL_SUFFIX}"
AMAZON_K8S_CNI="${BUILD_IMAGE_REPO}/amazon-k8s-cni:${IMAGE_TAG}"
AMAZON_K8S_CNI_INIT="${BUILD_IMAGE_REPO}/amazon-k8s-cni-init:${IMAGE_TAG}"

function replace_cni_images() {
  echo "Replacing aws-node images with build images:"
  echo "  aws-node         -> ${AMAZON_K8S_CNI}"
  echo "  aws-vpc-cni-init -> ${AMAZON_K8S_CNI_INIT}"

  # Swap only the container images; all IPv6 env (ENABLE_IPV6=true, etc.) set by the managed addon
  # is preserved. Container names match the aws-node DaemonSet: main container "aws-node" and init
  # container "aws-vpc-cni-init" (see charts/aws-vpc-cni/templates/daemonset.yaml and
  # test/framework/utils/const.go AWSInitContainerName).
  kubectl set image daemonset/aws-node -n kube-system \
    aws-node="${AMAZON_K8S_CNI}" \
    aws-vpc-cni-init="${AMAZON_K8S_CNI_INIT}"

  echo "Waiting for aws-node daemonset rollout after image swap..."
  check_ds_rollout "aws-node" "kube-system" "10m"
}

# Assert the running aws-node DaemonSet is actually the build image. The managed addon is left in
# place (for its IPv6 config), and its reconciler could in principle revert the image swap; this
# guards against silently testing the shipped addon image instead of the build image. Called after
# the swap AND after the suite so a mid-run revert is caught rather than passing as a false green.
function verify_cni_images() {
  local phase=$1
  local running_cni running_init
  running_cni=$(kubectl get daemonset aws-node -n kube-system -o jsonpath='{.spec.template.spec.containers[?(@.name=="aws-node")].image}')
  running_init=$(kubectl get daemonset aws-node -n kube-system -o jsonpath='{.spec.template.spec.initContainers[?(@.name=="aws-vpc-cni-init")].image}')
  if [[ "$running_cni" != "$AMAZON_K8S_CNI" || "$running_init" != "$AMAZON_K8S_CNI_INIT" ]]; then
    echo "ERROR ($phase): aws-node is not running the build image (managed-addon reconciler may have reverted it)."
    echo "  aws-node         running=$running_cni expected=$AMAZON_K8S_CNI"
    echo "  aws-vpc-cni-init running=$running_init expected=$AMAZON_K8S_CNI_INIT"
    exit 1
  fi
  echo "verified aws-node is running the build image ($phase)"
}

# Compile and run the IPv6 suite from source (the run-cni-release-tests.sh pattern) rather than a
# prebuilt test/build/ipv6.test, since test/build/ is a gitignored artifact that a fresh clone
# never populates (nothing here runs `make build-test-binaries`). ginkgo compiles the suite
# in-place, so no separate build step is needed.
function run_ginkgo_test() {
  (cd "$IPV6_TEST_DIR" && CGO_ENABLED=0 ginkgo ${EXTRA_GINKGO_FLAGS:-} -v --timeout 30m --no-color --fail-on-pending -- \
    --cluster-kubeconfig="$KUBECONFIG" \
    --cluster-name="$CLUSTER_NAME" \
    --aws-region="$REGION" \
    --aws-vpc-id="$VPC_ID" \
    --ng-name-label-key="kubernetes.io/os" \
    --ng-name-label-val="linux" \
    --test-image-registry="$TEST_IMAGE_REGISTRY")
}

load_cluster_details
load_addon_details

echo "Installing latest managed addon ($LATEST_ADDON_VERSION) to obtain an IPv6-configured aws-node"
install_add_on "$LATEST_ADDON_VERSION"

echo "Swapping in the CNI image under test (tag: ${IMAGE_TAG})"
replace_cni_images
verify_cni_images "post-swap"

echo "Running IPv6 integration tests against the build image"
run_ginkgo_test

# Confirm the addon reconciler did not revert the image out from under the suite.
verify_cni_images "post-suite"

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
