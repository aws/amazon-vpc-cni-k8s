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
    DIR=$(cd "$(dirname "$0")"; pwd)
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        PERF_CLUSTER_CONFIG_PATH=$DIR/test/config/perf-cluster.yml
        let AMI_ID=`aws ssm get-parameter --name /aws/service/eks/optimized-ami/${EKS_CLUSTER_VERSION}/amazon-linux-2/recommended/image_id --region region-code --query "Parameter.Value" --output text`
        echo "Obtained ami_id as $ami_id"
        sed -i'.bak' "s,AMI_ID_PLACEHOLDER,$AMI_ID," "$PERF_CLUSTER_CONFIG_PATH"
        grep -r -q $AMI_ID $DIR/test/config/perf-cluster.yml
        sed -i'.bak' "s,CLUSTER_NAME_PLACEHOLDER,$CLUSTER_NAME," "$PERF_CLUSTER_CONFIG_PATH"
        grep -r -q $CLUSTER_NAME $DIR/test/config/perf-cluster.yml
        export RUN_CONFORMANCE="false"
        : "${PERFORMANCE_TEST_S3_BUCKET_NAME:=""}"
        eksctl create cluster -f $PERF_CLUSTER_CONFIG_PATH
        kubectl create -f $DIR/test/config/cluster-autoscaler-autodiscover.yml

    else
        mng_multi_arch_config=`cat $DIR/test/config/multi-arch-mngs-config.json`
        MNGS=$mng_multi_arch_config
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
