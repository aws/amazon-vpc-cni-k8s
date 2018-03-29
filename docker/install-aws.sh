echo "=====Starting installing AWS-CNI ========="
sed -i s/__VETHPREFIX__/${EKS_CNI_VETHPREFIX:-"eni"}/g /app/aws.conf
cp /app/aws-cni /host/opt/cni/bin/
cp /app/aws-cni-support.sh /host/opt/cni/bin/
cp /app/aws.conf /host/etc/cni/net.d/
echo "=====Starting amazon-k8s-agent ==========="
exec /app/aws-k8s-agent
