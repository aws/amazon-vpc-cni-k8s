#!/bin/bash

# Helper script used for running canary test for CNI IPv4 and IPv6

SECONDS=0
VPC_CNI_ADDON_NAME="vpc-cni"

echo "Running Canary tests for amazon-vpc-cni-k8s with the following variables
KUBE_CONFIG_PATH:  $KUBE_CONFIG_PATH
CLUSTER_NAME: $CLUSTER_NAME
REGION: $REGION
ENDPOINT: $ENDPOINT"

if [[ -n "${ENDPOINT}" ]]; then
  ENDPOINT_FLAG="--endpoint $ENDPOINT"
fi

# Request timesout in China Regions with default proxy
if [[ $REGION == "cn-north-1" || $REGION == "cn-northwest-1" ]]; then
  go env -w GOPROXY=https://goproxy.cn,direct
  go env -w GOSUMDB=sum.golang.google.cn
fi
