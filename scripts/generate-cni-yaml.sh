#!/bin/bash
set -euo pipefail

SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"

PLATFORM=$(uname | tr '[:upper:]' '[:lower:]')
HELM_VERSION="3.12.3"
NAMESPACE="kube-system"

MAKEFILEPATH=$SCRIPTPATH/../Makefile
VPC_CNI_VERSION="v1.15.0"
NODE_AGENT_VERSION="v1.0.2"
BUILD_DIR=$SCRIPTPATH/../build/cni-rel-yamls/$VPC_CNI_VERSION

REGIONS_FILE=$SCRIPTPATH/../charts/regions.json
CNI_RESOURCES_YAML=$BUILD_DIR/aws-k8s-cni
METRICS_RESOURCES_YAML=$BUILD_DIR/cni-metrics-helper

mkdir -p $BUILD_DIR

USAGE=$(cat << 'EOM'
  Usage: generate-cni-yaml  [-n <K8s_NAMESPACE>]
  Generates the kubernetes yaml resource files from the helm chart
  and places them into the build dir.
  Example: generate-cni-yaml -n kube-system
          Optional:
            -n          Kubernetes namespace
EOM
)

# Process our input arguments
while getopts "vn:" opt; do
  case ${opt} in
    n ) # K8s namespace
        NAMESPACE=$OPTARG
      ;;
    v ) # Verbose
        set -x
      ;;
    \? )
        echo "$USAGE" 1>&2
        exit
      ;;
  esac
done

curl -L https://get.helm.sh/helm-v$HELM_VERSION-$PLATFORM-amd64.tar.gz | tar zxf - -C $BUILD_DIR
mv $BUILD_DIR/$PLATFORM-amd64/helm $BUILD_DIR/.
rm -rf $BUILD_DIR/$PLATFORM-amd64
chmod +x $BUILD_DIR/helm

jq -c '.[]' $REGIONS_FILE | while read i; do
    ecrRegion=`echo $i | jq '.ecrRegion' -r`
    ecrAccount=`echo $i | jq '.ecrAccount' -r`
    ecrDomain=`echo $i | jq '.ecrDomain' -r`
    if [ "$ecrRegion" = "us-west-2" ]; then
        NEW_CNI_RESOURCES_YAML="${CNI_RESOURCES_YAML}.yaml"
        NEW_METRICS_RESOURCES_YAML="${METRICS_RESOURCES_YAML}.yaml"
    elif [ "$ecrRegion" = "cn-northwest-1" ]; then
        NEW_CNI_RESOURCES_YAML="${CNI_RESOURCES_YAML}-cn.yaml"
        NEW_METRICS_RESOURCES_YAML="${METRICS_RESOURCES_YAML}-cn.yaml"
    else
        NEW_CNI_RESOURCES_YAML="${CNI_RESOURCES_YAML}-${ecrRegion}.yaml"
        NEW_METRICS_RESOURCES_YAML="${METRICS_RESOURCES_YAML}-${ecrRegion}.yaml"
    fi

    $BUILD_DIR/helm template aws-vpc-cni \
      --include-crds \
      --set originalMatchLabels=true \
      --set init.image.region=$ecrRegion \
      --set-string init.image.account=$ecrAccount \
      --set init.image.domain=$ecrDomain \
      --set init.image.tag=$VPC_CNI_VERSION \
      --set image.tag=$VPC_CNI_VERSION \
      --set image.region=$ecrRegion \
      --set-string image.account=$ecrAccount \
      --set image.domain=$ecrDomain  \
      --set-string nodeAgent.image.account=$ecrAccount \
      --set nodeAgent.image.region=$ecrRegion \
      --set nodeAgent.image.domain=$ecrDomain \
      --set nodeAgent.image.tag=$NODE_AGENT_VERSION \
      --set env.VPC_CNI_VERSION=$VPC_CNI_VERSION \
      --namespace $NAMESPACE \
      $SCRIPTPATH/../charts/aws-vpc-cni > $NEW_CNI_RESOURCES_YAML
    # Remove 'managed-by: Helm' annotation
    sed -i '/helm.sh\|app.kubernetes.io\/managed-by: Helm/d' $NEW_CNI_RESOURCES_YAML

    $BUILD_DIR/helm template cni-metrics-helper \
      --set image.region=$ecrRegion \
      --set-string image.account=$ecrAccount \
      --set image.domain=$ecrDomain \
      --set image.tag=$VPC_CNI_VERSION \
      --namespace $NAMESPACE \
      $SCRIPTPATH/../charts/cni-metrics-helper > $NEW_METRICS_RESOURCES_YAML
    # Remove 'managed-by: Helm' annotation
    sed -i '/helm.sh\|app.kubernetes.io\/managed-by: Helm/d' $NEW_METRICS_RESOURCES_YAML
done

cd $SCRIPTPATH

echo "Generated aws-vpc-cni and cni-metrics-helper manifest resources files in:"
echo "    - $CNI_RESOURCES_YAML"
echo "    - $METRICS_RESOURCES_YAML"
