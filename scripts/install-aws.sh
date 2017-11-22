echo "=====Starting installing AWS-CNI ========="
cp /app/aws-cni /host/opt/cni/bin/
cp /app/aws.conf /host/etc/cni/net.d/
echo "=====Starting amazon-k8s-agent ==========="
/app/aws-k8s-agent
