#!/bin/bash

# The script runs amazon-vpc-cni static canary tests
# The tests in this suite are designed to exercise AZ failure scenarios.

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
GINKGO_TEST_BUILD="$SCRIPT_DIR/../test/build"
# TEST_IMAGE_REGISTRY is the registry in test-infra-* accounts where e2e test images are stored
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-"617930562442.dkr.ecr.us-west-2.amazonaws.com"}

# If $ENDPOINT is set, as in it is for beta clusters then $ENDPOINT_OPTION,
# defined in lib/cluster.sh will add --eks-endpoint=$ENDPOINT to the ginkgo
# test command

source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/canary.sh

function run_ginkgo_test() {
  local focus=$1
  echo "Running ginkgo tests with focus: $focus"

  (CGO_ENABLED=0 ginkgo $EXTRA_GINKGO_FLAGS --no-color --focus="$focus" -v --timeout 3h --fail-on-pending $GINKGO_TEST_BUILD/cni.test -- \
      --cluster-kubeconfig="$KUBECONFIG" \
      --cluster-name="$CLUSTER_NAME" \
      --aws-region="$REGION" \
      --aws-vpc-id="$VPC_ID" \
      --ng-name-label-key="kubernetes.io/os" \
      --ng-name-label-val="linux" \
      --test-image-registry=$TEST_IMAGE_REGISTRY \
      --publish-cw-metrics=true \
      $ENDPOINT_OPTION)
}

load_cluster_details

run_ginkgo_test "SOAK_TEST"

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
