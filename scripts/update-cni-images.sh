#!/usr/bin/env bash

# script to set the image on aws-node daemonset for running tests

# Usage: Set test images as ENV variables $AMAZON_K8S_CNI & $AMAZON_K8S_CNI_INIT
# Run script to update aws-node daemonset images

set -e

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
source "$SCRIPTS_DIR/lib/k8s.sh"

AWS_K8S_CNI_MANIFEST="$SCRIPTS_DIR/../config/master/aws-k8s-cni.yaml"
MANIFEST_IMG_VERSION=`grep "image:" $AWS_K8S_CNI_MANIFEST | cut -d ":" -f3 | cut -d "\"" -f1 | head -1`

# Replace the images in aws-k8s-cni.yaml with the tester images when environment variables are set
if [[ -z $AWS_K8S_CNI ]]; then
    echo "Applying latest CNI image from aws-k8s-cni manifest"
else
    echo "Replacing CNI image in aws-k8s-cni manifest with $AMAZON_K8S_CNI"
    sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni:$MANIFEST_IMG_VERSION,$AMAZON_K8S_CNI," "$AWS_K8S_CNI_MANIFEST"
fi
if [[ -z $AWS_K8S_CNI_INIT ]]; then
    echo "Applying latest CNI init image from aws-k8s-cni manifest"
else
    echo "Replacing CNI image in aws-k8s-cni manifest with $AMAZON_K8S_CNI"
    sed -i'.bak' "s,602401143452.dkr.ecr.us-west-2.amazonaws.com/amazon-k8s-cni-init:$MANIFEST_IMG_VERSION,$AMAZON_K8S_CNI_INIT," "$AWS_K8S_CNI_MANIFEST"
fi

echo "Applying aws-k8s-cni.yaml manifest to aws-node daemonset"
kubectl apply -f $AWS_K8S_CNI_MANIFEST

check_ds_rollout "aws-node" "kube-system" "4m"
