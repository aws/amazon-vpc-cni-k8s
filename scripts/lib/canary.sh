#!/bin/bash

# Initialize variables needed for running ginkgo tests 

SECONDS=0
VPC_CNI_ADDON_NAME="vpc-cni"

echo "Running tests with the following variables
KUBE_CONFIG_PATH:  $KUBE_CONFIG_PATH
CLUSTER_NAME: $CLUSTER_NAME
REGION: $REGION
ENDPOINT: $ENDPOINT"

if [[ ! -n "${KUBE_CONFIG_PATH}" ]]; then
  echo "KUBE_CONFIG_PATH not set"
  exit 1
fi

if [[ ! -n "${CLUSTER_NAME}" ]]; then
  echo "CLUSTER_NAME not set"
  exit 1
fi

if [[ ! -n "${REGION}" ]]; then
  echo "REGION not set"
  exit 1
fi

if [[ -n "${ENDPOINT}" ]]; then
  ENDPOINT_FLAG="--endpoint $ENDPOINT"
fi

if [[ -z "${SKIP_MAKE_TEST_BINARIES}" ]]; then
  echo "making ginkgo test binaries"
  (cd $SCRIPT_DIR/.. && make build-test-binaries)
else
  echo "skipping making ginkgo test binaries"
fi

# Request timesout in China Regions with default proxy
if [[ $REGION == "cn-north-1" || $REGION == "cn-northwest-1" ]]; then
  go env -w GOPROXY=https://goproxy.cn,direct
  go env -w GOSUMDB=sum.golang.google.cn
fi
