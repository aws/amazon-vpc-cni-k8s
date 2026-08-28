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

ensure_ecr_image_exists() {
    local image=$1
    local registry=${image%%/*}
    local repository_and_tag=${image#*/}
    local repository=${repository_and_tag%:*}
    local tag=${repository_and_tag##*:}
    local registry_id=${registry%%.*}
    local region
    local image_count

    if [[ ! "$registry" =~ ^[0-9]{12}\.dkr\.ecr\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$ || "$repository_and_tag" == "$image" || "$repository" == "$repository_and_tag" || -z "$repository" || -z "$tag" ]]; then
        echo "Invalid ECR image reference: $image" >&2
        return 1
    fi

    region=$(echo "$registry" | cut -d. -f4)
    image_count=$(aws ecr batch-get-image \
        --registry-id "$registry_id" \
        --repository-name "$repository" \
        --image-ids "imageTag=$tag" \
        --region "$region" \
        --query 'length(images)' \
        --output text)

    if [[ "$image_count" != "1" ]]; then
        echo "Required image $image is not available" >&2
        return 1
    fi
}
