# Changelog

## v1.4.1

* Feature - [Add flag to disable metrics and introspection](https://github.com/aws/amazon-vpc-cni-k8s/pull/436) (#436, @mogren)
* Bug fix - [Adding additional CRD for Calico that was missing](https://github.com/aws/amazon-vpc-cni-k8s/pull/410) (#410, @wmorgan6796)
* Improvement - [Update CNI metrics](https://github.com/aws/amazon-vpc-cni-k8s/pull/413) (#413, @mogren)

## v1.4.0

* Feature - [Add an environment variable to limit the number of ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/251) (#251, @pdbogen)
    - Makes it possible to limit how many ENIs that are allocated per node.
* Feature - [Randomize outgoing port for connections in the SNAT iptables rule](https://github.com/aws/amazon-vpc-cni-k8s/pull/246) (#246, @taylorb-syd)
    - To avoid a race condition when using SNAT, select ports randomly instead of sequentially.
* Feature - [ENIConfig set by custom annotation or label names](https://github.com/aws/amazon-vpc-cni-k8s/pull/280) (#280, @etopeter)
    - Enables users to set a custom annotation or label key to define ENIConfig name.
* Improvement - [Update Calico to 3.3.6](https://github.com/aws/amazon-vpc-cni-k8s/pull/368) (#368, @2ffs2nns)
* Improvement - [Add new instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/366) (#366, @mogren)
    - Adds m5ad and r5ad families.
* Improvement - [Actually enable prometheus metrics](https://github.com/aws/amazon-vpc-cni-k8s/pull/361) (#361, @mogren)
* Improvement - [Retry LinkByMac when link not found](https://github.com/aws/amazon-vpc-cni-k8s/pull/360) (#360, @peterbroadhurst)
    - Sometimes it takes a few seconds for a new ENI to be available, so we retry 5 times.
* Improvement - [Run `yum clean all` to reduce image size](https://github.com/aws/amazon-vpc-cni-k8s/pull/351) (#351, @mogren)
* Improvement - [Renaming Prometheus metrics with "awscni_" prefix](https://github.com/aws/amazon-vpc-cni-k8s/pull/348) (#348, @max-rocket-internet)
* Improvement - [Allow configuring docker image when running make](https://github.com/aws/amazon-vpc-cni-k8s/pull/178) (#178, @mikkeloscar)
* Improvement - [Add support for stdout logging](https://github.com/aws/amazon-vpc-cni-k8s/pull/342) (#342, @rudoi)
    - Adds the environment variable `AWS_VPC_K8S_CNI_LOG_FILE` that can be set to `stdout` or a file path.
* Improvement - [Some cleanups related to #234](https://github.com/aws/amazon-vpc-cni-k8s/pull/244) (#244, @mogren)
* Improvement - [Use apps/v1 for DaemonSet](https://github.com/aws/amazon-vpc-cni-k8s/pull/341) (#341, @errordeveloper)
* Improvement - [Clean up aws-cni-support.sh and update the documentation](https://github.com/aws/amazon-vpc-cni-k8s/pull/320) (#320, @mogren)
* Improvement - [Fix tiny typo in log message](https://github.com/aws/amazon-vpc-cni-k8s/pull/324) (#323, #324, @ankon)
* Improvement - [Collect rp_filter from all network interface in aws-cni-support.sh](https://github.com/aws/amazon-vpc-cni-k8s/pull/338) (#338, @nak3)
* Improvement - [Use device number 0 for primary device in unit test](https://github.com/aws/amazon-vpc-cni-k8s/pull/247) (#247, @nak3)
* Improvement - [Collect iptables -nvL -t mangle in support script](https://github.com/aws/amazon-vpc-cni-k8s/pull/304) (#304, @nak3)
* Improvement - [Return the err from f.Close()](https://github.com/aws/amazon-vpc-cni-k8s/pull/249) (#249, @mogren)
* Improvement - [Explicitly set the IP on secondary ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/271) (#271, @ewbankkit)
    - Fixes IP bug on older kernels.
* Improvement - [Update instance ENI and IP mapping table](https://github.com/aws/amazon-vpc-cni-k8s/pull/275) (#275, @hmizuma)
    - Adds a1 and c5n instances. (Already included in v1.3.2) 
* Improvement - [Add ENI entries for p3dn.24xlarge instance](https://github.com/aws/amazon-vpc-cni-k8s/pull/274) (#274, @hmizuma)
    - p3dn.24xlarge was already included in v1.3.2 
* Improvement - [Use InClusterConfig when CreateKubeClient() was called without args](https://github.com/aws/amazon-vpc-cni-k8s/pull/293) (#293, @nak3)
* Improvement - [Expose configuration variables via ipamD to make it debug friendly](https://github.com/aws/amazon-vpc-cni-k8s/pull/287) (#287, @nak3)
* Improvement - [Allow cross compile on different platform ](https://github.com/aws/amazon-vpc-cni-k8s/pull/292) (#292, @nak3)
* Improvement - [Add changes to support multiple platform build](https://github.com/aws/amazon-vpc-cni-k8s/pull/286) (#286, @mbartsch)
    - arm64 build support
* Improvement - [Improve setup advice in README around ENI / IP ](https://github.com/aws/amazon-vpc-cni-k8s/pull/276) (#276 @sftim)
* Improvement - [Use `unix.RT_TABLE_MAIN` for main routing table number](https://github.com/aws/amazon-vpc-cni-k8s/pull/269) (#269, @nak3)
* Improvement - [Detect if mockgen and goimports are in the path](https://github.com/aws/amazon-vpc-cni-k8s/pull/278) (#278, @nak3)
* Improvement - [Increment IP address safely](https://github.com/aws/amazon-vpc-cni-k8s/pull/258) (#258, @nak3)
    - Calculate the gateway IP in a safe way.
* Improvement - [Remove unused options from rpc.proto](https://github.com/aws/amazon-vpc-cni-k8s/pull/252) (#252, @nak3)
* Improvement - [Add missing unit tests execution to Makefile](https://github.com/aws/amazon-vpc-cni-k8s/pull/253) (#253, @nak3)
* Improvement - [Bump TravisCI to use 1.11](https://github.com/aws/amazon-vpc-cni-k8s/pull/243) (#243, @mogren)
* Bug fix - [Fix typos in json types for ENIConfig](https://github.com/aws/amazon-vpc-cni-k8s/pull/393) (#393, @tiffanyfay)
* Bug fix - [Avoid unbound variable error in aws-cni-support.sh](https://github.com/aws/amazon-vpc-cni-k8s/pull/382) (#382, @StevenACoffman)
* Bug fix - [Output CIDR in correct format](https://github.com/aws/amazon-vpc-cni-k8s/pull/267) (#267, @nak3)
* Bug fix - [Use replace when adding host route](https://github.com/aws/amazon-vpc-cni-k8s/pull/367) (#367, @mogren)
* Bug fix - [Update k8sapi to use operator-framework inClusterConfig](https://github.com/aws/amazon-vpc-cni-k8s/pull/364) (#364, @tiffanyfay)
    - If the environment variables are missing, fall back to DNS lookup.
* Bug fix - [Set mainENIRule mask](https://github.com/aws/amazon-vpc-cni-k8s/pull/340) (#340, @tustvold)
    - In order to match the connmark correctly, we need to mask it out when checking.
* Bug fix - [Use primary interface to add iptables for connmark entry](https://github.com/aws/amazon-vpc-cni-k8s/pull/305) (#305, @nak3)
* Bug fix - [Stop wrapping and returning nil](https://github.com/aws/amazon-vpc-cni-k8s/pull/245) (#245, @nak3)
* Bug fix - [Fix return path of NodePort traffic when using Calico network policy](https://github.com/aws/amazon-vpc-cni-k8s/pull/263) (#263, @ikatson)
* Bug fix - [Remove scope: Cluster from spec.names](https://github.com/aws/amazon-vpc-cni-k8s/pull/199) (#199, @rickardrosen)
* Bug fix - [Remove unneeded spec entry in v1.3 manifest](https://github.com/aws/amazon-vpc-cni-k8s/pull/262) (#262, @hmizuma)
* Bug fix - [Add formatter to errors.Wrapf in driver](https://github.com/aws/amazon-vpc-cni-k8s/pull/241) (#241, @nak3)

## v1.3.3

* Feature - [Add ENI entries for a1 and c5n instance families](https://github.com/aws/amazon-vpc-cni-k8s/pull/349)

## v1.3.2

* Bug fix - [Fix max pods for p3dn.24xlarge](https://github.com/aws/amazon-vpc-cni-k8s/pull/312)
* Bug fix - [Bump CNI to latest 1.3 version](https://github.com/aws/amazon-vpc-cni-k8s/pull/333)

## v1.3.1

* Feature - [Add ENI entries for p3dn.24xlarge](https://github.com/aws/amazon-vpc-cni-k8s/pull/295)
* Bug fix - [Restrict p3dn.24xlarge to 31 IPs/ENI](https://github.com/aws/amazon-vpc-cni-k8s/pull/300)

## v1.3.0

* Feature - [Add logic to handle multiple VPC CIDRs](https://github.com/aws/amazon-vpc-cni-k8s/pull/234)
* Improvement - [Update instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/229)
* Improvement - [Add retry for plumbing route entry](https://github.com/aws/amazon-vpc-cni-k8s/pull/223)
* Improvement - [Update vpc_ip_resource_limit.go](https://github.com/aws/amazon-vpc-cni-k8s/pull/221)
* Improvement - [Add support for g3s.xlarge machines](https://github.com/aws/amazon-vpc-cni-k8s/pull/218)
* Improvement - [Fixing t3.xl and t3.2xl eni numbers](https://github.com/aws/amazon-vpc-cni-k8s/pull/197)
* Improvement - [Configure MTU of ENI and veths to 9001](https://github.com/aws/amazon-vpc-cni-k8s/pull/210)
* Bug fix - [Update containerPort in the spec](https://github.com/aws/amazon-vpc-cni-k8s/pull/207)
* Bug fix - [cleanup the host route when perform CNI delete](https://github.com/aws/amazon-vpc-cni-k8s/pull/228)

## 1.2.1

* Bug fix - [Add missing calico.yaml to 1.2](https://github.com/aws/amazon-vpc-cni-k8s)
* Bug fix - [Do not watch eniconfig CRD if cni is not configured to use pod config](https://github.com/aws/amazon-vpc-cni-k8s/pull/192)
* Bug fix - [Fixed typo in aws-k8s-cni.yaml](https://github.com/aws/amazon-vpc-cni-k8s/pull/185)
* Bug fix - [Add logic to dynamically discover primary interface name](https://github.com/aws/amazon-vpc-cni-k8s/pull/196)

## 1.2.0

* Feature - Add hostPort support [#153](https://github.com/aws/amazon-vpc-cni-k8s/pull/153)
* Feature - Add a configuration knob to allow Pod to use different VPC SecurityGroups and Subnet [#165](https://github.com/aws/amazon-vpc-cni-k8s/pull/165)
* Feature - Fix return path of NodePort traffic [#130](https://github.com/aws/amazon-vpc-cni-k8s/pull/130)
* Improvement - Add more error messages during initialization [#174](https://github.com/aws/amazon-vpc-cni-k8s/pull/174)
* Improvement - Check to make it is a Pod object [#170](https://github.com/aws/amazon-vpc-cni-k8s/pull/170)
* Improvement - Maintain the right number of ENIs and its IP addresses in WARM-IP pool [#169](https://github.com/aws/amazon-vpc-cni-k8s/pull/169)
* Improvement - Add support for more instance types: r5, r5d, z1d, t3 [#145](https://github.com/aws/amazon-vpc-cni-k8s/pull/145)

## 1.1.0

* Feature - Versioning with git SHA [#106](https://github.com/aws/amazon-vpc-cni-k8s/pull/106)
* Feature - Ability to configure secondary IP preallocation (https://github.com/aws/amazon-vpc-cni-k8s/pull/125)
* Feature - Allow pods communicate with outside VPC without NAT[#81](https://github.com/aws/amazon-vpc-cni-k8s/pull/81)
* Improvement - Added travis CI support [#116](https://github.com/aws/amazon-vpc-cni-k8s/pull/116), [#117](https://github.com/aws/amazon-vpc-cni-k8s/pull/117), [#118](https://github.com/aws/amazon-vpc-cni-k8s/pull/118)
* Improvement - Modify toleration to make aws-node schedule-able on all nodes [#128](https://github.com/aws/amazon-vpc-cni-k8s/pull/128)
* Improvement - Move from TagResources to CreateTags for ENI Tagging [#129](https://github.com/aws/amazon-vpc-cni-k8s/pull/129)
* Improvement - Updated troubleshooting guidelines
* Bug Fix - Release IP to datastore upon failure [#127](https://github.com/aws/amazon-vpc-cni-k8s/pull/127)

## 1.0.0

Initial release of **amazon-vpc-cni-k8s**  a cni plugin for use with Kubernetes that uses ENIs and secondary ip addresses.

See the [README](README.md) for additional information.
