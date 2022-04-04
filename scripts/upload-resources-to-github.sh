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
CNI_RESOURCES_YAML=$BUILD_DIR/aws-k8s-cni
METRICS_RESOURCES_YAML=$BUILD_DIR/cni-metrics-helper
CALICO_RESOURCES_YAML=$BUILD_DIR/calico.yaml
CALICO_OPERATOR_RESOURCES_YAML=$BUILD_DIR/calico-operator.yaml
CALICO_CRS_RESOURCES_YAML=$BUILD_DIR/calico-crs.yaml
REGIONS_FILE=$SCRIPTPATH/../charts/regions.json

BINARIES_ONLY="false"
PR_ID=$(uuidgen | cut -d '-' -f1)
BINARY_BASE="aws-vpc-cni-k8s"

REPO="aws/amazon-vpc-cni-k8s"
GH_CLI_VERSION="0.10.1"
GH_CLI_CONFIG_PATH="${HOME}/.config/gh/config.yml"
KERNEL=$(uname -s | tr '[:upper:]' '[:lower:]')
OS="${KERNEL}"
if [[ "${KERNEL}" == "darwin" ]]; then 
  OS="macOS"
fi

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

RELEASE_BRANCH=$(curl -s -H "Authorization: token $GITHUB_TOKEN" \
    https://api.github.com/repos/aws/amazon-vpc-cni-k8s/releases | \
    jq -r --arg VERSION "$VERSION" '.[] | select(.tag_name==$VERSION) | .target_commitish')

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
        echo "✅ Created asset ID $asset_id successfully"
    else
        echo -e "❌ Upload failed with response code $response_code and message \n$response_content ❌"
        exit 1
    fi
}

RESOURCES_TO_UPLOAD=("$CALICO_OPERATOR_RESOURCES_YAML" "$CALICO_CRS_RESOURCES_YAML" "$CNI_TAR_RESOURCES_FILE" "$METRICS_TAR_RESOURCES_FILE" "$CALICO_TAR_RESOURCES_FILE")
RESOURCES_TO_COPY=("$CALICO_OPERATOR_RESOURCES_YAML" "$CALICO_CRS_RESOURCES_YAML")

COUNT=1
echo -e "\nUploading release assets for release id '$RELEASE_ID' to Github"
for asset in ${RESOURCES_TO_UPLOAD[@]}; do
    name=$(echo $asset | tr '/' '\n' | tail -1)
    echo -e "\n  $((COUNT++)). $name"
    upload_asset $asset
done

while read i; do
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
    RESOURCES_TO_COPY=(${RESOURCES_TO_COPY[@]} "$NEW_CNI_RESOURCES_YAML" "$NEW_METRICS_RESOURCES_YAML")
    COUNT=1
    echo -e "\nUploading release assets for release id '$RELEASE_ID' to Github"
    for asset in ${RESOURCES_TO_UPLOAD[@]}; do
        name=$(echo $asset | tr '/' '\n' | tail -1)
        echo -e "\n  $((COUNT++)). $name"
        upload_asset $asset
    done
done < <(jq -c '.[]' $REGIONS_FILE)

echo "✅ Attach artifacts to release page done"

echo $REPO

if [[ -z $(command -v gh) ]] || [[ ! $(gh --version) =~ $GH_CLI_VERSION ]]; then
  mkdir -p "${BUILD_DIR}"/gh
  curl -Lo "${BUILD_DIR}"/gh/gh.tar.gz "https://github.com/cli/cli/releases/download/v${GH_CLI_VERSION}/gh_${GH_CLI_VERSION}_${OS}_amd64.tar.gz"
  tar -C "${BUILD_DIR}"/gh -xvf "${BUILD_DIR}/gh/gh.tar.gz"
  export PATH="${BUILD_DIR}/gh/gh_${GH_CLI_VERSION}_${OS}_amd64/bin:$PATH"
  if [[ ! $(gh --version) =~ $GH_CLI_VERSION ]]; then
    echo "❌ Failed install of github cli"
    exit 4
  fi
