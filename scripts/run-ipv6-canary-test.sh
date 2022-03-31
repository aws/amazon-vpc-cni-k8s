#!/bin/bash

# The script runs amazon-vpc-cni Canary tests on the default
# addon version and then runs smoke test on the latest addon version.

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
INTEGRATION_TEST_DIR="$SCRIPT_DIR/../test/integration-new"

source "$SCRIPT_DIR"/lib/add-on.sh
source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/canary.sh

function run_ginkgo_test() {
  local focus=$1
  echo "Running ginkgo tests with focus: $focus"
  (cd "$INTEGRATION_TEST_DIR/ipv6" && CGO_ENABLED=0 ginkgo $EXTRA_GINKO_FLAGS --focus="$focus" -v --timeout 15m --failOnPending -- --cluster-kubeconfig="$KUBE_CONFIG_PATH" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
}

load_cluster_details
load_addon_details

echo "Running IPv6 Canary tests on the latest addon version"

install_add_on "$LATEST_ADDON_VERSION"
run_ginkgo_test "CANARY"

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
