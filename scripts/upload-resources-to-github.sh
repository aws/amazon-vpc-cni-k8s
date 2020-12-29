#!/bin/bash
set -euo pipefail

# Script to upload release assets to Github.
# This script cleans up after itself in cases of parital failures. i.e. either all assets are uploaded or none
SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"
VERSION=$(make -s -f $SCRIPTPATH/../Makefile version)
BUILD_DIR=$SCRIPTPATH/../build/cni-rel-yamls/$VERSION
BINARY_DIR=$SCRIPTPATH/../build/bin
CNI_TAR_RESOURCES_FILE=$BUILD_DIR/cni_individual-resources.tar
METRICS_TAR_RESOURCES_FILE=$BUILD_DIR/cni_metrics_individual-resources.tar
CALICO_TAR_RESOURCES_FILE=$BUILD_DIR/calico_individual-resources.tar
CNI_RESOURCES_YAML=$BUILD_DIR/aws-vpc-cni
METRICS_RESOURCES_YAML=$BUILD_DIR/cni-metrics-helper
CALICO_RESOURCES_YAML=$BUILD_DIR/calico.yaml
REGIONS_FILE=$SCRIPTPATH/../charts/regions.json

BINARIES_ONLY="false"

USAGE=$(cat << 'EOM'
  Usage: upload-resources-to-github  [-b]
  Upload release assets to GitHub

  Example: upload-resources-to-github -b
          Optional:
            -b          Upload binaries only [DEFAULT: upload all the assets]
EOM
)

# Process our input arguments
while getopts "b" opt; do
  case ${opt} in
    b ) # Binaries only
        BINARIES_ONLY="true"
      ;;
    \? )
        echo "$USAGE" 1>&2
        exit
      ;;
  esac
done

RELEASE_ID=$(curl -s -H "Authorization: token $GITHUB_TOKEN" \
    https://api.github.com/repos/aws/amazon-vpc-cni-k8s/releases | \
    jq --arg VERSION "$VERSION" '.[] | select(.tag_name==$VERSION) | .id')

ASSET_IDS_UPLOADED=()

trap 'handle_errors_and_cleanup $?' EXIT

handle_errors_and_cleanup() {
    if [ $1 -eq 0 ]; then
        exit 0
    fi

    if [[ ${#ASSET_IDS_UPLOADED[@]} -ne 0 ]]; then
        echo -e "\nCleaning up assets uploaded in the current execution of the script"
        for asset_id in "${ASSET_IDS_UPLOADED[@]}"; do
            echo "Deleting asset $asset_id"
            curl -X DELETE \
              -H "Authorization: token $GITHUB_TOKEN" \
              "https://api.github.com/repos/aws/amazon-vpc-cni-k8s/releases/assets/$asset_id"
        done
        exit $1
    fi
}

# $1: absolute path to asset
upload_asset() {
    resp=$(curl --write-out '%{http_code}' --silent \
        -H "Authorization: token $GITHUB_TOKEN" \
        -H "Content-Type: $(file -b --mime-type $1)" \
        --data-binary @$1 \
        "https://uploads.github.com/repos/aws/amazon-vpc-cni-k8s/releases/$RELEASE_ID/assets?name=$(basename $1)")

    response_code=$(echo $resp | sed 's/\(.*\)}//')
    response_content=$(echo $resp | sed "s/$response_code//")

    # HTTP success code expected - 201 Created
    if [[ $response_code -eq 201 ]]; then
        asset_id=$(echo $response_content | jq '.id')
        ASSET_IDS_UPLOADED+=("$asset_id")
        echo "Created asset ID $asset_id successfully"
    else
        echo -e "❌ Upload failed with response code $response_code and message \n$response_content ❌"
        exit 1
    fi
}

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
    RESOURCES_TO_UPLOAD=("$NEW_CNI_RESOURCES_YAML" "$NEW_METRICS_RESOURCES_YAML")

    COUNT=1
    echo -e "\nUploading release assets for release id '$RELEASE_ID' to Github"
    for asset in ${RESOURCES_TO_UPLOAD[@]}; do
        name=$(echo $asset | tr '/' '\n' | tail -1)
        echo -e "\n  $((COUNT++)). $name"
        upload_asset $asset
    done
done    

RESOURCES_TO_UPLOAD=("$CALICO_RESOURCES_YAML" "$CNI_TAR_RESOURCES_FILE" "$METRICS_TAR_RESOURCES_FILE" "$CALICO_TAR_RESOURCES_FILE")

COUNT=1
echo -e "\nUploading release assets for release id '$RELEASE_ID' to Github"
for asset in ${RESOURCES_TO_UPLOAD[@]}; do
    name=$(echo $asset | tr '/' '\n' | tail -1)
    echo -e "\n  $((COUNT++)). $name"
    upload_asset $asset
done
