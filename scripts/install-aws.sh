#!/usr/bin/env bash

set -uo pipefail

grpcHealthCheck () {
  /app/grpc_health_probe -addr 127.0.0.1:50051
}

waitIPamDServing () {
   until grpcHealthCheck; do
    echo "Waiting for ipamd health check";
    sleep 1;
  done
}

main () {
  # Remove old config files that might have been baked into older AMIs
  if [[ -f /host/etc/cni/net.d/aws.conf ]]; then
    rm /host/etc/cni/net.d/aws.conf
  fi

  # Copy standard files
  cp /app/aws-cni-support.sh /host/opt/cni/bin/
  cp /app/portmap /host/opt/cni/bin/

  echo "===== Starting amazon-k8s-agent ==========="
  /app/aws-k8s-agent &

  # Check ipamd health
  echo "Checking if ipamd is serving"
  waitIPamDServing

  echo "===== Copying AWS CNI plugin and config ========="
  sed -i s/__VETHPREFIX__/"${AWS_VPC_K8S_CNI_VETHPREFIX:-"eni"}"/g /app/10-aws.conflist
  cp /app/aws-cni /host/opt/cni/bin/
  cp /app/10-aws.conflist /host/etc/cni/net.d/

  # Check that ipamd is healthy, exit if the check fails
  while grpcHealthCheck; do
      sleep 10;
  done
}

main
