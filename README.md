
# amazon-vpc-cni-k8s

Networking plugin for pod networking in [Kubernetes](https://kubernetes.io/) using [Elastic Network Interfaces](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html) on AWS.

[![Nightly Tests](https://github.com/aws/amazon-vpc-cni-k8s/workflows/Nightly%20Cron%20tests/badge.svg)](https://github.com/aws/amazon-vpc-cni-k8s/actions)
[![GoReport Widget]][GoReport Status] [![codecov](https://codecov.io/gh/aws/amazon-vpc-cni-k8s/branch/master/graph/badge.svg)](https://codecov.io/gh/aws/amazon-vpc-cni-k8s)

[GoReport Status]: https://goreportcard.com/report/github.com/aws/amazon-vpc-cni-k8s
[GoReport Widget]: https://goreportcard.com/badge/github.com/aws/amazon-vpc-cni-k8s?

## Setup

Download the latest version of the [yaml](./config/) and apply it to the cluster.

```
kubectl apply -f aws-k8s-cni.yaml
```

Launch kubelet with network plugins set to cni (`--network-plugin=cni`), the cni directories configured (`--cni-config-dir`
and `--cni-bin-dir`) and node ip set to the primary IPv4 address of the primary ENI for the instance
(`--node-ip=$(curl http://169.254.169.254/latest/meta-data/local-ipv4)`).
It is also recommended that you set `--max-pods` equal to _(the number of ENIs for the instance type ×
(the number of IPs per ENI - 1)) + 2_; for details, see [vpc_ip_resource_limit.go][]. Setting `--max-pods` will prevent
scheduling that exceeds the IP address resources available to the kubelet.

[vpc_ip_resource_limit.go]: ./pkg/awsutils/vpc_ip_resource_limit.go

The default manifest expects `--cni-conf-dir=/etc/cni/net.d` and `--cni-bin-dir=/opt/cni/bin`.

Alternatively there is also a [Helm](https://helm.sh/) chart: [eks/aws-vpc-cni](https://github.com/aws/eks-charts/tree/master/stable/aws-vpc-cni)

## IAM Policy

See [here](./docs/iam-policy.md) for required IAM policies.

## Building

* `make` defaults to `make build-linux` that builds the Linux binaries.
* `unit-test`, `format`,`lint` and `vet` provide ways to run the respective tests/tools and should be run before submitting a PR.
* `make docker` will create a docker container using the docker-build with the finished binaries, with a tag of `amazon/amazon-k8s-cni:latest`
* `make docker-build` uses a docker container (golang:1.21.5-6-gcc-al2) to build the binaries.
* `make docker-unit-tests` uses a docker container (golang:1.21.5-6-gcc-al2) to run all unit tests.

## Components

  There are 2 components:

* [CNI Plugin](https://kubernetes.io/docs/concepts/cluster-administration/network-plugins/#cni), which will wire up the host's and pod's network stack when called.
* `ipamd`, a long-running node-Local IP Address Management (IPAM) daemon, is responsible for:
  * maintaining a warm-pool of available IP addresses, and
  * assigning an IP address to a Pod.

The details can be found in [Proposal: CNI plugin for Kubernetes networking over AWS VPC](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/cni-proposal.md).

## Help & Feedback

For help, please consider the following venues (in order):

* [Amazon EKS Best Practices Guide for Networking](https://aws.github.io/aws-eks-best-practices/networking/index/)
* [Troubleshooting Guide](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/troubleshooting.md) provides tips on how to debug and troubleshoot this CNI.
* [Search open issues](https://github.com/aws/amazon-vpc-cni-k8s/issues)
* [File an issue](https://github.com/aws/amazon-vpc-cni-k8s/issues/new/choose)
* Chat with us on the `#aws-vpc-cni` channel in the [Kubernetes Slack](https://kubernetes.slack.com/) community.

## Recommended Version

For all Kubernetes releases, *we recommend installing the latest VPC CNI release*. The following table denotes our *oldest* recommended
VPC CNI version for each actively supported Kubernetes release.

| Kubernetes Release | 1.29     | 1.28     | 1.27     | 1.26     | 1.25     | 1.24    |
| ------------------ | -------- | -------- | -------- | -------- | -------- | ------- |
| VPC CNI Version    | v1.14.1+ | v1.13.4+ | v1.12.5+ | v1.12.0+ | v1.11.4+ | v1.9.3+ |

## Version Upgrade

Upgrading (or downgrading) the VPC CNI version should result in no downtime. Existing pods should not be affected and will not lose network connectivity.
New pods will be in pending state until the VPC CNI is fully initialized and can assign pod IP addresses. In v1.12.0+, VPC CNI state is
restored via an on-disk file: `/var/run/aws-node/ipam.json`. In lower versions, state is restored via calls to container runtime.

## ENI Allocation

When a worker node first joins the cluster, there is only 1 ENI along with all of the addresses on the ENI. Without any
configuration, ipamd always tries to keep one extra ENI.

When the number of pods running on the node exceeds the number of addresses on a single ENI, the CNI backend starts allocating
a new ENI using the following allocation scheme:

* If the number of current running Pods is between 0 and 29, ipamd will allocate one more eni. And Warm-Pool size is 2 eni * (30 -1) = 58
* If the number of current running Pods is between 30 and 58, ipamd will allocate 2 more eni. And Warm-Pool size is 3 eni * (30 -1) = 87

For example, a m4.4xlarge node can have up to 8 ENIs, and each ENI can have up to 30 IP addresses. See
[Elastic Network Interfaces documentation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html) for details.

For a detailed explanation, see [`WARM_ENI_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/eni-and-ip-target.md).

## Privileged mode

VPC CNI makes use of privileged mode (`privileged: true`) in the manifest for its `aws-vpc-cni-init` and `aws-eks-nodeagent` containers. `aws-vpc-cni-init` container requires elevated privilege to set the networking kernel parameters while `aws-eks-nodeagent` container requires these privileges for attaching BPF probes to enforce network policy

## Network Policies

In Kubernetes, by default, all pod-to-pod communication is allowed. Communication can be restricted with Kubernetes NetworkPolicy objects.

VPC CNI versions v1.14, and greater support [Kubernetes Network Policies.](https://kubernetes.io/docs/concepts/services-networking/network-policies/). Network Policies specify how pods can communicate over the network, at the IP address or port level. The VPC CNI implements the Kubernetes [NetworkPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#networkpolicy-v1-networking-k8s-io) API. Network policies generally include a pod selector, and Ingress/Egress rules.

For EKS clusters, review [Configure your cluster for Kubernetes network policies](https://docs.aws.amazon.com/eks/latest/userguide/cni-network-policy.html) in the Amazon EKS User Guide.

The AWS VPC CNI implementation of network policies may be enabled in self-managed clusters. This requires the VPC CNI agent, the [Network Policy Controller](https://github.com/aws/amazon-network-policy-controller-k8s), and [Network Policy Node Agent](https://github.com/aws/aws-network-policy-agent).

Review the [Network Policy FAQ](./docs/network-policy-faq.md) for more information.

### Network Policy Related Components

* [Network Policy Controller](https://github.com/aws/amazon-network-policy-controller-k8s) watches for NetworkPolicy objects and instructs the node agent.
  * Network policy controller configures policies for pods in parallel to pod provisioning, until then new pods will come up with default allow policy. All ingress and egress traffic is allowed to and from the new pods until they are resolved against the existing policies.
  * This controller is automatically installed on the EKS Control Plane.
* [Network Policy Node Agent](https://github.com/aws/aws-network-policy-agent) implements Network Policies on nodes by creating eBPF programs.
* [AWS eBPF SDK for Go](https://github.com/aws/aws-ebpf-sdk-go) provides an interface to interact with eBPF programs on the node. This SDK allows for runtime introspection, tracing, and analysis of eBPF execution, aiding in identifying and resolving connectivity issues.
* [VPC Resource Controller](https://github.com/aws/amazon-vpc-resource-controller-k8s) manages Branch & Trunk Network Interfaces for Kubernetes Pods. 

## ConfigMap

If the VPC CNI is installed as an Amazon EKS add-ons (also known as a managed add-on), configure it using [AWS APIs as described in the EKS User Guide](https://docs.aws.amazon.com/eks/latest/userguide/eks-add-ons.html).

If the VPC CNI is installed with a Helm Chart, the ConfigMap is installed in your cluster. Review the [Helm Chart information.](https://github.com/aws/eks-charts/tree/master/stable/aws-vpc-cni)

Otherwise, the VPC CNI may be configured with a ConfigMap, as shown below:

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: amazon-vpc-cni
  namespace: kube-system
data:
  enable-network-policy-controller: "true"
```

* `enable-network-policy-controller`
  * Default: False
  * If enabled, the VPC CNI will enforce NetworkPolicies. Pods that are selected by at least one NetworkPolicy will have their traffic restricted.

## Helm Charts

AWS publishes a Helm chart to install the VPC CNI. [Review how to install the helm chart, and the configuration parameters for the chart.](https://github.com/aws/eks-charts/tree/master/stable/aws-vpc-cni)

## CNI Configuration Variables<a name="cni-env-vars"></a>

The Amazon VPC CNI plugin for Kubernetes supports a number of configuration options, which are set through environment variables.
The following environment variables are available, and all of them are optional.

#### `AWS_MANAGE_ENIS_NON_SCHEDULABLE` (v1.12.6+)

Type: Boolean as a String

Default: `false`

Specifies whether IPAMD should allocate or deallocate ENIs on a non-schedulable node.

#### `AWS_VPC_CNI_NODE_PORT_SUPPORT`

Type: Boolean as a String

Default: `true`

Specifies whether `NodePort` services are enabled on a worker node's primary network interface\. This requires additional
`iptables` rules, and the kernel's reverse path filter on the primary interface is set to `loose`.

#### `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG`

Type: Boolean as a String

Default: `false`

Specifies that your pods may use subnets and security groups that are independent of your worker node's VPC configuration.
By default, pods share the same subnet and security groups as the worker node's primary interface\. Setting this variable
to `true` causes `ipamd` to use the security groups and VPC subnet in a worker node's `ENIConfig` for elastic network interface
allocation\. You must create an `ENIConfig` custom resource for each subnet that your pods will reside in, and then annotate or
label each worker node to use a specific `ENIConfig`. Multiple worker nodes can be annotated or labelled with the same `ENIConfig`, but
each Worker node can be annotated with a single `ENIConfig` at a time.  Further, the subnet in the `ENIConfig` must belong to the
same Availability Zone that the worker node resides in.
For more information, see [*CNI Custom Networking*](https://docs.aws.amazon.com/eks/latest/userguide/cni-custom-network.html)
in the Amazon EKS User Guide.

#### `ENI_CONFIG_ANNOTATION_DEF`

Type: String

Default: `k8s.amazonaws.com/eniConfig`

Specifies node annotation key name. This should be used when `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true`. Annotation value
will be used to set `ENIConfig` name. Note that annotations take precedence over labels.

#### `ENI_CONFIG_LABEL_DEF`

Type: String

Default: `k8s.amazonaws.com/eniConfig`

Specifies node label key name\. This should be used when `AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true`. Label value will be used
to set `ENIConfig` name\. Note that annotations will take precedence over labels. To use labels, ensure there is no annotation with key
`k8s.amazonaws.com/eniConfig` or defined key (in `ENI_CONFIG_ANNOTATION_DEF`) set on the node.
To select an `ENIConfig` based upon availability zone set this to `topology.kubernetes.io/zone` and create an
`ENIConfig` custom resource for each availability zone (e.g. `us-east-1a`). Note that tag `failure-domain.beta.kubernetes.io/zone` is deprecated and replaced with the tag `topology.kubernetes.io/zone`.

#### `HOST_CNI_BIN_PATH`

Type: String

Default: `/host/opt/cni/bin`

Specifies the location to install CNI binaries. Note that the `aws-node` daemonset mounts `/opt/cni/bin` to `/host/opt/cni/bin`. The value you choose must be a location that the `aws-node` pod can write to.

#### `HOST_CNI_CONFDIR_PATH`

Type: String

Default: `/host/etc/cni/net.d`

Specifies the location to install the VPC CNI conflist. Note that the `aws-node` daemonset mounts `/etc/cni/net.d` to `/host/etc/cni/net.d`. The value you choose must be a location that the `aws-node` pod can write to.

#### `AWS_VPC_ENI_MTU` (v1.6.0+)

Type: Integer as a String

Default: 9001

Used to configure the MTU size for attached ENIs. The valid range is from `576` to `9001`.

#### `AWS_VPC_K8S_CNI_EXTERNALSNAT`

Type: Boolean as a String

Default: `false`

Specifies whether an external NAT gateway should be used to provide SNAT of secondary ENI IP addresses. If set to `true`, the
SNAT `iptables` rule and off\-VPC IP rule are not applied, and these rules are removed if they have already been applied.
Disable SNAT if you need to allow inbound communication to your pods from external VPNs, direct connections, and external VPCs,
and your pods do not need to access the Internet directly via an Internet Gateway. However, your nodes must be running in a
private subnet and connected to the internet through an AWS NAT Gateway or another external NAT device.

#### `AWS_VPC_K8S_CNI_RANDOMIZESNAT`

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

#### `AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS` (v1.6.0+)

Type: String

Default: empty

Specify a comma-separated list of IPv4 CIDRs to exclude from SNAT. For every item in the list an `iptables` rule and off\-VPC
IP rule will be applied. If an item is not a valid ipv4 range it will be skipped. This should be used when `AWS_VPC_K8S_CNI_EXTERNALSNAT=false`.

#### `WARM_ENI_TARGET`

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

**NOTE!** If `WARM_IP_TARGET` is set, then this environment variable is ignored and the `WARM_IP_TARGET` behavior is used instead.

#### `WARM_IP_TARGET`

Type: Integer

Default: None

Specifies the number of free IP addresses that the `ipamd` daemon should attempt to keep available for pod assignment on the node. Setting this to a non-positive value is the same as setting this to 0 or not setting the variable.
With `ENABLE_PREFIX_DELEGATION` set to `true` then `ipamd` daemon will check if the existing (/28) prefixes are enough to maintain the
`WARM_IP_TARGET` if it is not sufficient then more prefixes will be attached.

For example,

1. if `WARM_IP_TARGET` is set to 5, then `ipamd` attempts to keep 5 free IP addresses available at all times. If the
elastic network interfaces on the node are unable to provide these free addresses, `ipamd` attempts to allocate more interfaces
until `WARM_IP_TARGET` free IP addresses are available.
2. `ENABLE_PREFIX_DELEGATION` set to `true` and `WARM_IP_TARGET` is 16. Initially, 1 (/28) prefix is sufficient but once a single pod is assigned IP then
remaining free IPs are 15 hence IPAMD will allocate 1 more prefix to achieve 16 `WARM_IP_TARGET`

**NOTE!** Avoid this setting for large clusters, or if the cluster has high pod churn. Setting it will cause additional calls to the
EC2 API and that might cause throttling of the requests. It is strongly suggested to set `MINIMUM_IP_TARGET` when using `WARM_IP_TARGET`.

If both `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` are set, `ipamd` will attempt to meet both constraints.
This environment variable overrides `WARM_ENI_TARGET` behavior. For a detailed explanation, see
[`WARM_ENI_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/eni-and-ip-target.md).

If `ENABLE_PREFIX_DELEGATION` set to `true` and `WARM_IP_TARGET` overrides `WARM_PREFIX_TARGET` behavior. For a detailed explanation, see
[`WARM_PREFIX_TARGET`, `WARM_IP_TARGET` and `MINIMUM_IP_TARGET`](https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/prefix-and-ip-target.md).

#### `MINIMUM_IP_TARGET` (v1.6.0+)

Type: Integer

Default: None

Specifies the number of total IP addresses that the `ipamd` daemon should attempt to allocate for pod assignment on the node.
`MINIMUM_IP_TARGET` behaves identically to `WARM_IP_TARGET` except that instead of setting a target number of free IP
addresses to keep available at all times, it sets a target number for a floor on how many total IP addresses are allocated. Setting to a
non-positive value is same as setting this to 0 or not setting the variable.

`MINIMUM_IP_TARGET` is for pre-scaling, `WARM_IP_TARGET` is for dynamic scaling. For example, suppose a cluster has an
expected pod density of approximately 30 pods per node. If `WARM_IP_TARGET` is set to 30 to ensure there are enough IPs
allocated up front by the CNI, then 30 pods are deployed to the node, the CNI will allocate an additional 30 IPs, for
a total of 60, accelerating IP exhaustion in the relevant subnets. If instead `MINIMUM_IP_TARGET` is set to 30 and
`WARM_IP_TARGET` to 2, after the 30 pods are deployed the CNI would allocate an additional 2 IPs. This still provides
elasticity, but uses roughly half as many IPs as using WARM_IP_TARGET alone (32 IPs vs 60 IPs).

This also improves the reliability of the EKS cluster by reducing the number of calls necessary to allocate or deallocate
private IPs, which may be throttled, especially at scaling-related times.

**NOTE!** 
1. If `MINIMUM_IP_TARGET` is set, `WARM_ENI_TARGET` will be ignored. Please utilize `WARM_IP_TARGET` instead.
2. If `MINIMUM_IP_TARGET` is set and `WARM_IP_TARGET` is not set, `WARM_IP_TARGET` is assumed to be 0, which leads to the number of IPs attached to the node will be the value of `MINIMUM_IP_TARGET`. This configuration will prevent future ENIs/IPs from being allocated. It is strongly recommended that `WARM_IP_TARGET` should be set greater than 0 when `MINIMUM_IP_TARGET` is set.

#### `MAX_ENI`

Type: Integer

Default: None

Specifies the maximum number of ENIs that will be attached to the node. When `MAX_ENI` is unset or 0 (or lower), the setting
is not used, and the maximum number of ENIs is always equal to the maximum number for the instance type in question. Even when
`MAX_ENI` is a positive number, it is limited by the maximum number for the instance type.

#### `AWS_VPC_K8S_CNI_LOGLEVEL`

Type: String

Default: `DEBUG`

Valid Values: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`. (Not case sensitive)

Specifies the log level for `ipamd` and `cni-metric-helper`.

#### `AWS_VPC_K8S_CNI_LOG_FILE`

Type: String

Default: `/host/var/log/aws-routed-eni/ipamd.log`

Valid Values: `stdout`, `stderr`, or a file path

Specifies where to write the logging output of `ipamd`: `stdout`, `stderr`, or a file path other than the default (`/var/log/aws-routed-eni/ipamd.log`).

Note: `/host/var/log/...` is the container file-system path, which maps to `/var/log/...` on the node.

Note: The IPAMD process runs within the `aws-node` pod, so writing to `stdout` or `stderr` will write to `aws-node` pod logs.

#### `AWS_VPC_K8S_PLUGIN_LOG_FILE`

Type: String

Default: `/var/log/aws-routed-eni/plugin.log`

Valid Values: `stderr` or a file path. Note that setting to the empty string is an alias for `stderr`, and this comes from upstream kubernetes best practices.

Specifies where to write the logging output for `aws-cni` plugin: `stderr` or a file path other than the default (`/var/log/aws-routed-eni/plugin.log`).

Note: `stdout` cannot be supported for plugin log. Please refer to [#1248](https://github.com/aws/amazon-vpc-cni-k8s/issues/1248) for more details.

Note: In EKS 1.24+, the CNI plugin is exec'ed by the container runtime, so `stderr` is for the container-runtime process, NOT the `aws-node` pod. In older versions, the CNI plugin was exec'ed by kubelet, so `stderr` is for the kubelet process.

Note: If chaining an external plugin (i.e. Cilium) that does not provide a `pluginLogFile` in its config file, the CNI plugin will by default write to `os.Stderr`.

#### `AWS_VPC_K8S_PLUGIN_LOG_LEVEL`

Type: String

Default: `DEBUG`

Valid Values: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`. (Not case sensitive)

Specifies the loglevel for `aws-cni` plugin.

#### `INTROSPECTION_BIND_ADDRESS`

Type: String

Default: `127.0.0.1:61679`

Specifies the bind address for the introspection endpoint.

A Unix Domain Socket can be specified with the `unix:` prefix before the socket path.

#### `DISABLE_INTROSPECTION`

Type: Boolean as a String

Default: `false`

Specifies whether introspection endpoints are disabled on a worker node. Setting this to `true` will reduce the debugging
information we can get from the node when running the `aws-cni-support.sh` script.

#### `DISABLE_METRICS`

Type: Boolean as a String

Default: `false`

Specifies whether the prometheus metrics endpoint is disabled or not for ipamd. By default metrics are published
on `:61678/metrics`.

#### `AWS_VPC_K8S_CNI_VETHPREFIX`

Type: String

Default: `eni`

Specifies the veth prefix used to generate the host-side veth device name for the CNI. The prefix can be at most 4 characters long. The prefixes `eth`, `vlan`, and `lo` are reserved by the CNI plugin and cannot be specified. We recommend using prefix name not shared by any other network interfaces on the worker node instance.

#### `ADDITIONAL_ENI_TAGS` (v1.6.0+)

Type: String

Default: `{}`

Example values: `{"tag_key": "tag_val"}`

Metadata applied to ENI helps you categorize and organize your resources for billing or other purposes. Each tag consists of a
custom-defined key and an optional value. Tag keys can have a maximum character length of 128 characters. Tag values can have
a maximum length of 256 characters. These tags will be added to all ENIs on the host.

Important: Custom tags should not contain `k8s.amazonaws.com` prefix as it is reserved. If the tag has `k8s.amazonaws.com`
string, tag addition will be ignored.

#### `AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER` (deprecated v1.12.1+)

Type: Boolean as a String

Default: `true`

Specifies whether ipamd should configure rp filter for primary interface. Setting this to `false` will require rp filter to be configured through init container.

**NOTE!** `AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER` has been deprecated, so setting this environment variable results in a no-op. The init container unconditionally configures the rp filter for the primary interface.

#### `CLUSTER_NAME`

Type: String

Default: `""`

Specifies the cluster name to tag allocated ENIs with. See the "Cluster Name tag" section below.

#### `CLUSTER_ENDPOINT` (v1.12.1+)

Type: String

Default: `""`

Specifies the cluster endpoint to use for connecting to the api-server without relying on kube-proxy.
This is an optional configuration parameter that can improve the initialization time of the AWS VPC CNI.

**NOTE!** When setting CLUSTER_ENDPOINT, it is *STRONGLY RECOMMENDED* that you enable private endpoint access for your API server, otherwise VPC CNI requests can traverse the public NAT gateway and may result in additional charges.

#### `ENABLE_POD_ENI` (v1.7.0+)

Type: Boolean as a String

Default: `false`

To enable security groups for pods you need to have at least an EKS 1.17 eks.3 cluster.

Setting `ENABLE_POD_ENI` to `true` will allow IPAMD to add the `vpc.amazonaws.com/has-trunk-attached` label to the node if the instance has the capacity to attach an additional ENI.

The label notifies [vpc-resource-controller](https://github.com/aws/amazon-vpc-resource-controller-k8s) to attach a Trunk ENI to the instance. The label value is initially set to `false` and is marked to `true` by IPAMD when vpc-resource-controller attaches a Trunk ENI to the instance. However, there might be cases where the label value will remain `false` if the instance doesn't support ENI Trunking.

Once enabled the VPC resource controller will then advertise branch network interfaces as extended resources on these nodes in your cluster. Branch interface capacity is additive to existing instance type limits for secondary IP addresses and prefixes. For example, a c5.4xlarge can continue to have up to 234 secondary IP addresses or 234 /28 prefixes assigned to standard network interfaces and up to 54 branch network interfaces. Each branch network interface only receives a single primary IP address and this IP address will be allocated to pods with a security group(branch ENI pods).

Any of the WARM targets do not impact the scale of the branch ENI pods so you will have to set the WARM_{ENI/IP/PREFIX}_TARGET based on the number of non-branch ENI pods. If you are having the cluster mostly using pods with a security group consider setting WARM_IP_TARGET to a very low value instead of default WARM_ENI_TARGET or WARM_PREFIX_TARGET to reduce wastage of IPs/ENIs.

**NOTE!** Toggling `ENABLE_POD_ENI` from `true` to `false` will not detach the Trunk ENI from an instance. To delete/detach the Trunk ENI from an instance, you need to recycle the instance.

#### `POD_SECURITY_GROUP_ENFORCING_MODE` (v1.11.0+)

Type: String

Default: `strict`

Valid Values: `strict`, `standard`

Once `ENABLE_POD_ENI` is set to `true`, this value controls how the traffic of pods with the security group behaves.

* `strict` mode: all inbound/outbound traffic from pod with security group will be enforced by security group rules. This is the **default** mode if POD_SECURITY_GROUP_ENFORCING_MODE is not set.

* `standard` mode: the traffic of pod with security group behaves same as pods without a security group, except that each pod occupies a dedicated branch ENI.
  * inbound traffic to pod with security group from another host will be enforced by security group rules.
  * outbound traffic from pod with security group to another host in the same VPC will be enforced by security group rules.
  * inbound/outbound traffic from another pod on the same host or another service on the same host(such as kubelet/nodeLocalDNS) won't be enforced by security group rules.
  * outbound traffic from pod with security group to IP address outside VPC
    * if externalSNAT enabled, traffic won't be SNATed, thus will be enforced by security group rules.
    * if externalSNAT disabled, traffic will be SNATed via eth0, thus will only be enforced by the security group associated with eth0.

**NOTE!**: To make new behavior be in effect after switching the mode, existing pods with security group must be recycled. Alternatively, you can restart the nodes as well.

#### `DISABLE_TCP_EARLY_DEMUX` (v1.7.3+)

Type: Boolean as a String

Default: `false`

If `ENABLE_POD_ENI` is set to `true`, for the kubelet to connect via TCP (for liveness or readiness probes)
to pods that are using per pod security groups, `DISABLE_TCP_EARLY_DEMUX` should be set to `true` for `amazon-k8s-cni-init`
the container under `initcontainers`. This will increase the local TCP connection latency slightly.
Details on why this is needed can be found in this [#1212 comment](https://github.com/aws/amazon-vpc-cni-k8s/pull/1212#issuecomment-693540666).
To use this setting, a Linux kernel version of at least 4.6 is needed on the worker node.

You can use the below command to enable `DISABLE_TCP_EARLY_DEMUX` to `true` -

```
kubectl patch daemonset aws-node -n kube-system -p '{"spec": {"template": {"spec": {"initContainers": [{"env":[{"name":"DISABLE_TCP_EARLY_DEMUX","value":"true"}],"name":"aws-vpc-cni-init"}]}}}}'
```

#### `ENABLE_PREFIX_DELEGATION` (v1.9.0+)

Type: Boolean as a String

Default: `false`

To enable prefix delegation on nitro instances. Setting `ENABLE_PREFIX_DELEGATION` to `true` will start allocating a prefix (/28 for IPv4
and /80 for IPv6) instead of a secondary IP in the ENIs subnet. The total number of prefixes and private IP addresses will be less than the
limit on private IPs allowed by your instance. Setting or resetting of `ENABLE_PREFIX_DELEGATION` while pods are running or if ENIs are attached is supported and the new pods allocated will get IPs based on the mode of IPAMD but the max pods of kubelet should be updated which would need either kubelet restart or node recycle.

Setting ENABLE_PREFIX_DELEGATION to true will not increase the density of branch ENI pods. The limit on the number of [branch network interfaces per instance type will remain the same.](https://docs.aws.amazon.com/eks/latest/userguide/security-groups-for-pods.html#supported-instance-types) Each branch network will be allocated a primary IP and this IP will be allocated for the branch ENI pods.

Please refer to [VPC CNI Feature Matrix](https://github.com/aws/amazon-vpc-cni-k8s#vpc-cni-feature-matrix) section below for additional information around using Prefix delegation with Custom Networking and Security Groups Per Pod features.

**Note:** `ENABLE_PREFIX_DELEGATION` needs to be set to `true` when VPC CNI is configured to operate in IPv6 mode (supported in v1.10.0+). Prefix Delegation in IPv4 and IPv6 modes is supported on Nitro based Bare Metal instances as well from v1.11+. If you're using Prefix Delegation feature on Bare Metal instances, downgrading to an earlier version of VPC CNI from v1.11+ will be disruptive and not supported.

#### `WARM_PREFIX_TARGET` (v1.9.0+)

Type: Integer

Default: None

Specifies the number of free IPv4(/28) prefixes that the `ipamd` daemon should attempt to keep available for pod assignment on the node. Setting to a non-positive value is same as setting this to 0 or not setting the variable.
This environment variable works when `ENABLE_PREFIX_DELEGATION` is set to `true` and is overridden when `WARM_IP_TARGET` and `MINIMUM_IP_TARGET` are configured.

#### `DISABLE_NETWORK_RESOURCE_PROVISIONING` (v1.9.1+)

Type: Boolean as a String

Default: `false`

Setting `DISABLE_NETWORK_RESOURCE_PROVISIONING` to `true` will make IPAMD depend only on IMDS to get attached ENIs and IPs/prefixes.

#### `ENABLE_BANDWIDTH_PLUGIN` (v1.10.0+)

Type: Boolean as a String

Default: `false`

Setting `ENABLE_BANDWIDTH_PLUGIN` to `true` will update `10-aws.conflist` to include upstream [bandwidth plugin](https://www.cni.dev/plugins/current/meta/bandwidth/) as a chained plugin.

NOTE: Kubernetes Network Policy is supported in Amazon VPC CNI starting with version v1.14.0. Note that bandwidth plugin is not compatible with Amazon VPC CNI based Network policy. Network Policy agent uses TC (traffic classifier) system to enforce configured network policies for the pods. The policy enforcement will fail if bandwidth plugin is enabled due to conflict between TC configuration of bandwidth plugin and Network policy agent. We're exploring options to support bandwidth plugin along with Network policy feature and the issue is tracked [here](https://github.com/aws/aws-network-policy-agent/issues/68)

#### `ANNOTATE_POD_IP` (v1.9.3+)

Type: Boolean as a String

Default: `false`

Setting `ANNOTATE_POD_IP` to `true` will allow IPAMD to add an annotation `vpc.amazonaws.com/pod-ips` to the pod with pod IP.

There is a known [issue](https://github.com/kubernetes/kubernetes/issues/39113) with kubelet taking time to update `Pod.Status.PodIP` leading to calico being blocked on programming the policy. Setting `ANNOTATE_POD_IP` to `true` will enable AWS VPC CNI plugin to add Pod IP as an annotation to the pod spec to address this race condition.

To annotate the pod with pod IP, you will have to add "patch" permission for pods resource in aws-node clusterrole. You can use the below command -

```
cat << EOF > append.yaml
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - patch
EOF
```

```
kubectl apply -f <(cat <(kubectl get clusterrole aws-node -o yaml) append.yaml)
```

#### `ENABLE_IPv4` (v1.10.0+)

Type: Boolean as a String

Default: `true`

VPC CNI can operate in either IPv4 or IPv6 mode. Setting `ENABLE_IPv4` to `true` will configure it in IPv4 mode (default mode).

**Note:** Dual-stack mode isn't yet supported. So, enabling both IPv4 and IPv6 will be treated as an invalid configuration.

#### `ENABLE_IPv6` (v1.10.0+)

Type: Boolean as a String

Default: `false`

VPC CNI can operate in either IPv4 or IPv6 mode. Setting `ENABLE_IPv6` to `true` (both under `aws-node` and `aws-vpc-cni-init` containers in the manifest)
will configure it in IPv6 mode. IPv6 is only supported in Prefix Delegation mode, so `ENABLE_PREFIX_DELEGATION` needs to be set to `true` if VPC CNI is
configured to operate in IPv6 mode. Prefix delegation is only supported on nitro instances.

**Note:** Please make sure that the required IPv6 IAM policy is applied (Refer to [IAM Policy](https://github.com/aws/amazon-vpc-cni-k8s#iam-policy) section above). Dual stack mode isn't yet supported. So, enabling both IPv4 and IPv6 will be treated as invalid configuration. Please refer to the [VPC CNI Feature Matrix](https://github.com/aws/amazon-vpc-cni-k8s#vpc-cni-feature-matrix) section below for additional information.

#### `ENABLE_NFTABLES` (introduced in v1.12.1, deprecated in v1.13.2+)

Type: Boolean as a String

Default: `false`

VPC CNI uses `iptables-legacy` by default. Setting `ENABLE_NFTABLES` to `true` will update VPC CNI to use `iptables-nft`.

**Note:** VPC CNI image contains `iptables-legacy` and `iptables-nft`. Switching between them is done via `update-alternatives`. It is *strongly* recommended that the iptables mode matches that which is used by the base OS and `kube-proxy`.
Switching modes while pods are running or rules are installed will not trigger reconciliation. It is recommended that rules are manually updated or nodes are drained and cordoned before updating. If reloading node, ensure that previous rules are not set to be persisted.

#### `AWS_EXTERNAL_SERVICE_CIDRS` (v1.12.6+)

Type: String

Default: empty

Specify a comma-separated list of IPv4 CIDRs that *must* be routed via main routing table. This is required for secondary ENIs to reach endpoints outside of VPC that are backed by a service.
For every item in the list, an `ip rule` will be created with a priority greater than the `ip rule` capturing egress traffic from the container. If an item is not a valid IPv4 CIDR, it will be skipped.

#### `AWS_EC2_ENDPOINT` (v1.13.0+)

Type: String

Default: empty

Specify the EC2 endpoint to use. This is useful if you are using a custom endpoint for EC2. For example, if you are using a proxy for EC2, you can set this to the proxy endpoint. Any kind of URL or IP address is valid such as `https://localhost:8080` or `http://ec2.us-west-2.customaws.com`. If this is not set, the default EC2 endpoint will be used.

#### `DISABLE_LEAKED_ENI_CLEANUP` (v1.13.0+)

Type: Boolean as a String

Default: `false`

On IPv4 clusters, IPAMD schedules an hourly background task per node that cleans up leaked ENIs. Setting this environment variable to `true` disables that job. The primary motivation to disable this task is to decrease the amount of EC2 API calls made from each node.
Note that disabling this task should be considered carefully, as it requires users to manually cleanup ENIs leaked in their account. See [#1223](https://github.com/aws/amazon-vpc-cni-k8s/issues/1223) for a related discussion.

#### `ENABLE_V6_EGRESS` (v1.13.0+)

Type: Boolean as a String

Default: `false`

Specifies whether PODs in an IPv4 cluster support IPv6 egress. If env is set to `true`, range `fd00::ac:00/118` is reserved for IPv6 egress.

This environment variable must be set for both the `aws-vpc-cni-init` and `aws-node` containers in order for this feature to work properly. This feature also requires that the node has an IPv6 address assigned to its primary ENI, as this address is used for SNAT to IPv6 endpoints outside of the cluster. If the configuration prerequisites are not met, the `egress-cni` plugin is not enabled and an error log is printed in the `aws-node` container.

Note that enabling/disabling this feature only affects whether newly created pods have an IPv6 interface created. Therefore, it is recommended that you reboot existing nodes after enabling/disabling this feature.

#### `ENABLE_V4_EGRESS` (v1.15.1+)

Type: Boolean as a String

Default: `true`

Specifies whether PODs in an IPv6 cluster support IPv4 egress. If env is set to `true`, range `169.254.172.0/22` is reserved for IPv4 egress. When enabled, traffic egressing an IPv6 pod destined to an IPv4 endpoint will be SNAT'ed via the node IPv4 address.

Note that enabling/disabling this feature only affects whether newly created pods have an IPv4 interface created. Therefore, it is recommended that you reboot existing nodes after enabling/disabling this feature.

#### `IP_COOLDOWN_PERIOD` (v1.15.0+)

Type: Integer as a String

Default: `30`

Specifies the number of seconds an IP address is in cooldown after pod deletion. The cooldown period gives network proxies, such as kube-proxy, time to update node iptables rules when the IP was registered as a valid endpoint, such as for a service. Modify this value with caution, as kube-proxy update time scales with the number of nodes and services.

**Note:** 0 is a supported value, however it is highly discouraged.
**Note:** Higher cooldown periods may lead to a higher number of EC2 API calls as IPs are in cooldown cache.

#### `DISABLE_POD_V6` (v1.15.0+)

Type: Boolean as a String

Default: `false`

When `DISABLE_POD_V6` is set, the [tuning plugin](https://www.cni.dev/plugins/current/meta/tuning/) is chained and configured to disable IPv6 networking in each newly created pod network namespace. Set this variable when you have an IPv4 cluster and containerized applications that cannot tolerate IPv6 being enabled.
Container runtimes such as `containerd` will enable IPv6 in newly created container network namespaces regardless of host settings.

Note that if you set this while using Multus, you must ensure that any chained plugins do not depend on IPv6 networking. You must also ensure that chained plugins do not also modify these sysctls.

### VPC CNI Feature Matrix


| IP Mode | Secondary IP Mode | Prefix Delegation | Security Groups Per Pod | WARM & MIN IP/Prefix Targets | External SNAT | Network Policies |
|---------|-------------------|-------------------|-------------------------|------------------------------|---------------|------------------|
| `IPv4`  | Yes               | Yes               | Yes                     | Yes                          | Yes           | Yes              |
| `IPv6`  | No                | Yes               | No                      | No                           | No            | Yes              |

## ENI tags related to Allocation

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

## Container Runtime

For VPC CNI >=v1.12.0, IPAMD have switched to use an on-disk file `/var/run/aws-node/ipam.json` to track IP allocations, thus became container runtime agnostic and no longer requires access to Container Runtime Interface(CRI) socket.

* **Note**:
  * Helm chart >=v1.2.0 is released with VPC CNI v1.12.0, thus no longer supports the `cri.hostPath.path`. If you need to install a VPC CNI <v1.12.0 with helm chart, a Helm chart version that <v1.2.0 should be used.

For VPC CNI <v1.12.0, IPAMD still depends on CRI to track IP allocations using pod sandboxes information upon its starting.

* By default the dockershim CRI socket was mounted but can be customized to use other CRI:
  * The mountPath should be changed to `/var/run/cri.sock` and hostPath should be pointed to CRI used by kubelet, such as `/var/run/containerd/containerd.sock` for containerd.
  * With Helm chart <v1.2.0, the flag `--set cri.hostPath.path=/var/run/containerd/containerd.sock` can set above for you.
* **Note**:
  * When using a different container runtime instead of the default dockershim in VPC CNI, make sure kubelet is also configured to use the same CRI.
  * If you want to enable containerd runtime with the support provided by Amazon AMI, please follow the instructions in our documentation, [Enable the containerd runtime bootstrap flag](https://docs.aws.amazon.com/eks/latest/userguide/eks-optimized-ami.html#containerd-bootstrap)

## Notes

`L-IPAMD`(aws-node daemonSet) running on every worker node requires access to the Kubernetes API server. If it can **not** reach
the Kubernetes API server, ipamd will exit and CNI will not be able to get any IP address for Pods. Here is a way to confirm if
`aws-node` has access to the Kubernetes API server.

```
# find out Kubernetes service IP, e.g. 10.0.0.1
kubectl get svc Kubernetes
NAME         TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
kubernetes   ClusterIP   10.0.0.1   <none>        443/TCP   29d

# ssh into worker node, check if worker node can reach API server
telnet 10.0.0.1 443
Trying 10.0.0.1...
Connected to 10.0.0.1.
Escape character is '^]'.  <-------- Kubernetes API server is reachable
```

## Security disclosures

If you think you’ve found a potential security issue, please do not post it in the Issues. Instead, please follow the
instructions [here](https://aws.amazon.com/security/vulnerability-reporting/) or [email AWS security directly](mailto:aws-security@amazon.com).

## Contributing

[See CONTRIBUTING.md](./CONTRIBUTING.md)
