#!/usr/bin/env bash
echo "====== Installing AWS-CNI ======"
sed -i s/__VETHPREFIX__/${AWS_VPC_K8S_CNI_VETHPREFIX:-"eni"}/g /app/10-aws.conflist
cp /app/portmap /host/opt/cni/bin/
cp /app/aws-cni-support.sh /host/opt/cni/bin/

if [[ -f /host/etc/cni/net.d/aws.conf ]]; then
    rm /host/etc/cni/net.d/aws.conf
fi

echo "====== Starting amazon-k8s-agent ======"
/app/aws-k8s-agent
