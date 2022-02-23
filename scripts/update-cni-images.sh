#!/usr/bin/env bash

# script to set the image on aws-node daemonset for running tests

set -e

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
source "$SCRIPTS_DIR/lib/k8s.sh"

CONFIG_DIR="$SCRIPTS_DIR/../config"

echo "Applying config/master/aws-k8s-cni.yaml manifest to aws-node ds"
kubectl apply -f $CONFIG_DIR/master/aws-k8s-cni.yaml

echo "Setting aws-node ds for testing"
kubectl set image ds aws-node -n kube-system aws-node=$AMAZON_K8S_CNI aws-vpc-cni-init=$AMAZON_K8S_CNI_INIT

check_ds_rollout "aws-node" "kube-system"