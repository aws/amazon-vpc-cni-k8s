#!/bin/bash

# The script runs multus tests on the lastest version with latest AWS-VPC-CNI addon version

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
INTEGRATION_TEST_DIR="$SCRIPT_DIR/../test/integration"

source "$SCRIPT_DIR"/lib/set_kubeconfig.sh
source "$SCRIPT_DIR"/lib/common.sh
source "$SCRIPT_DIR"/lib/cluster.sh
source "$SCRIPT_DIR"/lib/canary.sh

function run_ginkgo_test() {
  local focus=$1
  (cd "$INTEGRATION_TEST_DIR/multus" && CGO_ENABLED=0 ginkgo --focus="$focus" -v --timeout 20m --fail-on-pending -- --cluster-kubeconfig="$KUBECONFIG" --cluster-name="$CLUSTER_NAME" --aws-region="$REGION" --aws-vpc-id="$VPC_ID" --ng-name-label-key="kubernetes.io/os" --ng-name-label-val="linux")
}

check_is_installed kubectl
check_is_installed ginkgo

load_cluster_details

LATEST_TAG=${1:-v4.1.4-eksbuild.1_thick}
echo "Installing latest multus manifest with tag: ${LATEST_TAG}"

kubectl apply -f https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/master/config/multus/${LATEST_TAG}/aws-k8s-multus.yaml

echo "Running multus ginkgo tests"
run_ginkgo_test

echo "all tests ran successfully in $(($SECONDS / 60)) minutes and $(($SECONDS % 60)) seconds"
