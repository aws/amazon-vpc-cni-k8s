#!/bin/bash

# The script runs amazon-vpc-cni IPv6 canary tests

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
GINKGO_TEST_BUILD="$SCRIPT_DIR/../test/build"

source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/canary.sh

function run_ginkgo_test() {
  local focus=$1
  echo "Running ginkgo tests with focus: $focus"
  (CGO_ENABLED=0 ginkgo $EXTRA_GINKGO_FLAGS --focus="$focus" -v --timeout 15m --fail-on-pending $GINKGO_TEST_BUILD/ipv6.test -- --cluster-kubeconfig="$KUBE_CONFIG_PATH" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
}

load_cluster_details

echo "Running IPv6 Canary tests"

run_ginkgo_test "CANARY"

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
