#!/usr/bin/env bash

# Provisions and deletes a SageMaker HyperPod cluster orchestrated by an EKS cluster for the [HYPERPOD] ipamd tests.
# See https://docs.aws.amazon.com/sagemaker/latest/dg/sagemaker-hyperpod-eks-prerequisites.html
#
# Variables:
# HYPERPOD_INSTANCE_TYPE: HyperPod instance type, default ml.t3.xlarge
# HYPERPOD_INSTANCE_COUNT: number of HyperPod nodes, default 3 (the ipamd suite needs at least 2)
# HYPERPOD_INSTANCE_GROUP: instance group name, default cni-test
# The EKS cluster uses EKS_CLUSTER_VERSION, which must be a Kubernetes version HyperPod supports.

: "${HYPERPOD_INSTANCE_TYPE:=ml.t3.xlarge}"
: "${HYPERPOD_INSTANCE_COUNT:=3}"
: "${HYPERPOD_INSTANCE_GROUP:=cni-test}"

function hyperpod_names() {
    HYPERPOD_CLUSTER_NAME="hp-$CLUSTER_NAME"
    # The managed AmazonSageMakerClusterInstanceRolePolicy only allows S3 buckets named sagemaker-*
    HYPERPOD_LIFECYCLE_BUCKET="sagemaker-$CLUSTER_NAME-$AWS_ACCOUNT_ID-$AWS_DEFAULT_REGION"
    HYPERPOD_EXECUTION_ROLE="sagemaker-hyperpod-$CLUSTER_NAME"
}

function warn_hyperpod_max_pods() {
    echo "*******************************************************************************"
    echo "WARNING: HyperPod nodes are attached with a single ENI by default, which limits pods per node"
    echo "(e.g. 14 on ml.t3.xlarge instead of 44). Currently only allowlisted accounts get the elevated max pods that"
    echo "let the VPC CNI attach more ENIs, and only for clusters created with continuous node provisioning, which this"
    echo "script uses. Without it, the multi-ENI and reserved ENI slot [HYPERPOD] specs skip."
    echo "Contact the SageMaker HyperPod team to make sure account $AWS_ACCOUNT_ID is allowlisted in $AWS_DEFAULT_REGION,"
    echo "and check the capacity.pods this script prints once the nodes are Ready."
    echo "*******************************************************************************"
}

