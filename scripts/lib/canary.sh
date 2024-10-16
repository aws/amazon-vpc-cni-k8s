#!/bin/bash

# Helper script used for running canary test for CNI IPv4 and IPv6

SECONDS=0

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
source "$SCRIPT_DIR"/set_kubeconfig.sh


echo "Running tests for amazon-vpc-cni-k8s with the following variables
KUBECONFIG:  $KUBECONFIG
CLUSTER_NAME: $CLUSTER_NAME
REGION: $REGION
ENDPOINT: $ENDPOINT"

if [[ -n "${ENDPOINT}" ]]; then
  ENDPOINT_FLAG="--endpoint $ENDPOINT"
  ENDPOINT_OPTION=" --eks-endpoint $ENDPOINT"
fi

if [[ -z "${SKIP_MAKE_TEST_BINARIES}" ]]; then
  echo "making ginkgo test binaries"
  (cd $SCRIPT_DIR/../ && make build-test-binaries)
else
  echo "skipping making ginkgo test binaries"
fi

# Request times out in China Regions with default proxy
if [[ $REGION == "cn-north-1" || $REGION == "cn-northwest-1" ]]; then
  go env -w GOPROXY=https://goproxy.cn,direct
  go env -w GOSUMDB=sum.golang.google.cn
fi
