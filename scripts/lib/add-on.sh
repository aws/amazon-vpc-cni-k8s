#!/bin/bash

# Helper script for performing operations on vpc-cni addons

function load_addon_details() {
  echo "loading $VPC_CNI_ADDON_NAME addon details"
  DESCRIBE_ADDON_VERSIONS=$(aws eks describe-addon-versions --addon-name $VPC_CNI_ADDON_NAME --kubernetes-version "$K8S_VERSION")

  LATEST_ADDON_VERSION=$(echo "$DESCRIBE_ADDON_VERSIONS" | jq '.addons[0].addonVersions[0].addonVersion' -r)
  DEFAULT_ADDON_VERSION=$(echo "$DESCRIBE_ADDON_VERSIONS" | jq -r '.addons[].addonVersions[] | select(.compatibilities[0].defaultVersion == true) | .addonVersion')
}

function wait_for_addon_status() {
  local expected_status=$1
  local retry_attempt=0
  if [ "$expected_status" =  "DELETED" ]; then
    while $(aws eks describe-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME --region "$REGION"); do
      if [ $retry_attempt -ge 20 ]; then
        echo "failed to delete addon, qutting after too many attempts"
        exit 1
      fi
      echo "addon is still not deleted"
      sleep 5
      ((retry_attempt=retry_attempt+1))
    done
    echo "addon deleted"
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
    sleep 5
    ((retry_attempt=retry_attempt+1))
  done
}

function patch_aws_node_maxunavialable() {
   # Patch the aws-node, so any update in aws-node happens parallely for faster overall test execution
   kubectl patch ds -n kube-system aws-node -p '{"spec":{"updateStrategy":{"rollingUpdate":{"maxUnavailable": "100%"}}}}'
}

function install_add_on() {
  local new_addon_version=$1

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
  aws eks create-addon $ENDPOINT_FLAG --cluster-name "$CLUSTER_NAME" --addon-name $VPC_CNI_ADDON_NAME --resolve-conflicts OVERWRITE --addon-version $new_addon_version --region "$REGION"
  patch_aws_node_maxunavialable
  wait_for_addon_status "ACTIVE"
}
