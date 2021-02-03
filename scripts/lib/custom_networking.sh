#This function will setup custom networking for the cluster
function configure_custom_networking(){
    local VPC_ID=$(aws eks describe-cluster --name "$CLUSTER_NAME" | grep -iwo 'vpc-[a-zA-z0-9]*' | xargs)
    echo "VPC $VPC_ID"

    local RTASSOC_ID=$(aws ec2 describe-route-tables --filters Name=vpc-id,Values=$VPC_ID Name=tag:"aws:cloudformation:logical-id",Values=PublicRouteTable | jq -r '.RouteTables[].RouteTableId')
    
    echo "RT table $RTASSOC_ID"
    aws ec2 associate-vpc-cidr-block --vpc-id $VPC_ID --cidr-block 100.64.0.0/16

    local AZ1=us-west-2a
    local AZ2=us-west-2b
    local AZ3=us-west-2c

    CUST_SNET1=$(aws ec2 create-subnet --cidr-block 100.64.0.0/19 --vpc-id $VPC_ID --availability-zone $AZ1 | jq -r .Subnet.SubnetId)
    CUST_SNET2=$(aws ec2 create-subnet --cidr-block 100.64.32.0/19 --vpc-id $VPC_ID --availability-zone $AZ2 | jq -r .Subnet.SubnetId)
    CUST_SNET3=$(aws ec2 create-subnet --cidr-block 100.64.64.0/19 --vpc-id $VPC_ID --availability-zone $AZ3 | jq -r .Subnet.SubnetId)

    echo "Subnets - $CUST_SNET1 $CUST_SNET2 $CUST_SNET3"
    aws ec2 create-tags --resources $CUST_SNET1 --tags Key=kubernetes.io/cluster/$CLUSTER_NAME,Value=shared
    aws ec2 create-tags --resources $CUST_SNET2 --tags Key=kubernetes.io/cluster/$CLUSTER_NAME,Value=shared
    aws ec2 create-tags --resources $CUST_SNET3 --tags Key=kubernetes.io/cluster/$CLUSTER_NAME,Value=shared

    aws ec2 associate-route-table --route-table-id $RTASSOC_ID --subnet-id $CUST_SNET1
    aws ec2 associate-route-table --route-table-id $RTASSOC_ID --subnet-id $CUST_SNET2
    aws ec2 associate-route-table --route-table-id $RTASSOC_ID --subnet-id $CUST_SNET3

    #Enable custom networking
    $KUBECTL_PATH set env daemonset aws-node -n kube-system AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true
    $KUBECTL_PATH set env daemonset aws-node -n kube-system ENI_CONFIG_LABEL_DEF=failure-domain.beta.kubernetes.io/zone
    echo "Sleep until all aws-nodes are available"
    while [[ $($KUBECTL_PATH describe ds aws-node -n=kube-system | grep "Available Pods: 0") ]]
    do
        sleep 5
        echo "Waiting for daemonset update"
    done
    echo "Updated!"
    

cat <<EOF  | $KUBECTL_PATH apply -f -
apiVersion: crd.k8s.amazonaws.com/v1alpha1
kind: ENIConfig
metadata:
 name: $AZ1
spec:
  subnet: $CUST_SNET1
EOF

cat <<EOF | $KUBECTL_PATH apply -f -
apiVersion: crd.k8s.amazonaws.com/v1alpha1
kind: ENIConfig
metadata:
 name: $AZ2
spec:
  subnet: $CUST_SNET2
EOF

cat <<EOF | $KUBECTL_PATH apply -f -
apiVersion: crd.k8s.amazonaws.com/v1alpha1
kind: ENIConfig
metadata:
 name: $AZ3
spec:
  subnet: $CUST_SNET3
EOF
  
  #no need to set max-pods since it is already configured while creating the ASG
  ASG_NAME="$CLUSTER_NAME-cn-mng-for-cni"
  echo $ASG_NAME
  INSTANCE_IDS=(`aws ec2 describe-instances --query 'Reservations[*].Instances[*].InstanceId' --filters "Name=tag-key,Values=Name" "Name=tag-value,Values=$ASG_NAME" --output text` )
  for i in "${INSTANCE_IDS[@]}"
  do
	    echo "Terminating EC2 instance $i ..."
	    aws ec2 terminate-instances --instance-ids $i
  done
  echo "Terminated instances successfully. Now wait to let ASG create a new instance."
  DESIRED_SIZE=(`aws autoscaling describe-auto-scaling-groups --auto-scaling-group-name $ASG_NAME --query "AutoScalingGroups[].DesiredCapacity" --output text` )
  nodes_ready=0
  while [ $nodes_ready -lt $DESIRED_SIZE ]
  do
    sleep 100
    nodes_ready=$(kubectl get nodes -o json | jq -r '.items[] | select(.status.conditions[].type=="Ready") | .metadata.name ' | wc -l)
    echo "Waiting for nodes to be ready, current status $nodes_ready and desired size $DESIRED_SIZE"
  done

}