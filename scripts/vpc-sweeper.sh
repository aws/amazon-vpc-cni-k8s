#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
source "$SCRIPT_DIR"/lib/common.sh

: "${AWS_DEFAULT_REGION:=us-west-2}"
: "${DRY_RUN:=true}"
: "${MAX_AGE_HOURS:=168}"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

cleanup_test_vpcs() {
    local cutoff_time=$(date -d "${MAX_AGE_HOURS} hours ago" -u +"%Y-%m-%dT%H:%M:%S")
    
    log "Looking for test VPCs older than ${MAX_AGE_HOURS} hours (before ${cutoff_time})"
    
    # Find VPCs with test cluster tags
    local vpcs=$(aws ec2 describe-vpcs \
        --region "$AWS_DEFAULT_REGION" \
        --filters "Name=tag:kubernetes.io/cluster/cni-test-*,Values=owned" \
        --query "Vpcs[?CreationTime<'${cutoff_time}'].{VpcId:VpcId,CreationTime:CreationTime,Tags:Tags}" \
        --output json)
    
    if [[ $(echo "$vpcs" | jq length) -eq 0 ]]; then
        log "No orphaned test VPCs found"
        return
    fi
    
    echo "$vpcs" | jq -r '.[] | "\(.VpcId) \(.CreationTime)"' | while read vpc_id creation_time; do
        log "Found orphaned VPC: $vpc_id (created: $creation_time)"
        
        if [[ "$DRY_RUN" == "true" ]]; then
            log "DRY_RUN: Would delete VPC $vpc_id and associated resources"
        else
            delete_vpc_resources "$vpc_id"
        fi
    done
}

delete_vpc_resources() {
    local vpc_id="$1"
    log "Deleting resources for VPC: $vpc_id"
    
    # Delete EKS clusters in this VPC
    local clusters=$(aws eks list-clusters --region "$AWS_DEFAULT_REGION" --query "clusters[?starts_with(@, 'cni-test-')]" --output text)
    for cluster in $clusters; do
        local cluster_vpc=$(aws eks describe-cluster --name "$cluster" --region "$AWS_DEFAULT_REGION" --query "cluster.resourcesVpcConfig.vpcId" --output text 2>/dev/null || echo "")
        if [[ "$cluster_vpc" == "$vpc_id" ]]; then
            log "Deleting EKS cluster: $cluster"
            eksctl delete cluster "$cluster" --region "$AWS_DEFAULT_REGION" --wait || true
        fi
    done
    
    # Delete NAT Gateways
    aws ec2 describe-nat-gateways --region "$AWS_DEFAULT_REGION" --filter "Name=vpc-id,Values=$vpc_id" --query "NatGateways[].NatGatewayId" --output text | xargs -r -n1 aws ec2 delete-nat-gateway --region "$AWS_DEFAULT_REGION" --nat-gateway-id || true
    
    # Delete Internet Gateways
    aws ec2 describe-internet-gateways --region "$AWS_DEFAULT_REGION" --filters "Name=attachment.vpc-id,Values=$vpc_id" --query "InternetGateways[].InternetGatewayId" --output text | xargs -r -n1 -I {} sh -c 'aws ec2 detach-internet-gateway --region "$AWS_DEFAULT_REGION" --internet-gateway-id {} --vpc-id "$1" && aws ec2 delete-internet-gateway --region "$AWS_DEFAULT_REGION" --internet-gateway-id {}' -- "$vpc_id" || true
    
    # Delete subnets
    aws ec2 describe-subnets --region "$AWS_DEFAULT_REGION" --filters "Name=vpc-id,Values=$vpc_id" --query "Subnets[].SubnetId" --output text | xargs -r -n1 aws ec2 delete-subnet --region "$AWS_DEFAULT_REGION" --subnet-id || true
    
    # Delete security groups (except default)
    aws ec2 describe-security-groups --region "$AWS_DEFAULT_REGION" --filters "Name=vpc-id,Values=$vpc_id" --query "SecurityGroups[?GroupName!='default'].GroupId" --output text | xargs -r -n1 aws ec2 delete-security-group --region "$AWS_DEFAULT_REGION" --group-id || true
    
    # Delete VPC
    aws ec2 delete-vpc --region "$AWS_DEFAULT_REGION" --vpc-id "$vpc_id" || true
    log "Deleted VPC: $vpc_id"
}

main() {
    log "Starting VPC sweeper (DRY_RUN=$DRY_RUN, MAX_AGE_HOURS=$MAX_AGE_HOURS)"
    
    check_is_installed aws
    check_is_installed jq
    check_is_installed eksctl
    
    cleanup_test_vpcs
    
    log "VPC sweeper completed"
}

main "$@"
