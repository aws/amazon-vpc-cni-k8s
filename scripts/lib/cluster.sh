#!/usr/bin/env bash

function down-test-cluster() {
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        CLUSTER_CONFIG_FILE="./testdata/performance_test.yaml"
        eksctl delete cluster -f $CLUSTER_CONFIG_FILE || (echo "failed!" && exit 1)
    else
        CLUSTER_CONFIG_FILE="./testdata/test_cluster.yaml"
        eksctl delete cluster -f $CLUSTER_CONFIG_FILE || (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
    fi
}

function up-test-cluster() {
    CLUSTER_CONFIG_FILE=""
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        CLUSTER_CONFIG_FILE="./testdata/performance_test.yaml"
        export RUN_CONFORMANCE="false"
        : "${PERFORMANCE_TEST_S3_BUCKET_NAME:=""}"

        eksctl create cluster -f $CLUSTER_CONFIG_FILE || (echo "failed!" && exit 1)

    else

        CLUSTER_CONFIG_FILE="./testdata/test_cluster.yaml"

        # Changing the name in the config file to the reandomly generated CLUSTER_NAME
        # since we cannot have multiple concurrent clusters with the same name under the same account
        sed -i -e 's, CLUSTERNAME, '$CLUSTER_NAME',g'  $CLUSTER_CONFIG_FILE

        eksctl create cluster -f $CLUSTER_CONFIG_FILE || (echo "failed!" && exit 1)
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
