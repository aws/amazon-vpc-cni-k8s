#!/usr/bin/env bash

function down-test-cluster() {
    if [[ -n "${CIRCLE_JOB:-}" || -n "${DISABLE_PROMPT:-}" ]]; then
        $TESTER_PATH eks delete cluster --enable-prompt=false --path $CLUSTER_CONFIG || (echo "failed!" && exit 1)
    else
        echo -n "Deleting cluster $CLUSTER_NAME (this may take ~10 mins) ... "
        $TESTER_PATH eks delete cluster --enable-prompt=false --path $CLUSTER_CONFIG >>$CLUSTER_MANAGE_LOG_PATH 2>&1 ||
            (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
        echo "ok."
    fi
}

function up-test-cluster() {
    MNGS=""
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        MNGS='{"GetRef.Name-single-node-mng":{"name":"GetRef.Name-single-node-mng","remote-access-user-name":"ec2-user","tags":{"group":"amazon-vpc-cni-k8s"},"release-version":"","ami-type":"AL2_x86_64","asg-min-size":1,"asg-max-size":1,"asg-desired-capacity":1,"instance-types":["m5.16xlarge"],"volume-size":40}, "cni-test-multi-node-mng":{"name":"cni-test-multi-node-mng","remote-access-user-name":"ec2-user","tags":{"group":"amazon-vpc-cni-k8s"},"release-version":"","ami-type":"AL2_x86_64","asg-min-size":1,"asg-max-size":100,"asg-desired-capacity":99,"instance-types":["m5.xlarge"],"volume-size":40,"cluster-autoscaler":{"enable":true}}}'
        export RUN_CONFORMANCE="false"
        : "${PERFORMANCE_TEST_S3_BUCKET_NAME:=""}"
    else
        MNGS='{"GetRef.Name-mng-for-cni":{"name":"GetRef.Name-mng-for-cni","remote-access-user-name":"ec2-user","tags":{"group":"amazon-vpc-cni-k8s"},"release-version":"","ami-type":"AL2_x86_64","asg-min-size":3,"asg-max-size":3,"asg-desired-capacity":3,"instance-types":["c5.xlarge"],"volume-size":40}}'
    fi

    echo -n "Configuring cluster $CLUSTER_NAME"
    AWS_K8S_TESTER_EKS_NAME=$CLUSTER_NAME \
        AWS_K8S_TESTER_EKS_LOG_COLOR=false \
        AWS_K8S_TESTER_EKS_LOG_COLOR_OVERRIDE=true \
        AWS_K8S_TESTER_EKS_KUBECONFIG_PATH=$KUBECONFIG_PATH \
        AWS_K8S_TESTER_EKS_KUBECTL_PATH=$KUBECTL_PATH \
        AWS_K8S_TESTER_EKS_S3_BUCKET_NAME=$S3_BUCKET_NAME \
        AWS_K8S_TESTER_EKS_S3_BUCKET_CREATE=$S3_BUCKET_CREATE \
        AWS_K8S_TESTER_EKS_PARAMETERS_VERSION=${K8S_VERSION%.*} \
        AWS_K8S_TESTER_EKS_PARAMETERS_ENCRYPTION_CMK_CREATE=false \
        AWS_K8S_TESTER_EKS_PARAMETERS_ROLE_CREATE=$ROLE_CREATE \
        AWS_K8S_TESTER_EKS_PARAMETERS_ROLE_ARN=$ROLE_ARN \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_ENABLE=true \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_ROLE_CREATE=$ROLE_CREATE \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_ROLE_ARN=$MNG_ROLE_ARN \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_MNGS=$MNGS \
        AWS_K8S_TESTER_EKS_ADD_ON_MANAGED_NODE_GROUPS_FETCH_LOGS=true \
        AWS_K8S_TESTER_EKS_ADD_ON_NLB_HELLO_WORLD_ENABLE=$RUN_TESTER_LB_ADDONS \
        AWS_K8S_TESTER_EKS_ADD_ON_ALB_2048_ENABLE=$RUN_TESTER_LB_ADDONS \
        $TESTER_PATH eks create config --path $CLUSTER_CONFIG 1>&2

    if [[ -n "${CIRCLE_JOB:-}" || -n "${DISABLE_PROMPT:-}" ]]; then
        $TESTER_PATH eks create cluster --enable-prompt=false --path $CLUSTER_CONFIG || (echo "failed!" && exit 1)
    else
        echo -n "Creating cluster $CLUSTER_NAME (this may take ~20 mins. details: tail -f $CLUSTER_MANAGE_LOG_PATH)... "
        $TESTER_PATH eks create cluster --path $CLUSTER_CONFIG >>$CLUSTER_MANAGE_LOG_PATH 1>&2 ||
            (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
        echo "ok."
    fi
}

function up-kops-cluster {
    aws s3api create-bucket --bucket kops-cni-test-temp --region $AWS_DEFAULT_REGION --create-bucket-configuration LocationConstraint=$AWS_DEFAULT_REGION
    curl -LO https://github.com/kubernetes/kops/releases/download/$(curl -s https://api.github.com/repos/kubernetes/kops/releases/latest | grep tag_name | cut -d '"' -f 4)/kops-linux-amd64
    chmod +x kops-linux-amd64
    sudo mv kops-linux-amd64 /usr/local/bin/kops
    CLUSTER_NAME=kops-cni-test-cluster-${TEST_ID}.k8s.local
    export KOPS_STATE_STORE=s3://kops-cni-test-temp

    SSH_KEYS=~/.ssh/devopsinuse
    if [ ! -f "$SSH_KEYS" ]
    then
        echo -e "\nCreating SSH keys ..."
        ssh-keygen -t rsa -N '' -f ~/.ssh/devopsinuse
    else
        echo -e "\nSSH keys are already in place!"
    fi

    kops create cluster \
    --zones ${AWS_DEFAULT_REGION}a,${AWS_DEFAULT_REGION}b \
    --networking amazon-vpc-routed-eni \
    --node-count 2 \
    --ssh-public-key=~/.ssh/devopsinuse.pub \
    --kubernetes-version ${K8S_VERSION} \
    ${CLUSTER_NAME}
    kops update cluster --name ${CLUSTER_NAME} --yes
    sleep 100
    while [[ ! $(kops validate cluster | grep "is ready") ]]
    do
        sleep 5
        echo "Waiting for cluster validation"
    done
    kubectl apply -f https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/${MANIFEST_CNI_VERSION}/config/v1.6/cni-metrics-helper.yaml
}

function down-kops-cluster {
    kops delete cluster --name ${CLUSTER_NAME} --yes
    aws s3 rm ${KOPS_STATE_STORE} --recursive
    aws s3 rb ${KOPS_STATE_STORE} --region us-west-2
}
