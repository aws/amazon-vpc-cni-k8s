#!/usr/bin/env bash

function load_cluster_details() {
  echo "loading cluster details $CLUSTER_NAME"
  DESCRIBE_CLUSTER_OP=$(aws eks describe-cluster --name "$CLUSTER_NAME" --region "$REGION" $ENDPOINT_FLAG)
  VPC_ID=$(echo "$DESCRIBE_CLUSTER_OP" | jq -r '.cluster.resourcesVpcConfig.vpcId')
  K8S_VERSION=$(echo "$DESCRIBE_CLUSTER_OP" | jq .cluster.version -r)
}

function down-test-cluster() {
    echo -n "Deleting cluster  (this may take ~10 mins) ... "
    eksctl delete cluster $CLUSTER_NAME >>$CLUSTER_MANAGE_LOG_PATH 2>&1 ||
        (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
    echo "ok."
}

function up-test-cluster() {
    DIR=$(cd "$(dirname "$0")"; pwd)
    CLUSTER_TEMPLATE_PATH=$DIR/test/config
    if [[ "$RUN_BOTTLEROCKET_TEST" == true ]]; then
        echo "Copying bottlerocket config to $CLUSTER_CONFIG"
        cp $CLUSTER_TEMPLATE_PATH/bottlerocket.yaml $CLUSTER_CONFIG

    elif [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        echo "Copying perf test cluster config to $CLUSTER_CONFIG"
        cp $CLUSTER_TEMPLATE_PATH/perf-cluster.yml $CLUSTER_CONFIG
        AMI_ID=`aws ssm get-parameter --name /aws/service/eks/optimized-ami/${EKS_CLUSTER_VERSION}/amazon-linux-2/recommended/image_id --region us-west-2 --query "Parameter.Value" --output text`
        echo "Obtained ami_id as $AMI_ID"
        sed -i'.bak' "s,AMI_ID_PLACEHOLDER,$AMI_ID," $CLUSTER_CONFIG
        grep -r -q $AMI_ID $CLUSTER_CONFIG
        export RUN_CONFORMANCE="false"
        : "${PERFORMANCE_TEST_S3_BUCKET_NAME:=""}"
    
    else
        echo "Copying test cluster config to $CLUSTER_CONFIG"
        cp $CLUSTER_TEMPLATE_PATH/test-cluster.yaml $CLUSTER_CONFIG
        sed -i'.bak' "s,K8S_VERSION_PLACEHOLDER,$EKS_CLUSTER_VERSION," $CLUSTER_CONFIG
        AMI_ID=`aws ssm get-parameter --name /aws/service/eks/optimized-ami/${EKS_CLUSTER_VERSION}/amazon-linux-2/recommended/image_id --region us-west-2 --query "Parameter.Value" --output text`
        ARM_AMI_ID=`aws ssm get-parameter --name /aws/service/eks/optimized-ami/${EKS_CLUSTER_VERSION}/amazon-linux-2-arm64/recommended/image_id --region us-west-2 --query "Parameter.Value" --output text`
        sed -i'.bak' "s,X86_AMI_ID_PLACEHOLDER,$AMI_ID," $CLUSTER_CONFIG
        sed -i'.bak' "s,ARM_AMI_ID_PLACEHOLDER,$ARM_AMI_ID," $CLUSTER_CONFIG
        grep -r -q $EKS_CLUSTER_VERSION $CLUSTER_CONFIG
        grep -r -q $AMI_ID $CLUSTER_CONFIG
        grep -r -q $ARM_AMI_ID $CLUSTER_CONFIG
        : "${ROLE_ARN:=""}"
        sed -i'.bak' "s,ROLE_ARN_PLACEHOLDER,$ROLE_ARN," $CLUSTER_CONFIG
    fi

    sed -i'.bak' "s,CLUSTER_NAME_PLACEHOLDER,$CLUSTER_NAME," $CLUSTER_CONFIG
    grep -r -q $CLUSTER_NAME $CLUSTER_CONFIG
    echo -n "Creating cluster $CLUSTER_NAME (this may take ~20 mins. details: tail -f $CLUSTER_MANAGE_LOG_PATH)... "
    eksctl create cluster -f $CLUSTER_CONFIG --kubeconfig $KUBECONFIG_PATH >>$CLUSTER_MANAGE_LOG_PATH 1>&2 ||
        (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
    echo "ok."
    export KUBECONFIG=$KUBECONFIG_PATH
    
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        kubectl create -f $DIR/test/config/cluster-autoscaler-autodiscover.yml
    fi
}

function up-kops-cluster {
    KOPS_S3_BUCKET=kops-cni-test-eks-$AWS_ACCOUNT_ID
    echo "Using $KOPS_S3_BUCKET as kops state store"
    aws s3api create-bucket --bucket $KOPS_S3_BUCKET --region $AWS_DEFAULT_REGION --create-bucket-configuration LocationConstraint=$AWS_DEFAULT_REGION
    kops_version="v1.22.3"
    echo "Using kops version $kops_version"
    curl -LO https://github.com/kubernetes/kops/releases/download/$kops_version/kops-linux-amd64
    chmod +x kops-linux-amd64
    mkdir -p ~/kops_bin
    KOPS_BIN=~/kops_bin/kops
    mv kops-linux-amd64 $KOPS_BIN
    CLUSTER_NAME=kops-cni-test-cluster-${TEST_ID}.k8s.local
    export KOPS_STATE_STORE=s3://${KOPS_S3_BUCKET}

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
    --container-runtime docker \
    --node-count 2 \
    --ssh-public-key=~/.ssh/devopsinuse.pub \
    --kubernetes-version ${K8S_VERSION} \
    ${CLUSTER_NAME}
    $KOPS_BIN update cluster --name ${CLUSTER_NAME} --yes
    sleep 100
    $KOPS_BIN export kubeconfig --admin
    sleep 10
    MAX_RETRIES=15
    RETRY_ATTEMPT=0
    while [[ ! $($KOPS_BIN validate cluster | grep "is ready") && $RETRY_ATTEMPT -lt $MAX_RETRIES ]]
    do
        sleep 60
        let RETRY_ATTEMPT=RETRY_ATTEMPT+1
        echo "In attempt# $RETRY_ATTEMPT, waiting for cluster validation"
    done
    kubectl apply -f https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/${MANIFEST_CNI_VERSION}/config/master/cni-metrics-helper.yaml
}

function down-kops-cluster {
    KOPS_BIN=~/kops_bin/kops
    $KOPS_BIN delete cluster --name ${CLUSTER_NAME} --yes
    aws s3 rm ${KOPS_STATE_STORE} --recursive
    aws s3 rb ${KOPS_STATE_STORE} --region $AWS_DEFAULT_REGION
}