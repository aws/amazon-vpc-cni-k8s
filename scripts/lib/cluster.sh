#!/usr/bin/env bash

function down-test-cluster() {
    if [[ -n "${CIRCLE_JOB:-}" || -n "${DISABLE_PROMPT:-}" ]]; then
        $TESTER_PATH eks delete cluster --enable-prompt=false --path $CLUSTER_CONFIG || (echo "failed!" && exit 1)
    else
        echo -n "Deleting cluster $CLUSTER_NAME (this may take ~10 mins) ... "
        if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
          eksctl delete cluster --name $CLUSTER_NAME
        else
          $TESTER_PATH eks delete cluster --enable-prompt=false --path $CLUSTER_CONFIG >>$CLUSTER_MANAGE_LOG_PATH 2>&1 ||
              (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
          echo "ok."
    fi
}

function up-test-cluster() {
    MNGS=""
    DIR=$(cd "$(dirname "$0")"; pwd)
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        PERF_CLUSTER_TEMPLATE_PATH=$DIR/test/config/perf-cluster.yml
        PERF_TEST_CONFIG_PATH=$DIR/test/config/perf-cluster-$CLUSTER_NAME.yml
        cp $PERF_CLUSTER_TEMPLATE_PATH $PERF_TEST_CONFIG_PATH
        AMI_ID=`aws ssm get-parameter --name /aws/service/eks/optimized-ami/${EKS_CLUSTER_VERSION}/amazon-linux-2/recommended/image_id --region us-west-2 --query "Parameter.Value" --output text`
        echo "Obtained ami_id as $AMI_ID"
        sed -i'.bak' "s,AMI_ID_PLACEHOLDER,$AMI_ID," $PERF_TEST_CONFIG_PATH
        grep -r -q $AMI_ID $PERF_TEST_CONFIG_PATH
        sed -i'.bak' "s,CLUSTER_NAME_PLACEHOLDER,$CLUSTER_NAME," $PERF_TEST_CONFIG_PATH
        grep -r -q $CLUSTER_NAME $PERF_TEST_CONFIG_PATH
        export RUN_CONFORMANCE="false"
        : "${PERFORMANCE_TEST_S3_BUCKET_NAME:=""}"
        eksctl create cluster -f $PERF_TEST_CONFIG_PATH
        kubectl create -f $DIR/test/config/cluster-autoscaler-autodiscover.yml
        return
    else
        mng_multi_arch_config=`cat $DIR/test/config/multi-arch-mngs-config.json`
        MNGS=$mng_multi_arch_config
    fi

    echo -n "Configuring cluster $CLUSTER_NAME"
    AWS_K8S_TESTER_EKS_NAME=$CLUSTER_NAME \
        AWS_K8S_TESTER_EKS_LOG_COLOR=false \
        AWS_K8S_TESTER_EKS_LOG_COLOR_OVERRIDE=true \
        AWS_K8S_TESTER_EKS_KUBECONFIG_PATH=$KUBECONFIG_PATH \
        AWS_K8S_TESTER_EKS_KUBECTL_PATH=$KUBECTL_PATH \
        AWS_K8S_TESTER_EKS_S3_BUCKET_NAME=$S3_BUCKET_NAME \
        AWS_K8S_TESTER_EKS_S3_BUCKET_CREATE=$S3_BUCKET_CREATE \
        AWS_K8S_TESTER_EKS_VERSION=${EKS_CLUSTER_VERSION} \
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
    aws s3api create-bucket --bucket kops-cni-test-eks --region $AWS_DEFAULT_REGION --create-bucket-configuration LocationConstraint=$AWS_DEFAULT_REGION
    curl -LO https://github.com/kubernetes/kops/releases/download/$(curl -s https://api.github.com/repos/kubernetes/kops/releases/latest | grep tag_name | cut -d '"' -f 4)/kops-linux-amd64
    chmod +x kops-linux-amd64
    mkdir -p ~/kops_bin
    KOPS_BIN=~/kops_bin/kops
    mv kops-linux-amd64 $KOPS_BIN
    CLUSTER_NAME=kops-cni-test-cluster-${TEST_ID}.k8s.local
    export KOPS_STATE_STORE=s3://kops-cni-test-eks

    SSH_KEYS=~/.ssh/devopsinuse
    if [ ! -f "$SSH_KEYS" ]
    then
        echo -e "\nCreating SSH keys ..."
        ssh-keygen -t rsa -N '' -f ~/.ssh/devopsinuse
    else
        echo -e "\nSSH keys are already in place!"
    fi

    $KOPS_BIN create cluster \
    --zones ${AWS_DEFAULT_REGION}a,${AWS_DEFAULT_REGION}b \
    --networking amazon-vpc-routed-eni \
    --node-count 2 \
    --ssh-public-key=~/.ssh/devopsinuse.pub \
    --kubernetes-version ${K8S_VERSION} \
    ${CLUSTER_NAME}
    $KOPS_BIN update cluster --name ${CLUSTER_NAME} --yes
    sleep 100
    $KOPS_BIN export kubeconfig --admin
    sleep 10
    while [[ ! $($KOPS_BIN validate cluster | grep "is ready") ]]
    do
        sleep 5
        echo "Waiting for cluster validation"
    done
    kubectl apply -f https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/${MANIFEST_CNI_VERSION}/config/master/cni-metrics-helper.yaml
}

function down-kops-cluster {
    KOPS_BIN=~/kops_bin/kops
    $KOPS_BIN delete cluster --name ${CLUSTER_NAME} --yes
    aws s3 rm ${KOPS_STATE_STORE} --recursive
    aws s3 rb ${KOPS_STATE_STORE} --region $AWS_DEFAULT_REGION
}
