#!/bin/bash

# The script runs amazon-vpc-cni Canary tests on the default
# addon version and then runs smoke test on the latest addon version.

set -e

source lib/add-on.sh
source lib/cluster.sh
source lib/canary.sh

function run_ginkgo_test() {
  local focus=$1
  echo "Running ginkgo tests with focus: $focus"
  (cd "$INTEGRATION_TEST_DIR/cni" && CGO_ENABLED=0 ginkgo --focus="$focus" -v --timeout 20m --failOnPending -- --cluster-kubeconfig="$KUBE_CONFIG_PATH" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
  (cd "$INTEGRATION_TEST_DIR/ipamd" && CGO_ENABLED=0 ginkgo --focus="$focus" -v --timeout 10m --failOnPending -- --cluster-kubeconfig="$KUBE_CONFIG_PATH" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
}

load_cluster_details
load_addon_details

# TODO: v1.7.5 restarts continiously if IMDS goes out of sync, the issue is mitigated
# from v.1.8.0 onwards, once the default addon is updated to v1.8.0+ we should uncomment
# the following code. See: https://github.com/aws/amazon-vpc-cni-k8s/issues/1340

# Run more comprehensive test on the default addon version. CANARY focused tests
# tests basic functionlity plus test that could detect issues with dependencies
# early on.
#echo "Running Canary tests on the default addon version"
#install_add_on "$DEFAULT_ADDON_VERSION"
#run_ginkgo_test "CANARY"

# Run smoke test on the latest addon version. Smoke tests contains a subset of test
# used in Canary tests.
#echo "Running Smoke tests on the latest addon version"
#install_add_on "$LATEST_ADDON_VERSION"
#run_ginkgo_test "SMOKE"

# TODO: Remove the following code once the v1.8.0+ is made the default addon version
echo "Running Canary tests on the latest addon version"
install_add_on "$LATEST_ADDON_VERSION"
run_ginkgo_test "CANARY"

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
