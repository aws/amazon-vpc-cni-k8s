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

Launch kubelet with network plugins set to cni (`--network-plugin=cni`), the cni directories configured (`--cni-config-dir` and `--cni-bin-dir`) and node ip set to the primary IPv4 address of the primary ENI for the instance (`--node-ip=$(curl http://169.254.169.254/latest/meta-data/local-ipv4)`).  It is also recommended to set `--max-pods` equal to the number of ENIs for the instance type * (the number of IPs per ENI - 1) [see](./pkg/awsutils/vpc_ip_resource_limit.go) to prevent scheduling that exceeds the IP resources available to the kubelet.

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
     "Action": "tag:TagResources",
     "Resource": "*"
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
