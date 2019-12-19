#!/usr/bin/env bash

function down-test-cluster() {
    echo -n "Deleting cluster $CLUSTER_NAME (this may take ~10 mins) ... "
    $TESTER_PATH eks delete cluster --path $CLUSTER_CONFIG >>$CLUSTER_MANAGE_LOG_PATH 2>&1 ||
        ( echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1 )
    echo "ok."
}

function up-test-cluster() {
    ssh-keygen -q -P cni-test -f $SSH_KEY_PATH

    AWS_K8S_TESTER_EKS_CLUSTER_NAME=$CLUSTER_NAME \
        AWS_K8S_TESTER_EKS_KUBECONFIG_PATH=$KUBECONFIG_PATH \
        AWS_K8S_TESTER_EKS_KUBERNETES_VERSION=${K8S_VERSION%.*} \
        AWS_K8S_TESTER_EKS_ENABLE_WORKER_NODE_PRIVILEGED_PORT_ACCESS=true \
        AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MIN=3 \
        AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MAX=3 \
        AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_DESIRED_CAPACITY=3 \
        AWS_K8S_TESTER_EKS_WORKER_NODE_PRIVATE_KEY_PATH=$SSH_KEY_PATH \
        AWS_K8S_TESTER_EKS_WORKER_NODE_INSTANCE_TYPE=m3.xlarge \
        AWS_K8S_TESTER_EKS_AWS_K8S_TESTER_PATH=$TESTER_PATH \
        AWS_K8S_TESTER_EKS_AWS_IAM_AUTHENTICATOR_PATH=$AUTHENTICATOR_PATH \
        AWS_K8S_TESTER_EKS_KUBECTL_PATH=$KUBECTL_PATH \
    $TESTER_PATH eks create config --path $CLUSTER_CONFIG 1>&2
    echo -n "Creating cluster $CLUSTER_NAME (this may take ~20 mins. details: tail -f $CLUSTER_MANAGE_LOG_PATH)... "
    $TESTER_PATH eks create cluster --path $CLUSTER_CONFIG 2>>$CLUSTER_MANAGE_LOG_PATH 1>&2 ||
        ( echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1 )
    echo "ok."
}
