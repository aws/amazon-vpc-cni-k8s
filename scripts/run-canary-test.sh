#!/bin/bash

# The script runs amazon-vpc-cni Canary tests on the default
# addon version and then runs smoke test on the latest addon version.

set -e

SECONDS=0
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
INTEGRATION_TEST_DIR="$SCRIPT_DIR/../test/integration-new"
VPC_CNI_ADDON_NAME="vpc-cni"

echo "Running Canary tests for amazon-vpc-cni-k8s with the following varialbes
KUBE_CONFIG_PATH:  $KUBE_CONFIG_PATH
CLUSTER_NAME: $CLUSTER_NAME
REGION: $REGION
ENDPOINT: $ENDPOINT"

if [[ -n "${ENDPOINT}" ]]; then
  ENDPOINT_FLAG="--endpoint $ENDPOINT"
fi

function load_cluster_details() {
  echo "loading cluster details $CLUSTER_NAME"
  DESCRIBE_CLUSTER_OP=$(aws eks describe-cluster --name "$CLUSTER_NAME" --region "$REGION" $ENDPOINT_FLAG)
  VPC_ID=$(echo "$DESCRIBE_CLUSTER_OP" | jq -r '.cluster.resourcesVpcConfig.vpcId')
  K8S_VERSION=$(echo "$DESCRIBE_CLUSTER_OP" | jq .cluster.version -r)
}

function load_addon_details() {
  echo "loading $VPC_CNI_ADDON_NAME addon details"
  DESCRIBE_ADDON_VERSIONS=$(aws eks describe-addon-versions --addon-name $VPC_CNI_ADDON_NAME --kubernetes-version "$K8S_VERSION")

  LATEST_ADDON_VERSION=$(echo "$DESCRIBE_ADDON_VERSIONS" | jq '.addons[0].addonVersions[0].addonVersion' -r)
  DEFAULT_ADDON_VERSION=$(echo "$DESCRIBE_ADDON_VERSIONS" | jq -r '.addons[].addonVersions[] | select(.compatibilities[0].defaultVersion == true) | .addonVersion')
}

function wait_for_addon_status() {
  local expected_status=$1

  if [ "$expected_status" =  "DELETED" ]; then
    while $(aws eks describe-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME); do
      echo "addon is still not deleted"
      sleep 5
    done
    echo "addon deleted"
    return
  fi

  while true
  do
    STATUS=$(aws eks describe-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME | jq -r '.addon.status')
    if [ "$STATUS" = "$expected_status" ]; then
      echo "addon status matches expected status"
      return
    fi
    echo "addon status is not euqal to $expected_status"
    sleep 5
  done
}

function install_add_on() {
  local new_addon_version=$1

  if DESCRIBE_ADDON=$(aws eks describe-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME); then
    local current_addon_version=$(echo "$DESCRIBE_ADDON" | jq '.addon.addonVersion' -r)
    if [ "$new_addon_version" != "$current_addon_version" ]; then
      echo "deleting the $current_addon_version to install $new_addon_version"
      aws eks delete-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name "$VPC_CNI_ADDON_NAME"
      wait_for_addon_status "DELETED"
    else
      echo "addon version $current_addon_version already installed"
      patch_aws_node_maxunavialable
      return
    fi
  fi

  echo "installing addon $new_addon_version"
  aws eks create-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME --resolve-conflicts OVERWRITE --addon-version $new_addon_version
  patch_aws_node_maxunavialable
  wait_for_addon_status "ACTIVE"
}

function patch_aws_node_maxunavialable() {
   # Patch the aws-node, so any update in aws-node happens parallely for faster overall test execution
   kubectl patch ds -n kube-system aws-node -p '{"spec":{"updateStrategy":{"rollingUpdate":{"maxUnavailable": "100%"}}}}'
}

function run_ginkgo_test() {
  local focus=$1
  echo "Running ginkgo tests with focus: $focus"
  (cd "$INTEGRATION_TEST_DIR/cni" && ginkgo --focus="$focus" -v --timeout 20m --failOnPending -- --cluster-kubeconfig="$KUBE_CONFIG_PATH" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
  (cd "$INTEGRATION_TEST_DIR/ipamd" && ginkgo --focus="$focus" -v --timeout 10m --failOnPending -- --cluster-kubeconfig="$KUBE_CONFIG_PATH" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
}

load_cluster_details
load_addon_details

# Run more comprehensive test on the default addon version. CANARY focused tests
# tests basic functionlity plus test that could detect issues with dependencies
# early on.
echo "Running Canary tests on the default addon version"
install_add_on "$DEFAULT_ADDON_VERSION"
run_ginkgo_test "CANARY"

# Run smoke test on the latest addon version. Smoke tests contains a subset of test
# used in Canary tests.
echo "Running Smoke tests on the latest addon version"
install_add_on "$LATEST_ADDON_VERSION"
run_ginkgo_test "SMOKE"

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"