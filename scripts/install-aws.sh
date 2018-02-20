echo "=====Starting installing AWS-CNI ========="
sed -i s/__VETHPREFIX__/${AWS_VPC_K8S_CNI_VETHPREFIX:-"eni"}/g /app/aws.conf
cp /app/aws-cni /host/opt/cni/bin/
cp /app/aws-cni-support.sh /host/opt/cni/bin/
cp /app/aws.conf /host/etc/cni/net.d/
echo "=====Starting amazon-k8s-agent ==========="
/app/aws-k8s-agent
