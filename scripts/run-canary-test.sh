#!/bin/bash

# The script runs amazon-vpc-cni Canary tests on the default
# addon version and then runs smoke test on the latest addon version.

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
GINKGO_TEST_BUILD="$SCRIPT_DIR/../test/build"
# TEST_IMAGE_REGISTRY is the registry in test-infra-* accounts where e2e test images are stored
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-"617930562442.dkr.ecr.us-west-2.amazonaws.com"}
ADC_REGIONS="us-iso-east-1 us-isob-east-1 us-iso-west-1"

source "$SCRIPT_DIR"/lib/add-on.sh
source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/canary.sh

function run_ginkgo_test() {
  local focus=$1
  echo "Running ginkgo tests with focus: $focus"
  (CGO_ENABLED=0 ginkgo $EXTRA_GINKGO_FLAGS --no-color --focus="$focus" -v --timeout 30m --fail-on-pending $GINKGO_TEST_BUILD/cni.test -- --cluster-kubeconfig="$KUBECONFIG" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux" --test-image-registry=$TEST_IMAGE_REGISTRY)
  (CGO_ENABLED=0 ginkgo $EXTRA_GINKGO_FLAGS --no-color --focus="$focus" -v --timeout 30m --fail-on-pending $GINKGO_TEST_BUILD/ipamd.test -- --cluster-kubeconfig="$KUBECONFIG" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux" --test-image-registry=$TEST_IMAGE_REGISTRY)
}

load_cluster_details
load_addon_details

# Run more comprehensive test on the default addon version. CANARY focused tests
# cover basic functionlity plus test that could detect issues with dependencies
# early on.
echo "Running Canary tests on the default addon version"
install_add_on "$DEFAULT_ADDON_VERSION"
run_ginkgo_test "CANARY"

# Run smoke test on the latest addon version. Smoke tests consist of a subset of tests
# from Canary suite.
# skip the latest addon version for ADC regions
if [[ $ADC_REGIONS == *"$REGION"* ]]; then
  echo "Skipping Smoke tests on the latest addon version"
else
  echo "Running Smoke tests on the latest addon version"
  install_add_on "$LATEST_ADDON_VERSION"
  run_ginkgo_test "SMOKE"
fi

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
