#!/bin/bash

set -e
OS_OVERRIDE=$(go env GOOS)
pushd ./test/integration-new

echo "Running integration test with the following configuration:
KUBE CONFIG: $KUBECONFIG
CLUSTER_NAME: $CLUSTER_NAME
AWS_REGION: $AWS_REGION
OS_OVERRIDE: $OS_OVERRIDE"

while getopts "f:" arg; do
  case $arg in
    f) FOCUS=$OPTARG;
    echo "FOCUS: $FOCUS";
  esac
done
    

if [[ -z "${OS_OVERRIDE}" ]]; then
    OS_OVERRIDE=linux
fi

CLUSTER_INFO=$(aws eks describe-cluster --name $CLUSTER_NAME --region $AWS_REGION)

VPC_ID=$(echo $CLUSTER_INFO | jq -r '.cluster.resourcesVpcConfig.vpcId')
SERVICE_ROLE_ARN=$(echo $CLUSTER_INFO | jq -r '.cluster.roleArn')
ROLE_NAME=${SERVICE_ROLE_ARN##*/}
 


START=$SECONDS

for dir in */  
do

    cd "${dir%*/}"  
    if [ -n "$FOCUS" ]; then
        ginkgo --focus="$FOCUS" -v  -- --cluster-kubeconfig=$KUBECONFIG --cluster-name=$CLUSTER_NAME --aws-region=$AWS_REGION --aws-vpc-id=$VPC_ID
    else
        ginkgo -v  -- --cluster-kubeconfig=$KUBECONFIG --cluster-name=$CLUSTER_NAME --aws-region=$AWS_REGION --aws-vpc-id=$VPC_ID
    fi
    cd ..    
     
done


DEFAULT_INTEGRATION_DURATION=$((SECONDS - START))
echo "TIMELINE: Integration tests took $DEFAULT_INTEGRATION_DURATION seconds."

popd