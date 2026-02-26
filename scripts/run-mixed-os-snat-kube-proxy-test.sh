#!/usr/bin/env bash

# Creates an EKS cluster with mixed OS node groups AL2, AL2023, Bottlerocket, Ubuntu  and runs SNAT/kube-proxy tests on each.

set -Euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
INTEGRATION_TEST_DIR="$SCRIPT_DIR/../test/integration"

# Defaults
: "${AWS_DEFAULT_REGION:=us-west-2}"
: "${CLUSTER_NAME:=cni-mixed-os-$(date +%s)}"
: "${K8S_VERSION:=1.31}"
: "${IP_FAMILY:=IPv4}"
: "${NODES_PER_OS:=2}"
: "${INSTANCE_TYPE:=m5.large}"
: "${PROVISION:=true}"
: "${DEPROVISION:=true}"
: "${KUBECONFIG:=}"
: "${CNI_IMAGE:=}"

OS_TYPES=("al2" "al2023" "ubuntu" "bottlerocket")

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Creates EKS cluster with mixed OS nodes and runs SNAT/kube-proxy integration tests.

Options:
    -n, --name NAME         Cluster name (default: cni-mixed-os-RANDOM)
    -v, --version VERSION   Kubernetes version (default: 1.31)
    -f, --ip-family FAMILY  IPv4 or IPv6 (default: IPv4)
    -r, --region REGION     AWS region (default: us-west-2)
    --no-provision          Skip cluster creation (use existing)
    --no-deprovision        Skip cluster deletion after tests
    -k, --kubeconfig PATH   Kubeconfig for existing cluster
    -h, --help              Show this help

Example:
    $0 --name my-test --version 1.31 --ip-family IPv4
    $0 --kubeconfig ~/.kube/config --no-provision --no-deprovision
EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--name) CLUSTER_NAME="$2"; shift 2 ;;
        -v|--version) K8S_VERSION="$2"; shift 2 ;;
        -f|--ip-family) IP_FAMILY="$2"; shift 2 ;;
        -r|--region) AWS_DEFAULT_REGION="$2"; shift 2 ;;
        --no-provision) PROVISION=false; shift ;;
        --no-deprovision) DEPROVISION=false; shift ;;
        -k|--kubeconfig) KUBECONFIG="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) echo "Unknown option: $1"; usage ;;
    esac
done

CLUSTER_CONFIG="/tmp/${CLUSTER_NAME}-config.yaml"
: "${KUBECONFIG:=/tmp/${CLUSTER_NAME}-kubeconfig}"


verify_cluster_context() {
    local current_context
    current_context=$(kubectl config current-context 2>/dev/null || echo "")
    
    if [[ -z "$current_context" ]]; then
        echo "ERROR: No kubectl context set"
        exit 1
    fi
    
    if [[ "$current_context" != *"$CLUSTER_NAME"* ]]; then
        echo "ERROR: Current context '$current_context' does not match cluster '$CLUSTER_NAME'"
        exit 1
    fi
    echo "Verified kubectl context: $current_context"
}

update_cni_image() {
    if [[ -z "$CNI_IMAGE" ]]; then
        echo "No CNI_IMAGE specified, using existing image"
        return
    fi
    
    echo "Updating aws-node daemonset with image: $CNI_IMAGE"
    kubectl set image daemonset/aws-node -n kube-system aws-node="$CNI_IMAGE"
    echo "Waiting for aws-node rollout..."
    kubectl rollout status daemonset/aws-node -n kube-system --timeout=5m
    echo "CNI image updated successfully"
}

generate_cluster_config() {
    cat > "$CLUSTER_CONFIG" <<EOF
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: ${CLUSTER_NAME}
  region: ${AWS_DEFAULT_REGION}
  version: "${K8S_VERSION}"

kubernetesNetworkConfig:
  ipFamily: ${IP_FAMILY}

iam:
  withOIDC: true

availabilityZones:
  - ${AWS_DEFAULT_REGION}a
  - ${AWS_DEFAULT_REGION}b

managedNodeGroups:
  - name: al2-nodes
    instanceType: ${INSTANCE_TYPE}
    desiredCapacity: ${NODES_PER_OS}
    amiFamily: AmazonLinux2
    labels:
      os-type: al2

  - name: al2023-nodes
    instanceType: ${INSTANCE_TYPE}
    desiredCapacity: ${NODES_PER_OS}
    amiFamily: AmazonLinux2023
    labels:
      os-type: al2023

  - name: ubuntu-nodes
    instanceType: ${INSTANCE_TYPE}
    desiredCapacity: ${NODES_PER_OS}
    amiFamily: Ubuntu2204
    labels:
      os-type: ubuntu

nodeGroups:
  - name: bottlerocket-nodes
    instanceType: ${INSTANCE_TYPE}
    desiredCapacity: ${NODES_PER_OS}
    amiFamily: Bottlerocket
    labels:
      os-type: bottlerocket
    iam:
      attachPolicyARNs:
        - arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy
        - arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy
        - arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly
        - arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
EOF
}

create_cluster() {
    echo "Creating cluster $CLUSTER_NAME..."
    generate_cluster_config
    eksctl create cluster -f "$CLUSTER_CONFIG" --kubeconfig "$KUBECONFIG"
    echo "Cluster created."
}

delete_cluster() {
    echo "Deleting cluster $CLUSTER_NAME..."
    eksctl delete cluster --name "$CLUSTER_NAME" --region "$AWS_DEFAULT_REGION" --wait
}

get_vpc_id() {
    aws eks describe-cluster --name "$CLUSTER_NAME" --region "$AWS_DEFAULT_REGION" \
        --query 'cluster.resourcesVpcConfig.vpcId' --output text
}

run_tests_for_os() {
    local os_type=$1
    echo ""
    echo "========================================"
    echo "Running SNAT tests on OS: $os_type"
    echo "========================================"
    
    cd "$INTEGRATION_TEST_DIR/cni"
    CGO_ENABLED=0 ginkgo \
        --focus="test SNAT with kube-proxy modes" \
        -v --timeout 60m --no-color \
        -- \
        --cluster-kubeconfig="$KUBECONFIG" \
        --cluster-name="$CLUSTER_NAME" \
        --aws-region="$AWS_DEFAULT_REGION" \
        --aws-vpc-id="$VPC_ID" \
        --ng-name-label-key="os-type" \
        --ng-name-label-val="$os_type"
    
    local result=$?
    if [[ $result -eq 0 ]]; then
        echo "✓ Tests PASSED for $os_type"
    else
        echo "✗ Tests FAILED for $os_type"
        FAILED_OS+=("$os_type")
    fi
    return $result
}

# Main
FAILED_OS=()

if [[ "$PROVISION" == true ]]; then
    create_cluster
fi

export KUBECONFIG

verify_cluster_context
update_cni_image

VPC_ID=$(get_vpc_id)
echo "Using VPC: $VPC_ID"

# Run tests on each OS type
for os in "${OS_TYPES[@]}"; do
    run_tests_for_os "$os" || true
done

# Summary
echo ""
echo "========================================"
echo "Test Summary"
echo "========================================"
if [[ ${#FAILED_OS[@]} -eq 0 ]]; then
    echo "All OS types PASSED"
else
    echo "FAILED OS types: ${FAILED_OS[*]}"
fi

if [[ "$DEPROVISION" == true ]]; then
    delete_cluster
fi

[[ ${#FAILED_OS[@]} -eq 0 ]]
