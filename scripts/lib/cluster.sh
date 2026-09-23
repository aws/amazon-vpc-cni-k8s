#!/usr/bin/env bash

source "$SCRIPT_DIR"/lib/set_kubeconfig.sh

function load_cluster_details() {
  echo "loading cluster details $CLUSTER_NAME"
  DESCRIBE_CLUSTER_OP=$(aws eks describe-cluster --name "$CLUSTER_NAME" --region "$REGION" $ENDPOINT_FLAG)
  VPC_ID=$(echo "$DESCRIBE_CLUSTER_OP" | jq -r '.cluster.resourcesVpcConfig.vpcId')
  K8S_VERSION=$(echo "$DESCRIBE_CLUSTER_OP" | jq .cluster.version -r)
}

function load_deveks_cluster_details() {

  echo "loading cluster details $CLUSTER_NAME"
  PROVIDER_ID=$(kubectl get nodes --kubeconfig $KUBECONFIG -ojson | jq -r '.items[0].spec.providerID')
  INSTANCE_ID=${PROVIDER_ID##*/}
  VPC_ID=$(aws ec2 describe-instances --instance-ids ${INSTANCE_ID} | jq -r '.Reservations[].Instances[].VpcId')
}

function down-test-cluster() {
    local delete_status=0

    echo -n "Deleting cluster  (this may take ~10 mins) ... "
    eksctl delete cluster "$CLUSTER_NAME" >>"$CLUSTER_MANAGE_LOG_PATH" 2>&1 || delete_status=$?
    if [[ $delete_status -ne 0 ]]; then
        echo "failed. Check $CLUSTER_MANAGE_LOG_PATH."
        return "$delete_status"
    fi
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
        grep -r -q $EKS_CLUSTER_VERSION $CLUSTER_CONFIG
        : "${ROLE_ARN:=""}"
        sed -i'.bak' "s,ROLE_ARN_PLACEHOLDER,$ROLE_ARN," $CLUSTER_CONFIG
    fi

    sed -i'.bak' "s,CLUSTER_NAME_PLACEHOLDER,$CLUSTER_NAME," $CLUSTER_CONFIG
    grep -r -q $CLUSTER_NAME $CLUSTER_CONFIG
    echo -n "Creating cluster $CLUSTER_NAME (this may take ~20 mins. details: tail -f $CLUSTER_MANAGE_LOG_PATH)... "
    __cluster_created=1
    eksctl create cluster -f $CLUSTER_CONFIG --kubeconfig $KUBECONFIG_PATH >>$CLUSTER_MANAGE_LOG_PATH 1>&2 ||
        (echo "failed. Check $CLUSTER_MANAGE_LOG_PATH." && exit 1)
    echo "ok."
    export KUBECONFIG=$KUBECONFIG_PATH
    
    if [[ "$RUN_PERFORMANCE_TESTS" == true ]]; then
        echo "Deploying cluster autoscaler"
        kubectl create -f $DIR/test/config/cluster-autoscaler-autodiscover.yml
        echo "Deploying metrics server"
        kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
    fi
}

function up-kops-cluster {
    KOPS_S3_BUCKET=kops-cni-test-eks-$AWS_ACCOUNT_ID
    echo "Using $KOPS_S3_BUCKET as kops state store"
    if ! aws s3api head-bucket --bucket $KOPS_S3_BUCKET 2>/dev/null; then
        aws s3api create-bucket --bucket $KOPS_S3_BUCKET --region $AWS_DEFAULT_REGION --create-bucket-configuration LocationConstraint=$AWS_DEFAULT_REGION
    fi
    echo "Using kops version $KOPS_VERSION"
    curl -LO https://github.com/kubernetes/kops/releases/download/$KOPS_VERSION/kops-linux-amd64
    chmod +x kops-linux-amd64
    mkdir -p ~/kops_bin
    KOPS_BIN=~/kops_bin/kops
    mv kops-linux-amd64 $KOPS_BIN
    CLUSTER_NAME=kops-cni-test-cluster-${TEST_ID}.k8s.local
    export KOPS_STATE_STORE=s3://${KOPS_S3_BUCKET}
    HOST_IMAGE_SSM_PARAMETER="ssm:/aws/service/canonical/ubuntu/server/22.04/stable/current/amd64/hvm/ebs-gp2/ami-id"

    SSH_KEYS=~/.ssh/devopsinuse
    if [ ! -f "$SSH_KEYS" ]
    then
        echo -e "\nCreating SSH keys ..."
        ssh-keygen -t rsa -N '' -f ~/.ssh/devopsinuse
    else
        echo -e "\nSSH keys are already in place!"
    fi

    # Ubuntu 22.04 (glibc 2.35): kops installs containerd 2.1.x, which links against
    # glibc 2.35 and cannot exec on 20.04's glibc 2.31. The prior 20.04 pin (#2103) is
    # obsolete since #3354 fixed the host-veth MAC/ARP regression on newer kernels.
    if [[ -n ${KOPS_CLEANUP_STATE_FILE:-} ]]; then
        (
            umask 077
            printf '%s\n%s\n' "$CLUSTER_NAME" "$KOPS_STATE_STORE" > "${KOPS_CLEANUP_STATE_FILE}.tmp"
        )
        mv "${KOPS_CLEANUP_STATE_FILE}.tmp" "$KOPS_CLEANUP_STATE_FILE"
    fi

    __cluster_created=1
    $KOPS_BIN create cluster \
    --cloud aws \
    --zones ${AWS_DEFAULT_REGION}a,${AWS_DEFAULT_REGION}b \
    --networking amazonvpc \
    --node-count 2 \
    --node-size c5.xlarge \
    --control-plane-count 3 \
    --control-plane-size c5.xlarge \
    --control-plane-zones ${AWS_DEFAULT_REGION}a,${AWS_DEFAULT_REGION}b \
    --ssh-public-key=~/.ssh/devopsinuse.pub \
    --kubernetes-version ${K8S_VERSION} \
    --image ${HOST_IMAGE_SSM_PARAMETER} \
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

    # kOps cluster by default comes with ebs-csi-node and coredns-autoscaler as an add-on which cannot be excluded on cluster creation
    # Since CNI tests don't need any of these components for running tests, we delete these to avoid any flakiness
    export KUBECONFIG=~/.kube/config
    kubectl delete daemonset ebs-csi-node -n kube-system
    kubectl delete deployment coredns-autoscaler -n kube-system
    kubectl delete deployment aws-node-termination-handler -n kube-system --wait=true

    kubectl apply -f https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/${MANIFEST_CNI_VERSION}/config/master/cni-metrics-helper.yaml
}

function kops-cluster-is-absent {
    local get_output

    if get_output=$("$KOPS_BIN" get cluster "$CLUSTER_NAME" 2>&1); then
        return 1
    fi

    # kOps v1.36 reports either form when the state-store entry is gone.
    [[ "$get_output" == *"cluster not found \"$CLUSTER_NAME\""* ||
       "$get_output" == *"no clusters found"* ]]
}

function clear-kops-cleanup-state {
    if [[ -n ${KOPS_CLEANUP_STATE_FILE:-} ]]; then
        rm -f "$KOPS_CLEANUP_STATE_FILE"
    fi
}

function down-kops-cluster {
    local delete_delay=${KOPS_DELETE_DELAY_SECONDS:-240}
    local delete_attempts=${KOPS_DELETE_ATTEMPTS:-2}
    local retry_delay=${KOPS_DELETE_RETRY_DELAY_SECONDS:-10}
    local attempt
    local delete_status=0

    if [[ ! $delete_delay =~ ^[0-9]+$ ||
          ! $delete_attempts =~ ^[1-9][0-9]*$ ||
          ! $retry_delay =~ ^[0-9]+$ ]]; then
        echo "Invalid kOps deletion retry configuration" >&2
        return 2
    fi

    KOPS_BIN=${KOPS_BIN:-~/kops_bin/kops}

    if kops-cluster-is-absent; then
        clear-kops-cleanup-state
        return "$?"
    fi

    if [[ ${__kops_delete_delay_complete:-0} -eq 0 && $delete_delay -gt 0 ]]; then
        echo "Waiting for $delete_delay seconds to avoid ENI leakage during cluster deletion..."
        sleep "$delete_delay"
        __kops_delete_delay_complete=1
    fi

    for ((attempt = 1; attempt <= delete_attempts; attempt++)); do
        delete_status=0
        "$KOPS_BIN" delete cluster --name "$CLUSTER_NAME" --yes || delete_status=$?
        if [[ $delete_status -eq 0 ]] || kops-cluster-is-absent; then
            clear-kops-cleanup-state
            return "$?"
        fi

        if [[ $attempt -lt $delete_attempts ]]; then
            echo "kOps deletion attempt $attempt failed; retrying in $retry_delay seconds..."
            sleep "$retry_delay"
        fi
    done

    return "$delete_status"
}