fi

function fail() {
  echo "❌ Create PR failed"
  exit 5
}

CLONE_DIR="${BUILD_DIR}/config-sync"
SYNC_DIR="$CLONE_DIR"
echo $SYNC_DIR
rm -rf "${SYNC_DIR}"
mkdir -p "${SYNC_DIR}"
cd "${SYNC_DIR}"
gh repo clone aws/amazon-vpc-cni-k8s
DEFAULT_BRANCH=$(git rev-parse --abbrev-ref HEAD | tr -d '\n')
CONFIG_DIR=amazon-vpc-cni-k8s/config/master
cd $CONFIG_DIR
REPO_NAME=$(echo ${REPO} | cut -d'/' -f2)
git remote set-url origin https://"${GITHUB_USERNAME}":"${GITHUB_TOKEN}"@github.com/"${GITHUB_USERNAME}"/"${REPO_NAME}".git

git config user.name "eks-bot"
git config user.email "eks-bot@users.noreply.github.com"

FORK_RELEASE_BRANCH="${BINARY_BASE}-${VERSION}-${PR_ID}"
git checkout -b "${FORK_RELEASE_BRANCH}" origin

COUNT=1
for asset in ${RESOURCES_TO_COPY[@]}; do
        name=$(echo $asset | tr '/' '\n' | tail -1)
        echo -e "\n  $((COUNT++)). $name"
        cp "$asset" .
done

git add --all
git commit -m "${BINARY_BASE}: ${VERSION}"

PR_BODY=$(cat << EOM
## ${BINARY_BASE} ${VERSION} Automated manifest folder Sync! 🤖🤖

### Description 📝

Updating all the generated release artifacts in master/config for master branch.

EOM
)

git push -u origin "${FORK_RELEASE_BRANCH}"
gh pr create --title "🥳 ${BINARY_BASE} ${VERSION} Automated manifest sync! 🥑" \
    --body "${PR_BODY}" --repo ${REPO}

echo "✅ Manifest folder PR created for master"

CLONE_DIR="${BUILD_DIR}/config-sync-release"
SYNC_DIR="$CLONE_DIR"
echo $SYNC_DIR
rm -rf "${SYNC_DIR}"
mkdir -p "${SYNC_DIR}"
cd "${SYNC_DIR}"
gh repo clone aws/amazon-vpc-cni-k8s
echo "Release branch $RELEASE_BRANCH"
CONFIG_DIR=amazon-vpc-cni-k8s/config/master
cd $CONFIG_DIR
REPO_NAME=$(echo ${REPO} | cut -d'/' -f2)
git remote set-url origin https://"${GITHUB_USERNAME}":"${GITHUB_TOKEN}"@github.com/"${GITHUB_USERNAME}"/"${REPO_NAME}".git

git config user.name "eks-bot"
git config user.email "eks-bot@users.noreply.github.com"

FORK_RELEASE_BRANCH="${BINARY_BASE}-${VERSION}-${PR_ID}"
git checkout -b "${FORK_RELEASE_BRANCH}" origin/$RELEASE_BRANCH

COUNT=1
for asset in ${RESOURCES_TO_COPY[@]}; do
        name=$(echo $asset | tr '/' '\n' | tail -1)
        echo -e "\n  $((COUNT++)). $name"
        cp "$asset" .
done

git add --all
git commit -m "${BINARY_BASE}: ${VERSION}"

PR_BODY=$(cat << EOM
## ${BINARY_BASE} ${VERSION} Automated manifest folder Sync! 🤖🤖

### Description 📝

Updating all the generated release artifacts in master/config for $RELEASE_BRANCH branch.

EOM
)

git push -u origin "${FORK_RELEASE_BRANCH}":$RELEASE_BRANCH
gh pr create --title "🥳 ${BINARY_BASE} ${VERSION} Automated manifest sync! 🥑" \
    --body "${PR_BODY}" --repo ${REPO} --base ${RELEASE_BRANCH}

echo "✅ Manifest folder PR created for $RELEASE_BRANCH"