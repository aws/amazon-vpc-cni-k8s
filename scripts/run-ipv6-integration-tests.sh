#!/bin/bash

# The script runs the full IPv6 integration test suite

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
GINKGO_TEST_BUILD="$SCRIPT_DIR/../test/build"

# TEST_IMAGE_REGISTRY is the registry in test-infra-* accounts where e2e test images are stored
TEST_IMAGE_REGISTRY=${TEST_IMAGE_REGISTRY:-"617930562442.dkr.ecr.us-west-2.amazonaws.com"}

source "$SCRIPT_DIR"/lib/set_kubeconfig.sh
source "$SCRIPT_DIR"/lib/add-on.sh
source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/canary.sh

function run_ginkgo_test() {
  (CGO_ENABLED=0 ginkgo $EXTRA_GINKGO_FLAGS -v --timeout 30m --no-color --fail-on-pending $GINKGO_TEST_BUILD/ipv6.test -- --cluster-kubeconfig="$KUBECONFIG" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux" --test-image-registry=$TEST_IMAGE_REGISTRY)
}

load_cluster_details
load_addon_details

echo "Running IPv6 integration tests against the latest addon version"

install_add_on "$LATEST_ADDON_VERSION"
run_ginkgo_test

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