function up-hyperpod-cluster() {
    hyperpod_names
    warn_hyperpod_max_pods

    echo "Creating EKS cluster $CLUSTER_NAME for HyperPod (this may take ~20 mins. details: tail -f $CLUSTER_MANAGE_LOG_PATH)... "
    # HyperPod requires API or API_AND_CONFIG_MAP authentication and private subnets, which eksctl creates by default
    cat > "$CLUSTER_CONFIG" <<EOF
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: $CLUSTER_NAME
  region: $AWS_DEFAULT_REGION
  version: "$EKS_CLUSTER_VERSION"
accessConfig:
  authenticationMode: API_AND_CONFIG_MAP
addons:
  - name: vpc-cni
  - name: kube-proxy
  - name: coredns
    configurationValues: "{\"podDisruptionBudget\": {\"enabled\": false}}"
EOF
    eksctl create cluster -f "$CLUSTER_CONFIG" --kubeconfig "$KUBECONFIG_PATH" >>"$CLUSTER_MANAGE_LOG_PATH" 2>&1 ||
        { echo "failed. Check $CLUSTER_MANAGE_LOG_PATH."; return 1; }
    __cluster_created=1
    export KUBECONFIG=$KUBECONFIG_PATH
    echo "ok."

    echo "Installing the HyperPod Helm chart"
    rm -rf "$TEST_CLUSTER_DIR/sagemaker-hyperpod-cli"
    git clone --depth 1 https://github.com/aws/sagemaker-hyperpod-cli.git "$TEST_CLUSTER_DIR/sagemaker-hyperpod-cli"
    helm dependencies update "$TEST_CLUSTER_DIR/sagemaker-hyperpod-cli/helm_chart/HyperPodHelmChart"
    helm install hyperpod-dependencies "$TEST_CLUSTER_DIR/sagemaker-hyperpod-cli/helm_chart/HyperPodHelmChart" \
        --namespace kube-system

    echo "Creating HyperPod execution role $HYPERPOD_EXECUTION_ROLE"
    aws iam create-role --role-name "$HYPERPOD_EXECUTION_ROLE" --assume-role-policy-document '{
        "Version": "2012-10-17",
        "Statement": [{"Effect": "Allow", "Principal": {"Service": "sagemaker.amazonaws.com"}, "Action": "sts:AssumeRole"}]
    }' >/dev/null
    aws iam attach-role-policy --role-name "$HYPERPOD_EXECUTION_ROLE" \
        --policy-arn arn:aws:iam::aws:policy/AmazonSageMakerClusterInstanceRolePolicy
    # The EKS permissions from the HyperPod IAM documentation, plus the SageMaker ENI attach used by the VPC CNI
    aws iam put-role-policy --role-name "$HYPERPOD_EXECUTION_ROLE" --policy-name hyperpod-eks --policy-document '{
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Action": [
                    "ec2:AssignPrivateIpAddresses", "ec2:AttachNetworkInterface", "ec2:CreateNetworkInterface",
                    "ec2:CreateNetworkInterfacePermission", "ec2:DeleteNetworkInterface",
                    "ec2:DeleteNetworkInterfacePermission", "ec2:DescribeInstances", "ec2:DescribeInstanceTypes",
                    "ec2:DescribeNetworkInterfaces", "ec2:DescribeTags", "ec2:DescribeVpcs", "ec2:DescribeDhcpOptions",
                    "ec2:DescribeSubnets", "ec2:DescribeSecurityGroups", "ec2:DetachNetworkInterface",
                    "ec2:ModifyNetworkInterfaceAttribute", "ec2:UnassignPrivateIpAddresses",
                    "ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:GetAuthorizationToken",
                    "ecr:GetDownloadUrlForLayer", "eks-auth:AssumeRoleForPodIdentity",
                    "sagemaker:AttachClusterNodeNetworkInterface"
                ],
                "Resource": "*"
            },
            {"Effect": "Allow", "Action": "ec2:CreateTags", "Resource": "arn:aws:ec2:*:*:network-interface/*"}
        ]
    }'
    HYPERPOD_EXECUTION_ROLE_ARN=$(aws iam get-role --role-name "$HYPERPOD_EXECUTION_ROLE" --query Role.Arn --output text)
    # IAM is eventually consistent; CreateCluster fails if the role is not visible to SageMaker yet
    sleep 15

    echo "Uploading the lifecycle script to s3://$HYPERPOD_LIFECYCLE_BUCKET"
    if [[ $AWS_DEFAULT_REGION == us-east-1 ]]; then
        aws s3api create-bucket --bucket "$HYPERPOD_LIFECYCLE_BUCKET" >/dev/null
    else
        aws s3api create-bucket --bucket "$HYPERPOD_LIFECYCLE_BUCKET" \
            --create-bucket-configuration LocationConstraint="$AWS_DEFAULT_REGION" >/dev/null
    fi
    printf '#!/bin/bash\nset -ex\necho "[start] on_create.sh"\n' > "$TEST_CLUSTER_DIR/on_create.sh"
    aws s3 cp "$TEST_CLUSTER_DIR/on_create.sh" "s3://$HYPERPOD_LIFECYCLE_BUCKET/lifecycle/on_create.sh"

    local describe_cluster cluster_arn cluster_sg vpc_id subnets
    describe_cluster=$(aws eks describe-cluster --name "$CLUSTER_NAME" --region "$AWS_DEFAULT_REGION")
    cluster_arn=$(echo "$describe_cluster" | jq -r .cluster.arn)
    cluster_sg=$(echo "$describe_cluster" | jq -r .cluster.resourcesVpcConfig.clusterSecurityGroupId)
    vpc_id=$(echo "$describe_cluster" | jq -r .cluster.resourcesVpcConfig.vpcId)
    # HyperPod nodes must be in private subnets, which eksctl tags for internal load balancers
    subnets=$(aws ec2 describe-subnets --region "$AWS_DEFAULT_REGION" \
        --filters Name=vpc-id,Values="$vpc_id" Name=tag:kubernetes.io/role/internal-elb,Values=1 \
        --query 'Subnets[].SubnetId' --output json)

    echo -n "Creating HyperPod cluster $HYPERPOD_CLUSTER_NAME with $HYPERPOD_INSTANCE_COUNT x $HYPERPOD_INSTANCE_TYPE (this may take ~30 mins)... "
    cat > "$TEST_CLUSTER_DIR/create_hyperpod_cluster.json" <<EOF
{
    "ClusterName": "$HYPERPOD_CLUSTER_NAME",
    "InstanceGroups": [{
        "InstanceGroupName": "$HYPERPOD_INSTANCE_GROUP",
        "InstanceType": "$HYPERPOD_INSTANCE_TYPE",
        "InstanceCount": $HYPERPOD_INSTANCE_COUNT,
        "LifeCycleConfig": {
            "SourceS3Uri": "s3://$HYPERPOD_LIFECYCLE_BUCKET/lifecycle/",
            "OnCreate": "on_create.sh"
        },
        "ExecutionRole": "$HYPERPOD_EXECUTION_ROLE_ARN",
        "ThreadsPerCore": 1
    }],
    "VpcConfig": {
        "SecurityGroupIds": ["$cluster_sg"],
        "Subnets": $subnets
    },
    "Orchestrator": {"Eks": {"ClusterArn": "$cluster_arn"}},
    "NodeRecovery": "None",
    "NodeProvisioningMode": "Continuous"
}
EOF
    aws sagemaker create-cluster --region "$AWS_DEFAULT_REGION" \
        --cli-input-json "file://$TEST_CLUSTER_DIR/create_hyperpod_cluster.json" >/dev/null

    local status
    for _ in $(seq 1 120); do
        status=$(aws sagemaker describe-cluster --region "$AWS_DEFAULT_REGION" --cluster-name "$HYPERPOD_CLUSTER_NAME" \
            --query ClusterStatus --output text)
        [[ $status == InService || $status == Failed ]] && break
        sleep 30
    done
    if [[ $status != InService ]]; then
        echo "failed with status $status."
        aws sagemaker describe-cluster --region "$AWS_DEFAULT_REGION" --cluster-name "$HYPERPOD_CLUSTER_NAME" \
            --query FailureMessage --output text
        return 1
    fi
    echo "ok."

    # Elevated max pods, which the multi-ENI and reserved ENI slot specs need, is only given to clusters with
    # continuous node provisioning
    local provisioning_mode
    provisioning_mode=$(aws sagemaker describe-cluster --region "$AWS_DEFAULT_REGION" --cluster-name "$HYPERPOD_CLUSTER_NAME" \
        --query NodeProvisioningMode --output text)
    if [[ $provisioning_mode != Continuous ]]; then
        echo "HyperPod cluster $HYPERPOD_CLUSTER_NAME has NodeProvisioningMode $provisioning_mode, expected Continuous"
        return 1
    fi
    echo "HyperPod cluster $HYPERPOD_CLUSTER_NAME uses NodeProvisioningMode $provisioning_mode"

    echo "Waiting for $HYPERPOD_INSTANCE_COUNT HyperPod nodes to be Ready"
    local ready
    for _ in $(seq 1 60); do
        ready=$(kubectl get nodes -l sagemaker.amazonaws.com/instance-group-name="$HYPERPOD_INSTANCE_GROUP" --no-headers 2>/dev/null |
            awk '$2 == "Ready"' | wc -l)
        [[ $ready -ge $HYPERPOD_INSTANCE_COUNT ]] && break
        sleep 20
    done
    [[ $ready -ge $HYPERPOD_INSTANCE_COUNT ]] || { echo "only $ready of $HYPERPOD_INSTANCE_COUNT HyperPod nodes are Ready"; return 1; }

    local max_pods
    max_pods=$(kubectl get nodes -l sagemaker.amazonaws.com/instance-group-name="$HYPERPOD_INSTANCE_GROUP" \
        -o jsonpath='{.items[0].status.capacity.pods}')
    echo "HyperPod nodes advertise capacity.pods=$max_pods"
    warn_hyperpod_max_pods

    HYPERPOD_EKS_CLUSTER_NAME=$CLUSTER_NAME
    HYPERPOD_NG_LABEL_VAL=$HYPERPOD_INSTANCE_GROUP
}

