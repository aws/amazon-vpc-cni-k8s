#!/usr/bin/env bash

set -eo pipefail

TEST_DIR=$(cd "$(dirname "$0")" || exit 1; pwd)
TEMPLATE_FILE="$TEST_DIR/template/eksctl/eks-cluster.yaml"

source "$PWD/scripts/lib/common.sh"

USAGE=$(cat << 'EOM'
Usage: create-cluster.sh -n [cluster-name]
Creates an EKS Cluster with Nodegroups. The Cluster is
created using eksctl with a pre-defined eksctl template.
 Required:
 -n   Name of the EKS cluster
 Optional:
 -v   K8s Version. Defaults to 1.20
 -r   Region of the EKS Cluster. Defaults to us-west-2
 -c   eksctl Cluster config path. Defaults to build/eks-cluster.yaml
 -t   Type of cluster. Defaults to eksctl cluster. Can be set to "kops" or "eksctl"
EOM
)



while getopts "n:v:r:c:t:" o; do
  case "${o}" in
    n) # Name of the EKS Cluster
      CLUSTER_NAME=${OPTARG}
      ;;
    v) # K8s Version of the EKS Cluster
      K8S_VERSION=${OPTARG}
      ;;
    r) # Region where EKS Cluster will be created
      AWS_REGION=${OPTARG}
      ;;
    c) # eksctl cluster config file path
      CLUSTER_CONFIG_PATH=${OPTARG}
      ;;
    t) # Type of cluster to be created
      CLUSTER_TYPE=${OPTARG}
      ;;
    *)
      echoerr "${USAGE}"
      exit 1
      ;;
  esac
done



shift $((OPTIND-1))

if [[ -z "$CLUSTER_NAME" ]]; then
 echoerr "${USAGE}\n\nmissing: -n is a required flag\n"
 exit 1
fi

if [[ -z "$K8S_VERSION" ]]; then
  K8S_VERSION="1.20"
  echo "no k8s version defined, will fallback to default version $K8S_VERSION"
fi

if [[ -z "$AWS_REGION" ]]; then
  AWS_REGION="us-west-2"
  echo "no regions defined, will fallback to default region $AWS_REGION"
fi

if [[ -z "$CLUSTER_CONFIG_PATH" ]]; then
  CLUSTER_CONFIG_DIR="$TEST_DIR/build"
  CLUSTER_CONFIG_PATH="$CLUSTER_CONFIG_DIR/eks-cluster.yaml"

  echo "cluster config path not defined, will generate it at default path $CLUSTER_CONFIG_PATH"

  mkdir -p "$CLUSTER_CONFIG_DIR"
fi


function create_eksctl_cluster() {
  check_is_installed eksctl
  echo "creating a $K8S_VERSION cluster named $CLUSTER_NAME using eksctl"

  # Using a static Instance Role Name so IAM Policies
  # can be attached to it later without having to retrieve
  # the auto generated eksctl name.
  sed "s/CLUSTER_NAME/$CLUSTER_NAME/g;
  s/CLUSTER_REGION/$AWS_REGION/g;
  s/K8S_VERSION/$K8S_VERSION/g;
  s/INSTANCE_ROLE_NAME/$INSTANCE_ROLE_NAME/g" \
  "$TEMPLATE_FILE" > "$CLUSTER_CONFIG_PATH"

  eksctl create cluster -f "$CLUSTER_CONFIG_PATH"
}



function create_kops_cluster {
    aws s3api create-bucket --bucket kops-cni-test-temp --region $AWS_REGION --create-bucket-configuration LocationConstraint=$AWS_REGION
    curl -LO https://github.com/kubernetes/kops/releases/download/$(curl -s https://api.github.com/repos/kubernetes/kops/releases/latest | grep tag_name | cut -d '"' -f 4)/kops-linux-amd64
    chmod +x kops-linux-amd64
    sudo mv kops-linux-amd64 /usr/local/bin/kops
    CLUSTER_NAME=kops-cni-test-cluster-${CLUSTER_NAME}.k8s.local
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
    --zones ${AWS_REGION}a,${AWS_REGION}b \
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
    kubectl apply -f https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/master/config/v1.6/cni-metrics-helper.yaml
}



if [[ -z "$CLUSTER_TYPE" ]]; then  
    create_eksctl_cluster 
elif [[ $CLUSTER_TYPE == "kops" ]];then
    create_kops_cluster
elif [[ $CLUSTER_TYPE == "eksctl" ]];then
    create_eksctl_cluster   
else 
   echoerr "${USAGE}\n -t optional type flag can be eksctl or kops \n"
   exit 1
fi
