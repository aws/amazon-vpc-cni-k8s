#!/bin/bash
#
# This script can be used to run any ginkgo integration suite using SUITE_NAME environment variable.
# Prerequisite: cluster with at least three Linux nodes must already exist.
# It is up to the caller to ensure that tests are valid to run against cluster, i.e. IPv6 tests require IPv6 cluster.
# Set appropriate optional args for running test suite

set -e

# Fallback to KUBE_CONFIG_PATH if KUBECONFIG is not set
if [ -n "$KUBE_CONFIG_PATH" ]; then
  export KUBECONFIG=$KUBE_CONFIG_PATH
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
GINKGO_TEST_BUILD="$SCRIPT_DIR/../test/build"
: "${SKIP_MAKE_TEST_BINARIES:=}"

source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/canary.sh

function load_test_parameters(){

  EXTRA_OPTIONS=""
  if [[ ! -z $INITIAL_ADDON_VERSION ]]; then
    EXTRA_OPTIONS+=" --initial-addon-version $INITIAL_ADDON_VERSION"
  fi

  if [[ ! -z $TARGET_ADDON_VERSION ]]; then
    EXTRA_OPTIONS+=" --target-addon-version $TARGET_ADDON_VERSION"
  fi

  if [[ ! -z $INITIAL_MANIFEST_FILE ]]; then
    EXTRA_OPTIONS+=" --initial-manifest-file $INITIAL_MANIFEST_FILE"
  fi

  if [[ ! -z $TARGET_MANIFEST_FILE ]]; then
    EXTRA_OPTIONS+=" --target-manifest-file $TARGET_MANIFEST_FILE"
  fi

}

function run_ginkgo_test() {
  (CGO_ENABLED=0 ginkgo $EXTRA_GINKGO_FLAGS -v --timeout 60m --no-color --fail-on-pending $GINKGO_TEST_BUILD/$SUITE_NAME.test -- \
    --cluster-kubeconfig="$KUBECONFIG" \
    --cluster-name="$CLUSTER_NAME" \
    --aws-region="$REGION" \
    --aws-vpc-id="$VPC_ID" \
    --ng-name-label-key="kubernetes.io/os" \
    --ng-name-label-val="linux" \
    $ENDPOINT_OPTION $EXTRA_OPTIONS
  )
}

load_cluster_details

load_test_parameters

echo "Running $SUITE_NAME integration tests"

run_ginkgo_test

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
