#!/bin/bash
set -euo pipefail

SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"

PLATFORM=$(uname | tr '[:upper:]' '[:lower:]')
HELM_VERSION="3.6.3"
NAMESPACE="kube-system"

MAKEFILEPATH=$SCRIPTPATH/../Makefile
VERSION=$(make -s -f $MAKEFILEPATH version)
BUILD_DIR=$SCRIPTPATH/../build/cni-rel-yamls/$VERSION

REGIONS_FILE=$SCRIPTPATH/../charts/regions.json
INDV_RESOURCES_DIR=$BUILD_DIR/individual-resources
CNI_TAR_RESOURCES_FILE=$BUILD_DIR/cni_individual-resources.tar
METRICS_TAR_RESOURCES_FILE=$BUILD_DIR/cni_metrics_individual-resources.tar
CALICO_TAR_RESOURCES_FILE=$BUILD_DIR/calico_individual-resources.tar
CNI_RESOURCES_YAML=$BUILD_DIR/aws-k8s-cni
METRICS_RESOURCES_YAML=$BUILD_DIR/cni-metrics-helper
CALICO_OPERATOR_RESOURCES_YAML=$BUILD_DIR/calico-operator.yaml
CALICO_CRS_RESOURCES_YAML=$BUILD_DIR/calico-crs.yaml

mkdir -p $INDV_RESOURCES_DIR


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
      --set originalMatchLabels=true,\
      --set init.image.region=$ecrRegion,\
      --set init.image.account=$ecrAccount,\
      --set init.image.domain=$ecrDomain,\
      --set init.image.tag=$VERSION,\
      --set image.tag=$VERSION,\
      --set image.region=$ecrRegion,\
      --set image.account=$ecrAccount,\
      --set image.domain=$ecrDomain \
      --namespace $NAMESPACE \
      $SCRIPTPATH/../charts/aws-vpc-cni > $NEW_CNI_RESOURCES_YAML
    cat $NEW_CNI_RESOURCES_YAML | grep -v 'helm.sh\|app.kubernetes.io/managed-by: Helm' > $BUILD_DIR/helm_annotations_removed.yaml
    mv $BUILD_DIR/helm_annotations_removed.yaml $NEW_CNI_RESOURCES_YAML

    $BUILD_DIR/helm template cni-metrics-helper \
      --set image.region=$ecrRegion,\
      --set image.account=$ecrAccount,\
      --set image.domain=$ecrDomain \
      --set image.tag=$VERSION,\
      --namespace $NAMESPACE \
      $SCRIPTPATH/../charts/cni-metrics-helper > $NEW_METRICS_RESOURCES_YAML
    cat $NEW_METRICS_RESOURCES_YAML | grep -v 'helm.sh\|app.kubernetes.io/managed-by: Helm' > $BUILD_DIR/helm_annotations_removed.yaml
    mv $BUILD_DIR/helm_annotations_removed.yaml $NEW_METRICS_RESOURCES_YAML
done    

$BUILD_DIR/helm template \
    --namespace $NAMESPACE \
    --output-dir $INDV_RESOURCES_DIR/ \
    $SCRIPTPATH/../charts/aws-vpc-cni/

for i in $INDV_RESOURCES_DIR/aws-vpc-cni/templates/*; do
  cat $i | grep -v 'helm.sh\|app.kubernetes.io/managed-by: Helm' > $BUILD_DIR/helm_annotations_removed.yaml
  mv $BUILD_DIR/helm_annotations_removed.yaml $i
done

$BUILD_DIR/helm template \
    --namespace $NAMESPACE \
    --output-dir $INDV_RESOURCES_DIR/ \
    $SCRIPTPATH/../charts/cni-metrics-helper/

for i in $INDV_RESOURCES_DIR/cni-metrics-helper/templates/*; do
  cat $i | grep -v 'helm.sh\|app.kubernetes.io/managed-by: Helm' > $BUILD_DIR/helm_annotations_removed.yaml
  mv $BUILD_DIR/helm_annotations_removed.yaml $i
done

$BUILD_DIR/helm template \
    --namespace $NAMESPACE \
    $SCRIPTPATH/../charts/aws-calico/ \
    --output-dir $INDV_RESOURCES_DIR/


for i in $INDV_RESOURCES_DIR/aws-calico/templates/crs/*; do
  cat $i | grep -v 'app.kubernetes.io/managed-by: Helm' > $BUILD_DIR/helm_annotations_removed.yaml
  mv $BUILD_DIR/helm_annotations_removed.yaml $i
  cat $i >> $CALICO_CRS_RESOURCES_YAML
done

for i in $INDV_RESOURCES_DIR/aws-calico/templates/crds/*; do
  cat $i | grep -v 'helm.sh\|app.kubernetes.io/managed-by: Helm' > $BUILD_DIR/helm_annotations_removed.yaml
  mv $BUILD_DIR/helm_annotations_removed.yaml $i
  cat $i >> $CALICO_OPERATOR_RESOURCES_YAML
done

for i in $INDV_RESOURCES_DIR/aws-calico/templates/tigera-operator/*; do
  cat $i | grep -v 'helm.sh\|app.kubernetes.io/managed-by: Helm' > $BUILD_DIR/helm_annotations_removed.yaml
  mv $BUILD_DIR/helm_annotations_removed.yaml $i
  cat $i >> $CALICO_OPERATOR_RESOURCES_YAML
done

cd $INDV_RESOURCES_DIR/aws-vpc-cni/ && tar cvf $CNI_TAR_RESOURCES_FILE templates/*
cd $INDV_RESOURCES_DIR/cni-metrics-helper/ && tar cvf $METRICS_TAR_RESOURCES_FILE templates/*
cd $INDV_RESOURCES_DIR/aws-calico/ && tar cvf $CALICO_TAR_RESOURCES_FILE templates/*
cd $SCRIPTPATH

echo "Generated aws-vpc-cni, cni-metrics-helper and calico yaml resources files in:"
echo "    - $CNI_RESOURCES_YAML"
echo "    - $METRICS_RESOURCES_YAML"
echo "    - $CALICO_OPERATOR_RESOURCES_YAML"
echo "    - $CALICO_CRS_RESOURCES_YAML"
echo "    - $CNI_TAR_RESOURCES_FILE"
echo "    - $METRICS_TAR_RESOURCES_FILE"
echo "    - $CALICO_TAR_RESOURCES_FILE"