# Revised Network Policy FAQs

**Q) Is Network Policy support enabled by default?**

No, network policies are not enabled by default. You must opt-in to VPC CNI NetworkPolicy. [[GDC: Please add a link to the steps here]].

**Q) How can I determine if my cluster supports Network Policies?**

For newly created EKS clusters, all necessary components for Network Policy support will be installed by default.

For existing EKS clusters, you can verify the platform version of your cluster. This information is available in the cluster configuration section of the AWS console. Alternatively, you can use the [`aws eks describe-cluster` CLI command](https://docs.aws.amazon.com/cli/latest/reference/eks/describe-cluster.html). Please note that VPC CNI release 1.14 or later is required.

For self-managed clusters, the core requirement is Kubernetes v1.25 or later. You are responsible for installing the necessary VPC CNI components.

**Q) What are the different ways I can install and manage the VPC CNI?**

AWS reccomends installing the VPC CNI using as an Amazon EKS Add-on. EKS will manage installing the add-on.

The VPC CNI can alternatively be installed using a helm chart, on EKS clusters or self-managed clusters.

**Q) Which node operating systems support Network Policies?**

The table below summarizes the supported operating systems (Amazon Linux2, Bottlerocket, Ubuntu) and Kernel versions:

|**Operating System**	|Support Status	|
|---	|---	|
|Amazon Linux 2 (AL2)	|Supported at GA	|
|Bottlerocket	|Supported at GA	|
|Kernel	|v5.10 or later	|

For more details, please [review the EKS AMI changelog](https://github.com/awslabs/amazon-eks-ami/blob/master/CHANGELOG.md).

**Q) Are there any instance types that do not support Network Policies? Is Fargate supported?**

Network policies are currently not supported on GPU instances or Fargate Pods. The AMI for GPU instances lacks the required kernel version of v5.10 or later, and Fargate does not support eBPF.

**Q) Are Windows nodes compatible with Network Policies?**

No, Network Policies cannot be used with Windows nodes.

**Q) Can Network Policies be applied in host networking mode?**

No, Network Policies cannot be applied when using host networking.

**Q) Can Security Groups Per Pod (SGPP) be used with Network Policies?**

Yes, Network Policies can be used in conjunction with SGPP.

**Q) Is Multus compatible with Network Policies? Can I use multiple CNIs or network interfaces with Network Policies?**

No, Network Policies only work with the eth0 interface.

**Q) What should I do if I delete a policy endpoint?**

If a policy endpoint is deleted, the behavior is undefined. It is recommended to restart the node.

**Q) Can the container port differ from the service port?**

No, the Service Port must match the container port.

**Q) What happens if an unsupported policy is created?**

If an unsupported policy is created, an error will be recorded in the node agent log and the policy will not be enforced.

**Q) Can I roll back the VPC CNI version from v1.14 to v1.13?**

Yes, but you must first clean up the rules. Set the `enable-network-policy-controller` flag in the `amazon-vpc-cni` config map to `false` to trigger the network policy controller to clean up. Then, restart the nodes to clean up eBPF maps and programs.

**Q) What IAM configuration is required for Network Policies?**

Network policies do not require any additional IAM configuration beyond a standard AWS VPC CNI installation. However, enabling logging may require additional configuration [[GDC: Please provide the information from Sheetal]].

**Q) How can I troubleshoot Network Policies?**

You can troubleshoot Network Policies by enabling the sending of policy enforcement logs to Amazon CloudWatch. Review the logs to determine which connections were allowed or denied. [[GDC: Please add a link here]]

Sample Log:
```
<Timestamp> SIP:10.1.1.2 ; SPORT:80 ; DIP: 20.1.1.1 ; DPORT: 8080; PROTOCOL: TCP; Policy Verdict: <ALLOW/DENY>
```