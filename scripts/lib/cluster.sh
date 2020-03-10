#!/usr/bin/env bash

function down-test-cluster() {
    if [[ -n "${CIRCLE_JOB:-}" ]]; then
        $TESTER_PATH eks delete cluster --path $CLUSTER_CONFIG || (echo "failed!" && exit 1)
    else
        echo -n "Deleting cluster $CLUSTER_NAME (this may take ~10 mins) ... "
        $TESTER_PATH eks delete cluster --path $CLUSTER_CONFIG >>$CLUSTER_MANAGE_LOG_PATH 2>&1 ||
            (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
        echo "ok."
    fi
}

function up-test-cluster() {
    echo -n "Configuring cluster $CLUSTER_NAME"
    AWS_K8S_TESTER_EKS_NAME=$CLUSTER_NAME \
        AWS_K8S_TESTER_EKS_KUBECONFIG_PATH=$KUBECONFIG_PATH \
        AWS_K8S_TESTER_EKS_PARAMETERS_VERSION=${K8S_VERSION%.*} \
        AWS_K8S_TESTER_EKS_PARAMETERS_ENCRYPTION_CMK_CREATE=false \
        AWS_K8S_TESTER_EKS_PARAMETERS_ROLE_CREATE=false \
        AWS_K8S_TESTER_EKS_PARAMETERS_ROLE_ARN=arn:aws:iam::404174646922:role/K8sTester-ClusterManagementRole \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_ENABLE=true \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_ROLE_CREATE=false \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_ROLE_ARN=arn:aws:iam::404174646922:role/K8sTester-ClusterManagementRole \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_MNGS={\"${CLUSTER_NAME}-mng-for-cni\":{\"name\":\"${CLUSTER_NAME}-mng-for-cni\",\"tags\":{\"group\":\"amazon-vpc-cni-k8s\"},\"ami-type\":\"AL2_x86_64\",\"asg-min-size\":3,\"asg-max-size\":3,\"asg-desired-capacity\":3,\"instance-types\":[\"c5.xlarge\"]}} \
        AWS_K8S_TESTER_EKS_ADD_ON_NLB_HELLO_WORLD_ENABLE=true \
        AWS_K8S_TESTER_EKS_ADD_ON_ALB_2048_ENABLE=true \
        AWS_K8S_TESTER_EKS_KUBECTL_PATH=$KUBECTL_PATH \
        $TESTER_PATH eks create config --path $CLUSTER_CONFIG 1>&2

    if [[ -n "${CIRCLE_JOB:-}" ]]; then
        $TESTER_PATH eks create cluster --path $CLUSTER_CONFIG || (echo "failed!" && exit 1)
    else
        echo -n "Creating cluster $CLUSTER_NAME (this may take ~20 mins. details: tail -f $CLUSTER_MANAGE_LOG_PATH)... "
        $TESTER_PATH eks create cluster --path $CLUSTER_CONFIG >>$CLUSTER_MANAGE_LOG_PATH 1>&2 ||
            (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
        echo "ok."
    fi
}
