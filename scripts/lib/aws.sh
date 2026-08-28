#!/usr/bin/env bash

check_aws_credentials() {
    aws sts get-caller-identity --query "Account" ||
        ( echo "No AWS credentials found. Please run \`aws configure\` to set up the CLI for your credentials." && exit 1)
}

ensure_ecr_repo() {
    echo "Ensuring that $2 exists for account $1"
    local __registry_account_id="$1"
    local __repo_name="$2"
    local __region="$3"
    if ! `aws ecr describe-repositories --registry-id "$__registry_account_id" --repository-names "$__repo_name" --region "$__region" >/dev/null 2>&1`; then
        echo "creating ECR repo with name $__repo_name in registry account $__registry_account_id"
        aws ecr create-repository --repository-name "$__repo_name" --region "$__region"
    fi
}

emit_cloudwatch_metric() {
    aws cloudwatch put-metric-data --metric-name $1 --namespace TestExecution --unit None --value $2 --region $AWS_DEFAULT_REGION
}

