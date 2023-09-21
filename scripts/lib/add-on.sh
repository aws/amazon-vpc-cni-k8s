#!/bin/bash

# Helper script for performing operations on vpc-cni addons

VPC_CNI_ADDON_NAME="vpc-cni"

function load_addon_details() {
  echo "loading $VPC_CNI_ADDON_NAME addon details"
  DESCRIBE_ADDON_VERSIONS=$(aws eks describe-addon-versions $ENDPOINT_FLAG --addon-name $VPC_CNI_ADDON_NAME --kubernetes-version "$K8S_VERSION" --region $REGION)

  LATEST_ADDON_VERSION=$(echo "$DESCRIBE_ADDON_VERSIONS" | jq '.addons[0].addonVersions[0].addonVersion' -r)
  DEFAULT_ADDON_VERSION=$(echo "$DESCRIBE_ADDON_VERSIONS" | jq -r '.addons[].addonVersions[] | select(.compatibilities[0].defaultVersion == true) | .addonVersion')
  EXISTING_SERVICE_ACCOUNT_ROLE_ARN=$(kubectl get serviceaccount -n kube-system aws-node -o json | jq '.metadata.annotations."eks.amazonaws.com/role-arn"' -r)
}

function wait_for_addon_status() {
  local expected_status=$1
  local retry_attempt=0
  if [ "$expected_status" =  "DELETED" ]; then
    while $(aws eks describe-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME --region "$REGION"); do
      if [ $retry_attempt -ge 30 ]; then
        echo "failed to delete addon, qutting after too many attempts"
        exit 1
      fi
      echo "addon is still not deleted"
      sleep 5
      ((retry_attempt=retry_attempt+1))
    done
    echo "addon deleted"
    # Even after the addon API Returns an error, if you immediately try to create a new addon it sometimes fails
    sleep 10
    return
  fi

  retry_attempt=0
  while true
  do
    STATUS=$(aws eks describe-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME --region "$REGION" | jq -r '.addon.status')
    if [ "$STATUS" = "$expected_status" ]; then
      echo "addon status matches expected status"
      return
    fi
    # We have seen the canary test get stuck, we don't know what causes this but most likely suspect is
    # the addon update/delete attempts. So adding limited retries.
    if [ $retry_attempt -ge 30 ]; then
      echo "failed to get desired add-on status: $STATUS, qutting after too many attempts"
      exit 1
    fi
    echo "addon status is not equal to $expected_status"
    sleep 10
    ((retry_attempt=retry_attempt+1))
  done
}

function patch_aws_node_maxunavialable() {
   # Patch the aws-node, so any update in aws-node happens in parallel for faster overall test execution
   local retry=0
   while [ $retry -le 10 ]; do
     if kubectl patch ds -n kube-system aws-node -p '{"spec":{"updateStrategy":{"rollingUpdate":{"maxUnavailable": "100%"}}}}'; then
       break
     else
       # It's possible the ds is not yet created by the Addon APIs, so have couple of retries
       echo "failed to patch ds, will retry"
       sleep 5
       ((retry=retry+1))
     fi
   done
}

function install_add_on() {
  local new_addon_version=$1
  if [[ -z "$new_addon_version" || "$new_addon_version" == "null" ]]; then
    echo "addon information for $VPC_CNI_ADDON_NAME not available, skipping EKS-managed addon installation."
    return
  fi

  if DESCRIBE_ADDON=$(aws eks describe-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME --region "$REGION"); then
    local current_addon_version=$(echo "$DESCRIBE_ADDON" | jq '.addon.addonVersion' -r)
    if [ "$new_addon_version" != "$current_addon_version" ]; then
      echo "deleting the $current_addon_version to install $new_addon_version"
      aws eks delete-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name "$VPC_CNI_ADDON_NAME" --region "$REGION"
      wait_for_addon_status "DELETED"
    else
      echo "addon version $current_addon_version already installed"
      patch_aws_node_maxunavialable
      return
    fi
  fi

  echo "installing addon $new_addon_version"

  # If installing the addon after deleting it once, we should retain the Service Account Role ARN of the Addon
  if [ "$EXISTING_SERVICE_ACCOUNT_ROLE_ARN" != "null" ]; then
     SA_ROLE_ARN_ARG="--service-account-role-arn $EXISTING_SERVICE_ACCOUNT_ROLE_ARN"
  fi

  aws eks create-addon $ENDPOINT_FLAG $SA_ROLE_ARN_ARG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME --resolve-conflicts OVERWRITE --addon-version $new_addon_version --region "$REGION"
  patch_aws_node_maxunavialable
  wait_for_addon_status "ACTIVE"
}
