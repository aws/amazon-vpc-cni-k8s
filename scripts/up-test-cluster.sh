#!/usr/bin/env bash

function up-test-cluster() {
    echo "Creating cluster $CLUSTER_NAME"
    CLUSTER_YAML_PATH=$TEST_DIR/$CLUSTER_NAME.yaml
    SSH_KEY_PATH=$TEST_DIR/id_rsa
    ssh-keygen -P cni-test -f $SSH_KEY_PATH


    $TESTER_PATH eks create config --path /tmp/aws-k8s-tester-eks.yaml

    AWS_K8S_TESTER_EKS_KUBERNETES_VERSION=$K8S_VERSION \
        AWS_K8S_TESTER_EKS_ENABLE_WORKER_NODE_PRIVILEGED_PORT_ACCESS=true \
        AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MIN=3 \
        AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MAX=3 \
        AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_DESIRED_CAPACITY=3 \
        AWS_K8S_TESTER_EKS_WORKER_NODE_PRIVATE_KEY_PATH=$SSH_KEY_PATH \
        AWS_K8S_TESTER_EKS_WORKER_NODE_INSTANCE_TYPE=m3.xlarge \
        $TESTER_PATH eks create cluster --path /tmp/aws-k8s-tester-eks.yaml

    # Wait for cluster creation
    while [[ 1 ]]; do
        $TESTER_PATH eks check cluster --path /tmp/aws-k8s-tester-eks.yaml
        ret=$?
        if [[ $ret -eq 0 ]]; then
            break
        else
            echo "Waiting cluster to be created"
            sleep 30
        fi
    done;
}