#!/usr/bin/env bash

check_aws_credentials() {
    aws sts get-caller-identity --query "Account" >/dev/null 2>&1 ||
        ( echo "No AWS credentials found. Please run \`aws configure\` to set up the CLI for your credentials." && exit 1)
}

check_ecr_repo_exists() {
    local __registry_account_id="$1"
    local __repo_name="$2"
    aws ecr describe-repositories --registry-id "$__registry_account_id" --repository-names "$__repo_name" >/dev/null 2>&1 ||
        ( echo "Could not find ECR repo with name $__repo_name in registry account $__registry_account_id" && exit 1 )
}

ensure_aws_k8s_tester() {
    TESTER_DOWNLOAD_URL=https://github.com/aws/aws-k8s-tester/releases/download/v0.4.3/aws-k8s-tester-v0.4.3-$OS-$ARCH

    # Download aws-k8s-tester if not yet
    if [[ ! -e $TESTER_PATH ]]; then
      mkdir -p $TESTER_DIR
      echo "Downloading aws-k8s-tester from $TESTER_DOWNLOAD_URL to $TESTER_PATH"
      curl -s -L -X GET $TESTER_DOWNLOAD_URL -o $TESTER_PATH
      chmod +x $TESTER_PATH
    fi
}
