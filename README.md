# amazon-vpc-cni-k8s

Networking plugin for pod networking in [Kubernetes](https://kubernetes.io/) using [Elastic Network Interfaces](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html) on AWS.

[![Nightly Tests](https://github.com/aws/amazon-vpc-cni-k8s/workflows/Nightly%20e2e%20tests/badge.svg)](https://github.com/aws/amazon-vpc-cni-k8s/actions)
[![GoReport Widget]][GoReport Status] [![codecov](https://codecov.io/gh/aws/amazon-vpc-cni-k8s/branch/master/graph/badge.svg)](https://codecov.io/gh/aws/amazon-vpc-cni-k8s)

[GoReport Status]: https://goreportcard.com/report/github.com/aws/amazon-vpc-cni-k8s
[GoReport Widget]: https://goreportcard.com/badge/github.com/aws/amazon-vpc-cni-k8s?

## Setup

Download the latest version of the [yaml](./config/) and apply it the cluster.

```
kubectl apply -f aws-k8s-cni.yaml
```

Launch kubelet with network plugins set to cni (`--network-plugin=cni`), the cni directories configured (`--cni-config-dir`
and `--cni-bin-dir`) and node ip set to the primary IPv4 address of the primary ENI for the instance
(`--node-ip=$(curl http://169.254.169.254/latest/meta-data/local-ipv4)`).
It is also recommended to set `--max-pods` equal to _(the number of ENIs for the instance type ×
(the number of IPs per ENI - 1)) + 2_; for details, see [vpc_ip_resource_limit.go][]. Setting `--max-pods` will prevent
scheduling that exceeds the IP address resources available to the kubelet.

[vpc_ip_resource_limit.go]: ./pkg/awsutils/vpc_ip_resource_limit.go

The default manifest expects `--cni-conf-dir=/etc/cni/net.d` and `--cni-bin-dir=/opt/cni/bin`.

L-IPAM requires following [IAM policy](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html):

```
      {
        "Effect": "Allow",
        "Action": [
          "ec2:AssignPrivateIpAddresses",
          "ec2:AttachNetworkInterface",
          "ec2:CreateNetworkInterface",
          "ec2:DeleteNetworkInterface",
          "ec2:DescribeInstances",
          "ec2:DescribeInstanceTypes",
          "ec2:DescribeTags",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DetachNetworkInterface",
          "ec2:ModifyNetworkInterfaceAttribute",
          "ec2:UnassignPrivateIpAddresses"
        ],
        "Resource": "*"
      },
      {
         "Effect": "Allow",
         "Action": [
            "ec2:CreateTags"
          ],
          "Resource": ["arn:aws:ec2:*:*:network-interface/*"]
      }
```

Alternatively there is also a [Helm](https://helm.sh/) chart: [eks/aws-vpc-cni](https://github.com/aws/eks-charts/tree/master/stable/aws-vpc-cni)

## Building

* `make` defaults to `make build-linux` that builds the Linux binaries.
* `unit-test`, `format`,`lint` and `vet` provide ways to run the respective tests/tools and should be run before submitting a PR.
* `make docker` will create a docker container using the docker-build with the finished binaries, with a tag of `amazon/amazon-k8s-cni:latest`
* `make docker-build` uses a docker container (golang:1.14) to build the binaries.
* `make docker-unit-tests` uses a docker container (golang:1.14) to run all unit tests.

## Components

  There are 2 components:

  * [CNI Plugin](https://kubernetes.io/docs/concepts/cluster-administration/network-plugins/#cni), which will wire up host's and pod's network stack when called.
  * `ipamd`, which is a long-running node-Local IP Address Management (IPAM) daemon, is responsible for:
    * maintaining a warm-pool of available IP addresses, and
    * assigning an IP address to a Pod.

The details can be found in [Proposal: CNI plugin for Kubernetes networking over AWS VPC](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/cni-proposal.md).

[Troubleshooting Guide](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/troubleshooting.md) provides tips on how to debug and troubleshoot this CNI.

## ENI Allocation

When a worker node first joins the cluster, there is only 1 ENI along with all of its addresses in the ENI. Without any
configuration, ipamd always try to keep one extra ENI.

When number of pods running on the node exceeds the number of addresses on a single ENI, the CNI backend start allocating
a new ENI and start using following allocation scheme:

For example, a m4.4xlarge node can have up to 8 ENIs, and each ENI can have up to 30 IP addresses. See
[Elastic Network Interfaces documentation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html) for details.

* If the number of current running Pods is between 0 and 29, ipamd will allocate one more eni. And Warm-Pool size is 2 eni * (30 -1) = 58
* If the number of current running Pods is between 30 and 58, ipamd will allocate 2 more eni. And Warm-Pool size is 3 eni * (30 -1) = 87

For a detailed explanation, see [`WARM_ENI_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/eni-and-ip-target.md).

### CNI Configuration Variables<a name="cni-env-vars"></a>

The Amazon VPC CNI plugin for Kubernetes supports a number of configuration options, which are set through environment variables.
The following environment variables are available, and all of them are optional.

---

`AWS_VPC_CNI_NODE_PORT_SUPPORT`

Type: Boolean as a String

Default: `true`

Specifies whether `NodePort` services are enabled on a worker node's primary network interface\. This requires additional
`iptables` rules and that the kernel's reverse path filter on the primary interface is set to `loose`.

---

`AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG`

Type: Boolean as a String

Default: `false`

Specifies that your pods may use subnets and security groups that are independent of your worker node's VPC configuration.
By default, pods share the same subnet and security groups as the worker node's primary interface\. Setting this variable
to `true` causes `ipamd` to use the security groups and VPC subnet in a worker node's `ENIConfig` for elastic network interface
allocation\. You must create an `ENIConfig` custom resource for each subnet that your pods will reside in, and then annotate or
label each worker node to use a specific `ENIConfig` (multiple worker nodes can be annotated or labelled with the same `ENIConfig`).
Worker nodes can only be annotated with a single `ENIConfig` at a time, and the subnet in the `ENIConfig` must belong to the
same Availability Zone that the worker node resides in.
For more information, see [*CNI Custom Networking*](https://docs.aws.amazon.com/eks/latest/userguide/cni-custom-network.html)
in the Amazon EKS User Guide.

---

`ENI_CONFIG_ANNOTATION_DEF`

Type: String

Default: `k8s.amazonaws.com/eniConfig`

Specifies node annotation key name. This should be used when `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true`. Annotation value
will be used to set `ENIConfig` name. Note that annotations take precedence over labels.

---

`ENI_CONFIG_LABEL_DEF`

Type: String

Default: `k8s.amazonaws.com/eniConfig`

Specifies node label key name\. This should be used when `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true`. Label value will be used
to set `ENIConfig` name\. Note that annotations will take precedence over labels. To use labels ensure annotation with key
`k8s.amazonaws.com/eniConfig` or defined key (in `ENI_CONFIG_ANNOTATION_DEF`) is not set on the node.
To select an `ENIConfig` based upon availability zone set this to `failure-domain.beta.kubernetes.io/zone` and create an
`ENIConfig` custom resource for each availability zone (e.g. `us-east-1a`).

---

`AWS_VPC_ENI_MTU` (Since v1.6.0)

Type: Integer as a String

Default: 9001

Used to configure the MTU size for attached ENIs. The valid range is from `576` to `9001`.

---

`AWS_VPC_K8S_CNI_EXTERNALSNAT`

Type: Boolean as a String

Default: `false`

Specifies whether an external NAT gateway should be used to provide SNAT of secondary ENI IP addresses. If set to `true`, the
SNAT `iptables` rule and off\-VPC IP rule are not applied, and these rules are removed if they have already been applied.
Disable SNAT if you need to allow inbound communication to your pods from external VPNs, direct connections, and external VPCs,
and your pods do not need to access the Internet directly via an Internet Gateway. However, your nodes must be running in a
private subnet and connected to the internet through an AWS NAT Gateway or another external NAT device.

---

`AWS_VPC_K8S_CNI_RANDOMIZESNAT`

Type: String

Default: `prng`

Valid Values: `hashrandom`, `prng`, `none`

Specifies whether the SNAT `iptables` rule should randomize the outgoing ports for connections\. This setting takes effect when
`AWS_VPC_K8S_CNI_EXTERNALSNAT=false`, which is the default setting. The default setting for `AWS_VPC_K8S_CNI_RANDOMIZESNAT` is
`prng`, meaning that `--random-fully` will be added to the SNAT `iptables` rule\. For old versions of `iptables` that do not
support `--random-fully` this option will fall back to `--random`. To disable random port allocation, if you for example
rely on sequential port allocation for outgoing connections set it to `none`.

*Note*: Any options other than `none` will cause outbound connections to be assigned a source port that is not necessarily
part of the ephemeral port range set at the OS level (`/proc/sys/net/ipv4/ip_local_port_range`). This is relevant for any
customers that might have NACLs restricting traffic based on the port range found in `ip_local_port_range`.

---

`AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS` (Since v1.6.0)

Type: String

Default: empty

Specify a comma separated list of IPv4 CIDRs to exclude from SNAT. For every item in the list an `iptables` rule and off\-VPC
IP rule will be applied. If an item is not a valid ipv4 range it will be skipped. This should be used when `AWS_VPC_K8S_CNI_EXTERNALSNAT=false`.

---

`WARM_ENI_TARGET`

Type: Integer as a String

Default: `1`

Specifies the number of free elastic network interfaces \(and all of their available IP addresses\) that the `ipamd` daemon should
attempt to keep available for pod assignment on the node\. By default, `ipamd` attempts to keep 1 elastic network interface and all
of its IP addresses available for pod assignment. The number of IP addresses per network interface varies by instance type. For more
information, see [IP Addresses Per Network Interface Per Instance Type](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI)
in the *Amazon EC2 User Guide for Linux Instances*.

For example, an `m4.4xlarge` launches with 1 network interface and 30 IP addresses\. If 5 pods are placed on the node and 5 free IP
addresses are removed from the IP address warm pool, then `ipamd` attempts to allocate more interfaces until `WARM_ENI_TARGET` free
interfaces are available on the node.
If `WARM_IP_TARGET` is set, then this environment variable is ignored and the `WARM_IP_TARGET` behavior is used instead.

---

`WARM_IP_TARGET`

Type: Integer

Default: None

Specifies the number of free IP addresses that the `ipamd` daemon should attempt to keep available for pod assignment on the node.
For example, if `WARM_IP_TARGET` is set to 5, then `ipamd` attempts to keep 5 free IP addresses available at all times. If the
elastic network interfaces on the node are unable to provide these free addresses, `ipamd` attempts to allocate more interfaces
until `WARM_IP_TARGET` free IP addresses are available.

**NOTE!** Avoid this setting for large clusters, or if the cluster has high pod churn. Setting it will cause additional calls to the
EC2 API and that might cause throttling of the requests. It is strongly suggested to set `MINIMUM_IP_TARGET` when using `WARM_IP_TARGET`.

If both `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` are set, `ipamd` will attempt to meet both constraints.
This environment variable overrides `WARM_ENI_TARGET` behavior. For a detailed explanation, see
[`WARM_ENI_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/eni-and-ip-target.md).


---

`MINIMUM_IP_TARGET` (Since v1.6.0)

Type: Integer

Default: None

Specifies the number of total IP addresses that the `ipamd` daemon should attempt to allocate for pod assignment on the node.
`MINIMUM_IP_TARGET` behaves identically to `WARM_IP_TARGET` except that instead of setting a target number of free IP
addresses to keep available at all times, it sets a target number for a floor on how many total IP addresses are allocated.

`MINIMUM_IP_TARGET` is for pre-scaling, `WARM_IP_TARGET` is for dynamic scaling. For example, suppose a cluster has an
expected pod density of approximately 30 pods per node. If `WARM_IP_TARGET` is set to 30 to ensure there are enough IPs
allocated up front by the CNI, then 30 pods are deployed to the node, the CNI will allocate an additional 30 IPs, for
a total of 60, accelerating IP exhaustion in the relevant subnets. If instead `MINIMUM_IP_TARGET` is set to 30 and
`WARM_IP_TARGET` to 2, after the 30 pods are deployed the CNI would allocate an additional 2 IPs. This still provides
elasticity, but uses roughly half as many IPs as using WARM_IP_TARGET alone (32 IPs vs 60 IPs).

This also improves reliability of the EKS cluster by reducing the number of calls necessary to allocate or deallocate
private IPs, which may be throttled, especially at scaling-related times.

---

`MAX_ENI`

Type: Integer

Default: None

Specifies the maximum number of ENIs that will be attached to the node. When `MAX_ENI` is unset or 0 (or lower), the setting
is not used, and the maximum number of ENIs is always equal to the maximum number for the instance type in question. Even when
`MAX_ENI` is a positive number, it is limited by the maximum number for the instance type.

---

`AWS_VPC_K8S_CNI_LOGLEVEL`

Type: String

Default: `DEBUG`

Valid Values: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`. (Not case sensitive)

Specifies the loglevel for `ipamd`.

---

`AWS_VPC_K8S_CNI_LOG_FILE`

Type: String

Default: `/host/var/log/aws-routed-eni/ipamd.log`

Valid Values: `stdout` or a file path

Specifies where to write the logging output of `ipamd`. Either to stdout or to override the default file (i.e., `/var/log/aws-routed-eni/ipamd.log`).

---

`AWS_VPC_K8S_PLUGIN_LOG_FILE`

Type: String

Default: `/var/log/aws-routed-eni/plugin.log`

Valid Values: `stderr` or a file path

Specifies where to write the logging output for `aws-cni` plugin. Either to `stderr` or to override the default file (i.e., `/var/log/aws-routed-eni/plugin.log`).
`Stdout` cannot be supported for plugin log, please refer to [#1248](https://github.com/aws/amazon-vpc-cni-k8s/issues/1248) for more details.

Note: If chaining an external plugin (i.e Cilium) that does not provide a `pluginLogFile` in its config file, the CNI plugin will by default write to `os.Stderr`. The output of `cmdAdd` are available in the Kubelet logs.

---

`AWS_VPC_K8S_PLUGIN_LOG_LEVEL`

Type: String

Default: `DEBUG`

Valid Values: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`. (Not case sensitive)

Specifies the loglevel for `aws-cni` plugin.

---

`INTROSPECTION_BIND_ADDRESS`

Type: String

Default: `127.0.0.1:61679`

Specifies the bind address for the introspection endpoint.

A Unix Domain Socket can be specified with the `unix:` prefix before the socket path.

---

`DISABLE_INTROSPECTION`

Type: Boolean as a String

Default: `false`

Specifies whether introspection endpoints are disabled on a worker node. Setting this to `true` will reduce the debugging
information we can get from the node when running the `aws-cni-support.sh` script.

---

`DISABLE_METRICS`

Type: Boolean as a String

Default: `false`

Specifies whether the prometheus metrics endpoint is disabled or not for ipamd. By default metrics are published
on `:61678/metrics`.

---

`AWS_VPC_K8S_CNI_VETHPREFIX`

Type: String

Default: `eni`

Specifies the veth prefix used to generate the host-side veth device name for the CNI. The prefix can be at most 4 characters long. The prefixes `eth`, `vlan` and `lo` are reserved by the CNI plugin and cannot be specified. We recommend using prefix name not shared by any other network interfaces on the worker node instance.

---

`ADDITIONAL_ENI_TAGS` (Since v1.6.0)

Type: String

Default: `{}`

Example values: `{"tag_key": "tag_val"}`

Metadata applied to ENI help you categorize and organize your resources for billing or other purposes. Each tag consists of a
custom-defined key and an optional value. Tag keys can have a maximum character length of 128 characters. Tag values can have
a maximum length of 256 characters. These tags will be added to all ENIs on the host.

Important: Custom tags should not contain `k8s.amazonaws.com` prefix as it is reserved. If the tag has `k8s.amazonaws.com`
string, tag addition will ignored.

---

`AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER`

Type: Boolean as a String

Default: `true`

Specifies whether ipamd should configure rp filter for primary interface. Setting this to `false` will require rp filter to be configured through init container

---

`CLUSTER_NAME`

Type: String

Default: `""`

Specifies the cluster name to tag allocated ENIs with. See the "Cluster Name tag" section below.

---

`ENABLE_POD_ENI` (Since v1.7.0)

Type: Boolean as a String

Default: `false`

To enable security groups for pods you need to have at least an EKS 1.17 eks.3 cluster. 

Setting `ENABLE_POD_ENI` to `true` will allow IPAMD to add the `vpc.amazonaws.com/has-trunk-attached` label to the node if the instance has capacity to attach an additional ENI. 

The label notifies vpc-resource-controller (https://github.com/aws/amazon-vpc-resource-controller-k8s) to attach a Trunk ENI to the instance. The label value is initially set to `false` and is marked to `true` by IPAMD when vpc-resource-controller attaches a Trunk ENI to the instance. However, there might be cases where the label value will remain `false` if the instance doesn't support ENI Trunking. 

**NOTE!** Toggling `ENABLE_POD_ENI` from `true` to `false` will not detach the Trunk ENI from instance. To delete/detach the Trunk ENI from instance, you need recycle the instance.

---

`DISABLE_TCP_EARLY_DEMUX` (Since v1.7.3)

Type: Boolean as a String

Default: `false`

If `ENABLE_POD_ENI` is set to `true`, in order for the kubelet to connect via TCP (for liveness or readiness probes)
to pods that are using per pod security groups, `DISABLE_TCP_EARLY_DEMUX` should be set to `true` for `amazon-k8s-cni-init`
container under `initcontainers`. This will increase the local TCP connection latency slightly.
Details on why this is needed can be found in this [#1212 comment](https://github.com/aws/amazon-vpc-cni-k8s/pull/1212#issuecomment-693540666).
To use this setting, a Linux kernel version of at least 4.6 is needed on the worker node.

You can use the below command to enable `DISABLE_TCP_EARLY_DEMUX` to `true` -

```
kubectl patch daemonset aws-node -n kube-system -p '{"spec": {"template": {"spec": {"initContainers": [{"env":[{"name":"DISABLE_TCP_EARLY_DEMUX","value":"true"}],"name":"aws-vpc-cni-init"}]}}}}'
```

### ENI tags related to Allocation

This plugin interacts with the following tags on ENIs:

* `cluster.k8s.amazonaws.com/name`
* `node.k8s.amazonaws.com/instance_id`
* `node.k8s.amazonaws.com/no_manage`

#### Cluster Name tag

The tag `cluster.k8s.amazonaws.com/name` will be set to the cluster name of the
aws-node daemonset which created the ENI.

#### Instance ID tag

The tag `node.k8s.amazonaws.com/instance_id` will be set to the instance ID of
the aws-node instance that allocated this ENI.

#### No Manage tag

The tag `node.k8s.amazonaws.com/no_manage` is read by the aws-node daemonset to
determine whether an ENI attached to the machine should not be configured or
used for private IPs.

This tag is not set by the cni plugin itself, but rather may be set by a user
to indicate that an ENI is intended for host networking pods, or for some other
process unrelated to Kubernetes.

*Note*: Attaching an ENI with the `no_manage` tag will result in an incorrect
value for the Kubelet's `--max-pods` configuration option. Consider also
updating the `MAX_ENI` and `--max-pods` configuration options on this plugin
and the kubelet respectively if you are making use of this tag.

### Container Runtime 

Currently IPAMD uses dockershim socket to pull pod sandboxes information upon its starting. The runtime can be set to others.
The mountPath should be changed to `/var/run/cri.sock` and hostPath should be pointed to the wanted socket, such as 
`/var/run/containerd/containerd.sock` for containerd. If using helm chart, the flag `--set cri.hostPath=/var/run/containerd/containerd.sock`
can set the paths for you.

*Note*: When using other container runtime instead of dockershim, make sure also setting kubelet in instances.

### Notes

`L-IPAMD`(aws-node daemonSet) running on every worker node requires access to kubernetes API server. If it can **not** reach
kubernetes API server, ipamd will exit and CNI will not be able to get any IP address for Pods. Here is a way to confirm if
`aws-node` has access to the kubernetes API server.

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

## Security disclosures

If you think you’ve found a potential security issue, please do not post it in the Issues. Instead, please follow the
instructions [here](https://aws.amazon.com/security/vulnerability-reporting/) or [email AWS security directly](mailto:aws-security@amazon.com).

## Contributing

[See CONTRIBUTING.md](./CONTRIBUTING.md)