function down-hyperpod-cluster() {
    hyperpod_names
    local status=0

    echo -n "Deleting HyperPod cluster $HYPERPOD_CLUSTER_NAME (this may take ~15 mins)... "
    if aws sagemaker delete-cluster --region "$AWS_DEFAULT_REGION" --cluster-name "$HYPERPOD_CLUSTER_NAME" >/dev/null 2>&1; then
        while aws sagemaker describe-cluster --region "$AWS_DEFAULT_REGION" --cluster-name "$HYPERPOD_CLUSTER_NAME" >/dev/null 2>&1; do
            sleep 30
        done
    fi
    echo "ok."

    down-test-cluster || status=$?

    echo "Deleting HyperPod execution role $HYPERPOD_EXECUTION_ROLE and lifecycle bucket $HYPERPOD_LIFECYCLE_BUCKET"
    aws iam delete-role-policy --role-name "$HYPERPOD_EXECUTION_ROLE" --policy-name hyperpod-eks 2>/dev/null || true
    aws iam detach-role-policy --role-name "$HYPERPOD_EXECUTION_ROLE" \
        --policy-arn arn:aws:iam::aws:policy/AmazonSageMakerClusterInstanceRolePolicy 2>/dev/null || true
    aws iam delete-role --role-name "$HYPERPOD_EXECUTION_ROLE" 2>/dev/null || status=1
    aws s3 rb "s3://$HYPERPOD_LIFECYCLE_BUCKET" --force >/dev/null 2>&1 || status=1

    return "$status"
}
