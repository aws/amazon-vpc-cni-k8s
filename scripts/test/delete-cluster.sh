#!/usr/bin/env bash


set -eo pipefail

USAGE=$(cat << 'EOM'
Usage: delete-cluster.sh -c [cluster-config]
Deletes an EKS Cluster along with the Nodegroups. Takes in
the cluster config as an optional argument. If no arugment is
provided then it will use the cluster config in the default
path i.e /build/eks-cluster.yaml
 Optional:
 -c   eksctl Cluster config path. Defaults to build/eks-cluster.yaml
 -k   Creation of kOps Cluster. No flag defaults to false.
 -t   Type of cluster. Defaults to eksctl cluster. Can be set to "kops" or "eksctl"
EOM
)

TEST_DIR=$(cd "$(dirname "$0")" || exit 1; pwd)
CLUSTER_CONFIG_DIR="$TEST_DIR/build"
CLUSTER_CONFIG_PATH="$CLUSTER_CONFIG_DIR/eks-cluster.yaml"

source "$PWD/scripts/lib/common.sh"


check_is_installed eksctl

while getopts "c:t:" o; do
  case "${o}" in
    c) # eksctl cluster config file path
      CLUSTER_CONFIG_PATH=${OPTARG}
      ;;
    t) # Type of cluster to be deleted
      CLUSTER_TYPE=${OPTARG}
      ;;
    *)
      echoerr "${USAGE}"
      exit 1
      ;;
  esac
done

shift $((OPTIND-1))

function delete_eksctl_cluster {
    eksctl delete cluster -f "$CLUSTER_CONFIG_PATH" --wait
}

function delete_kops_cluster {
    kops delete cluster --name ${CLUSTER_NAME} --yes
    aws s3 rm ${KOPS_STATE_STORE} --recursive
    aws s3 rb ${KOPS_STATE_STORE} --region us-west-2
}


if [[ -z "$CLUSTER_TYPE" ]]; then  
    delete_eksctl_cluster 
elif [[ $CLUSTER_TYPE == "kops" ]];then
    delete_kops_cluster
elif [[ $CLUSTER_TYPE == "eksctl" ]];then
    delete_eksctl_cluster   
else 
   echoerr "${USAGE}\n -t optional type flag can be eksctl or kops \n"
   exit 1
fi
