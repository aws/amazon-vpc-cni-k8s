# amazon-vpc-cni-k8s
Networking plugin for pod networking in [Kubernetes](https://kubernetes.io/) using [Elastic Network Interfaces](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html) on AWS.

[![BuildStatus Widget]][BuildStatus Result]
[![GoReport Widget]][GoReport Status]


[BuildStatus Result]: https://travis-ci.org/aws/amazon-vpc-cni-k8s
[BuildStatus Widget]: https://travis-ci.org/aws/amazon-vpc-cni-k8s.svg?branch=master

[GoReport Status]: https://goreportcard.com/report/github.com/aws/amazon-vpc-cni-k8s
[GoReport Widget]: https://goreportcard.com/badge/github.com/aws/amazon-vpc-cni-k8s?

## Setup
Download the latest version of the [yaml](./config/) and apply it the cluster.

```
kubectl apply -f aws-k8s-cni.yaml
```

Launch kubelet with network plugins set to cni (`--network-plugin=cni`), the cni directories configured (`--cni-config-dir` and `--cni-bin-dir`) and node ip set to the primary IPv4 address of the primary ENI for the instance (`--node-ip=$(curl http://169.254.169.254/latest/meta-data/local-ipv4)`).  It is also recommended to set `--max-pods` equal to (the number of ENIs for the instance type * (the number of IPs per ENI - 1)) + 2 [see](./pkg/awsutils/vpc_ip_resource_limit.go) to prevent scheduling that exceeds the IP resources available to the kubelet.

The default manifest expects `--cni-conf-dir=/etc/cni/net.d` and `--cni-bin-dir=/opt/cni/bin`.

L-IPAM requires following [IAM policy](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html):

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
     "Action": "ec2:CreateTags",
     "Resource": "arn:aws:ec2:*:*:network-interface/*"
 },
