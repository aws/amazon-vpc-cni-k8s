# amazon-vpc-cni-k8s
Networking plugin for pod networking in [Kubernetes](https://kubernetes.io/) using [Elastic Network Interfaces](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html) on AWS.

## Status

**Alpha**  This is an experimental release as part of the [Amazon EKS](https://aws.amazon.com/eks/) Preview.
Interfaces and functionality may change. Expect bugs (and please help us squash them).
DO NOT use for production workloads. 

## Installing

```
REPO_PATH=<path-to-this-repo>
kubectl apply -f $REPO_PATH/misc/aws-k8s-cni.yaml
```

## Components

  There are 2 components:
  
  * [CNI Plugin](https://kubernetes.io/docs/concepts/cluster-administration/network-plugins/#cni), which will wire up host's and pod's network stack when called.
  * L-IPAM, which is a long running node-Local IP Address Management (IPAM) daemon, is responsible for:
  	* maintaining a warm-pool of available IP addresses, and
  	* assigning an IP address to a Pod.

The details can be found in [Proposal: CNI plugin for Kubernetes networking over AWS VPC](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/proposals/cni-proposal.md)
   
## Requirements

* kubelets must be started with --network-plugin=cni and have --cni-conf-dir and --cni-bin-dir properly set. 
	* In aws-k8s-cni.yaml, the following defaults are configured:
		*  --cni-conf-dir=/etc/cni/net.d
		*  --cni-bin-dir=/opt/cni/bin

* kubeletes must also explicit specify using primary IPv4 address on the Primary ENI as its node-ip, for example:
 `--node-ip=$(curl http://169.254.169.254/latest/meta-data/local-ipv4)`

* L-IPAM requires following [IAM policy](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html):

 ```       
 {
     "Effect": "Allow",
     "Action": [
         "ec2:CreateNetworkInterface",
         "ec2:AttachNetworkInterface",
         "ec2:DeleteNetworkInterface",
         "ec2:DetachNetworkInterface",
         "ec2:DescribeNetworkInterfaces",
         "ec2:DescribeInstances",
         "ec2:ModifyNetworkInterfaceAttribute",
         "ec2:AssignPrivateIpAddresses"
     ],
     "Resource": [
         "*"
     ]
 },
 {
     "Effect": "Allow",
     "Action": "tag:TagResources",
     "Resource": "*"
 },
```       
   




