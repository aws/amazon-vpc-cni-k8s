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
    ssh-keygen -q -P cni-test -f $SSH_KEY_PATH
    AWS_K8S_TESTER_EKS_CLUSTER_NAME=$CLUSTER_NAME \
        AWS_K8S_TESTER_EKS_KUBECONFIG_PATH=$KUBECONFIG_PATH \
        AWS_K8S_TESTER_EKS_KUBERNETES_VERSION=${K8S_VERSION%.*} \
        AWS_K8S_TESTER_EKS_ENABLE_MANAGED_NODE_GROUP_PRIVILEGED_PORT_ACCESS=true \
        AWS_K8S_TESTER_EKS_MANAGED_NODE_GROUP_ASG_MIN=3 \
        AWS_K8S_TESTER_EKS_MANAGED_NODE_GROUP_ASG_MAX=3 \
        AWS_K8S_TESTER_EKS_MANAGED_NODE_GROUP_ASG_DESIRED_CAPACITY=3 \
        AWS_K8S_TESTER_EKS_MANAGED_NODE_GROUP_REMOTE_ACCESS_PRIVATE_KEY_PATH=$SSH_KEY_PATH \
        AWS_K8S_TESTER_EKS_MANAGED_NODE_GROUP_INSTANCE_TYPES=m3.xlarge \
        AWS_K8S_TESTER_EKS_AWS_K8S_TESTER_PATH=$TESTER_PATH \
        AWS_K8S_TESTER_EKS_AWS_IAM_AUTHENTICATOR_PATH=$AUTHENTICATOR_PATH \
        AWS_K8S_TESTER_EKS_ADD_ON_JOB_ECHO_ENABLE=true \
        AWS_K8S_TESTER_EKS_ADD_ON_JOB_ECHO_PARALLELS=3 \
        AWS_K8S_TESTER_EKS_ADD_ON_JOB_ECHO_COMPLETES=30 \
        AWS_K8S_TESTER_EKS_ADD_ON_JOB_PERL_ENABLE=true \
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