```

## Building

* `make` defaults to `make build-linux` that builds the Linux binaries.
* `make docker-build` uses a docker container (golang:1.10) to build the binaries.
* `make docker` will create a docker container using the docker-build with the finished binaries, with a tag of `amazon/amazon-k8s-cni:latest`
* `unit-test`, `lint` and `vet` provide ways to run the respective tests/tools and should be run before submitting a PR.

## Components

  There are 2 components:
  
  * [CNI Plugin](https://kubernetes.io/docs/concepts/cluster-administration/network-plugins/#cni), which will wire up host's and pod's network stack when called.
  * `L-IPAMD`, which is a long running node-Local IP Address Management (IPAM) daemon, is responsible for:
    * maintaining a warm-pool of available IP addresses, and
    * assigning an IP address to a Pod.

The details can be found in [Proposal: CNI plugin for Kubernetes networking over AWS VPC](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/cni-proposal.md).

[Troubleshooting Guide](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/troubleshooting.md) provides tips on how to debug and troubleshoot CNI.

## ENI Allocation

When a worker node first joins the cluster, there is only 1 ENI along with all of its addresses in the ENI. Without any configuration, ipamD always try to keep one extra ENI.

When number of pods running on the node exceeds the number of addresses on a single ENI, the CNI backend start allocating a new ENI and start using following allocation scheme:

For example, a m4.4xlarge node can have up to 8 ENIs, and each ENI can have up to 30 IP addresses. ( https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html ).   

* If the number of current running Pods is between 0 to 29, ipamD will allocate one more eni. And Warm-Pool size is 2 eni * (30 -1) = 58 
* If the number of current running Pods is between 30 and 58, ipamD will allocate 2 more eni. And Warm-Pool size is 3 eni * (30 -1) = 87

### CNI Configuration Variables<a name="cni-env-vars"></a>

The Amazon VPC CNI plugin for Kubernetes supports a number of configuration options, which are set through environment variables\. The following environment variables are available, and all of them are optional\. 

`AWS_VPC_CNI_NODE_PORT_SUPPORT`  
Type: Boolean  
Default: `true`  
Specifies whether `NodePort` services are enabled on a worker node's primary network interface\. This requires additional `iptables` rules and that the kernel's reverse path filter on the primary interface is set to `loose`\.

`AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG`  
Type: Boolean  
Default: `false`  
Specifies that your pods may use subnets and security groups that are independent of your worker node's VPC configuration\. By default, pods share the same subnet and security groups as the worker node's primary interface\. Setting this variable to `true` causes `ipamD` to use the security groups and VPC subnet in a worker node's `ENIConfig` for elastic network interface allocation\. You must create an `ENIConfig` custom resource definition for each subnet that your pods will reside in, and then annotate each worker node to use a specific `ENIConfig` \(multiple worker nodes can be annotated with the same `ENIConfig`\)\. Worker nodes can only be annotated with a single `ENIConfig` at a time, and the subnet in the `ENIConfig` must belong to the same Availability Zone that the worker node resides in\. For more information, see [https://github.com/aws/amazon-vpc-cni-k8s/pull/165](https://github.com/aws/amazon-vpc-cni-k8s/pull/165)\.

`AWS_VPC_K8S_CNI_EXTERNALSNAT`  
Type: Boolean  
Default: `false`  
Specifies whether an external NAT gateway should be used to provide SNAT of secondary ENI IP addresses\. If set to `true`, the SNAT `iptables` rule and off\-VPC IP rule are not applied, and these rules are removed if they have already been applied\.  
Disable SNAT if you need to allow inbound communication to your pods from external VPNs, direct connections, and external VPCs, and your pods do not need to access the Internet directly via an Internet Gateway\. However, your nodes must be running in a private subnet and connected to the internet through an AWS NAT Gateway or another external NAT device\.

`WARM_ENI_TARGET`  
Type: Integer  
Default: `1`  
Specifies the number of free elastic network interfaces \(and all of their available IP addresses\) that the `ipamD` daemon should attempt to keep available for pod assignment on the node\. By default, `ipamD` attempts to keep 1 elastic network interface and all of its IP addresses available for pod assignment\.  
The number of IP addresses per network interface varies by instance type\. For more information, see [IP Addresses Per Network Interface Per Instance Type](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI) in the *Amazon EC2 User Guide for Linux Instances*\. 
For example, an `m4.4xlarge` launches with 1 network interface and 30 IP addresses\. If 5 pods are placed on the node and 5 free IP addresses are removed from the IP address warm pool, then `ipamD` attempts to allocate more interfaces until `WARM_ENI_TARGET` free interfaces are available on the node\.  
If `WARM_IP_TARGET` is set, then this environment variable is ignored and the `WARM_IP_TARGET` behavior is used instead\.

`WARM_IP_TARGET`  
Type: Integer  
Default: None  
Specifies the number of free IP addresses that the `ipamD` daemon should attempt to keep available for pod assignment on the node\. For example, if `WARM_IP_TARGET` is set to 10, then `ipamD` attempts to keep 10 free IP addresses available at all times\. If the elastic network interfaces on the node are unable to provide these free addresses, `ipamD` attempts to allocate more interfaces until `WARM_IP_TARGET` free IP addresses are available\.  
This environment variable overrides `WARM_ENI_TARGET` behavior\.

### Notes

`L-IPAMD`(aws-node daemonSet) running on every worker node requires access to kubernetes API server.  If it can **not** reach kubernetes API server, ipamD will exit and CNI will not be able to get any IP address for Pods.  Here is a way to confirm if `L-IPAMD` has access to the kubernetes API server.

```

# find out kubernetes service IP, e.g. 10.0.0.1
kubectl get svc kubernetes
NAME         TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
kubernetes   ClusterIP   10.0.0.1   <none>        443/TCP   29d

# ssh into worker node, check if worker node can reach API server
telnet 10.0.0.1 443
Trying 10.0.0.1...
Connected to 10.0.0.1.
Escape character is '^]'.  <-------- kubernetes API server is reachable

``` 

## Contributing
[See CONTRIBUTING.md](./CONTRIBUTING.md)
