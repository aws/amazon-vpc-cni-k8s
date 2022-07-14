#!/bin/bash
set -euo pipefail
set +x


SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"
BUILD_DIR="${SCRIPTPATH}/../build"

VERSION=$(make -s -f $SCRIPTPATH/../Makefile version)

REPO="aws/amazon-vpc-cni-k8s"
PR_ID=$(uuidgen | cut -d '-' -f1)

SYNC_DIR="${BUILD_DIR}/config-sync"

BINARY_BASE=""
INCLUDE_NOTES=0
MANUAL_VERIFY=1

GH_CLI_VERSION="0.10.1"
GH_CLI_CONFIG_PATH="${HOME}/.config/gh/config.yml"
KERNEL=$(uname -s | tr '[:upper:]' '[:lower:]')
OS="${KERNEL}"
if [[ "${KERNEL}" == "darwin" ]]; then 
  OS="macOS"
fi

VERSION=$(make -s -f "${SCRIPTPATH}/../Makefile" version)

USAGE=$(cat << EOM
  Usage: sync-to-config-folder  -r <repo>
  Updates config folder in master and release branch
  Example: sync-to-config-folder -r "${REPO}"
          Optional:
            -r          Github repo to sync to in the form of "org/name"  (i.e. -r "${REPO}")
            -n          Include application release notes in the sync PR
            -y          Without asking for manual confirmation before creating PR to upstream
EOM
)

# Process our input arguments
while getopts r:n:y opt; do
  case "${opt}" in
    r ) # Github repo
        REPO="$OPTARG"
      ;;
    n ) # Include release notes
        INCLUDE_NOTES=1
      ;;
    y ) # Manual verify
        MANUAL_VERIFY=0
      ;;
    \? )
        echo "$USAGE" 1>&2
        exit
      ;;
  esac
done

if [[ -z "${REPO}" ]]; then 
  echo "Repo (-r) must be specified if no \"make repo-full-name\" target exists"
fi

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

function restore_gh_config() {
  mv -f "${GH_CLI_CONFIG_PATH}.bkup" "${GH_CLI_CONFIG_PATH}" || :
}

if [[ -n $(env | grep GITHUB_TOKEN) ]] && [[ -n "${GITHUB_TOKEN}" ]]; then
  trap restore_gh_config EXIT INT TERM ERR
  mkdir -p "${HOME}/.config/gh"
  cp -f "${GH_CLI_CONFIG_PATH}" "${GH_CLI_CONFIG_PATH}.bkup" || :
  cat << EOF > "${GH_CLI_CONFIG_PATH}"
hosts:
    github.com:
        oauth_token: ${GITHUB_TOKEN}
        user: ${GITHUB_USERNAME}
EOF
fi

function fail() {
  echo "❌ Config folders sync failed"
  exit 5
}

RELEASE_BRANCH=$(curl -s -H "Authorization: token $GITHUB_TOKEN" \
    https://api.github.com/repos/aws/amazon-vpc-cni-k8s/releases | \
    jq -r --arg VERSION "$VERSION" '.[] | select(.tag_name==$VERSION) | .target_commitish')

make -s -f $SCRIPTPATH/../Makefile generate-cni-yaml

rm -rf "${SYNC_DIR}"
mkdir -p "${SYNC_DIR}"
cd "${SYNC_DIR}"

gh repo clone aws/amazon-vpc-cni-k8s
DEFAULT_BRANCH=$(git rev-parse --abbrev-ref HEAD | tr -d '\n')

CONFIG_DIR=amazon-vpc-cni-k8s/config/master
cd $CONFIG_DIR
REPO_NAME=$(echo ${REPO} | cut -d'/' -f2)
git remote set-url origin https://"${GITHUB_USERNAME}":"${GITHUB_TOKEN}"@github.com/"${GITHUB_USERNAME}"/"${REPO_NAME}".git

git config user.name "eks-networking-bot"
git config user.email "eks-networking-bot@users.noreply.github.com"

FORK_RELEASE_BRANCH="${BINARY_BASE}-${VERSION}-${PR_ID}"
git checkout -b "${FORK_RELEASE_BRANCH}" origin/master

cp $SCRIPTPATH/../build/cni-rel-yamls/${VERSION}/aws-k8s-cni*.yaml .
cp $SCRIPTPATH/../build/cni-rel-yamls/${VERSION}/cni-metrics-helper*.yaml .

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

git config user.name "eks-networking-bot"
git config user.email "eks-networking-bot@users.noreply.github.com"

FORK_RELEASE_BRANCH="${BINARY_BASE}-${VERSION}-${PR_ID}"
git checkout -b "${FORK_RELEASE_BRANCH}" origin/$RELEASE_BRANCH

cp $SCRIPTPATH/../build/cni-rel-yamls/${VERSION}/aws-k8s-cni*.yaml .
cp $SCRIPTPATH/../build/cni-rel-yamls/${VERSION}/cni-metrics-helper*.yaml .

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
