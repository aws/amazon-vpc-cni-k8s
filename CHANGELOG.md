# Changelog

## v1.6.1
* Feature - [Support architecture targeted builds](https://github.com/aws/amazon-vpc-cni-k8s/pull/837) (#837, @jahkeup)
* Feature - [Zap logger](https://github.com/aws/amazon-vpc-cni-k8s/pull/824) (#824, @nithu0115)
* Improvement - [Run conformance test as part of PR/Release certification](https://github.com/aws/amazon-vpc-cni-k8s/pull/851) (#851, @SaranBalaji90)
* Improvement - [Use eks:cluster-name as clusterId](https://github.com/aws/amazon-vpc-cni-k8s/pull/856) (#856, @groodt)
* Improvement - [Bump Calico to v3.13.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/857) (#857, @lmm)
* Improvement - [Use go.mod version of mockgen](https://github.com/aws/amazon-vpc-cni-k8s/pull/863) (#863, @anguslees)
* Improvement - [Mock /proc/sys](https://github.com/aws/amazon-vpc-cni-k8s/pull/870) (#870, @anguslees)
* Improvement - [Replace debug script with updated script from EKS AMI](https://github.com/aws/amazon-vpc-cni-k8s/pull/864) (#864, @mogren)
* Improvement - [Update cluster-proportional-autoscaler to 1.7.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/885) (#885, @ricardochimal)
* Improvement - [Remove unnecessary/incorrect ClusterRole resource](https://github.com/aws/amazon-vpc-cni-k8s/pull/883) (#883, @anguslees)
* Improvement - [Disable IPv6 RA and ICMP redirects](https://github.com/aws/amazon-vpc-cni-k8s/pull/897) (#897, @anguslees)
* Improvement - [scripts/lib/aws.sh: use "aws-k8s-tester" v1.0.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/900) (#900, @gyuho)
* Improvement - [Configure rp_filter based on env variable](https://github.com/aws/amazon-vpc-cni-k8s/pull/902) (#902, @SaranBalaji90)
* Improvement - [Less verbose logging](https://github.com/aws/amazon-vpc-cni-k8s/pull/908) (#908, @mogren)
* Improvement - [Reduce number of calls to EC2 API](https://github.com/aws/amazon-vpc-cni-k8s/pull/909) (#909, @mogren)
* Improvement - [Bump containernetworking dependencies](https://github.com/aws/amazon-vpc-cni-k8s/pull/916) (#916, @mogren)
* Improvement - [Use -buildmode=pie for binaries](https://github.com/aws/amazon-vpc-cni-k8s/pull/919) (#919, @mogren)
* Bug - [Add missing permissions in typha-cpha sa (Calico)](https://github.com/aws/amazon-vpc-cni-k8s/pull/892) (#892, @marcincuber)
* Bug - [Fix logging to stdout](https://github.com/aws/amazon-vpc-cni-k8s/pull/904) (#904, @mogren)
* Bug - [Ensure non-nil Attachment in getENIAttachmentID](https://github.com/aws/amazon-vpc-cni-k8s/pull/915) (#915, @jaypipes)

## v1.6.0

* Feature - [Add fallback to fetch limits from EC2 API](https://github.com/aws/amazon-vpc-cni-k8s/pull/782) (#782, @mogren)
* Feature - [Additional tags to ENI](https://github.com/aws/amazon-vpc-cni-k8s/pull/734) (#734, @nithu0115)
* Feature - [Add support for a 'no manage' tag](https://github.com/aws/amazon-vpc-cni-k8s/pull/726) (#726, @euank)
* Feature - [Use CRI to obtain pod sandbox IDs instead of Kubernetes API](https://github.com/aws/amazon-vpc-cni-k8s/pull/714) (#714, @drakedevel)
* Feature - [Add support for listening on unix socket for introspection endpoint](https://github.com/aws/amazon-vpc-cni-k8s/pull/713) (#713, @adammw)
* Feature - [Add MTU to the plugin config](https://github.com/aws/amazon-vpc-cni-k8s/pull/676) (#676, @mogren)
* Feature - [Clean up leaked ENIs on startup](https://github.com/aws/amazon-vpc-cni-k8s/pull/624) (#624, @mogren)
* Feature - [Introduce a minimum target for ENI IPs](https://github.com/aws/amazon-vpc-cni-k8s/pull/612) (#612, @asheldon)
* Feature - [Allow peered VPC CIDRs to be excluded from SNAT](https://github.com/aws/amazon-vpc-cni-k8s/pull/520) (#520, @totahuanocotl, @rewiko, @yorg1st)
* Feature - [Get container ID from kube rather than docker](https://github.com/aws/amazon-vpc-cni-k8s/pull/371) (#371, @rudoi)
* Improvement - [Make entrypoint script fail if any step fails](https://github.com/aws/amazon-vpc-cni-k8s/pull/839) (#839, @drakedevel)
* Improvement - [Place binaries in cmd/ and packages in pkg/](https://github.com/aws/amazon-vpc-cni-k8s/pull/815) (#815, @jaypipes)
* Improvement - [De-dupe calls to DescribeNetworkInterfaces](https://github.com/aws/amazon-vpc-cni-k8s/pull/808) (#808, @jaypipes)
* Improvement - [Update RollingUpdate strategy to allow 10% unavailable](https://github.com/aws/amazon-vpc-cni-k8s/pull/805) (#805, @gavinbunney)
* Improvement - [Bump github.com/vishvananda/netlink version from 1.0.0 to 1.1.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/801) (#802, @ajayk)
* Improvement - [Adding node affinity for Fargate](https://github.com/aws/amazon-vpc-cni-k8s/pull/792) (#792, @nithu0115)
* Improvement - [Force ENI/IP reconciliation to delete from the datastore](https://github.com/aws/amazon-vpc-cni-k8s/pull/754) (#754, @tatatodd)
* Improvement - [Use dockershim.sock for CRI](https://github.com/aws/amazon-vpc-cni-k8s/pull/751) (#751, @mogren)
* Improvement - [Treating ErrUnknownPod from ipamd to be a noop and not returning error](https://github.com/aws/amazon-vpc-cni-k8s/pull/750) (#750, @uruddarraju)
* Improvement - [Copy CNI plugin and config in entrypoint not agent](https://github.com/aws/amazon-vpc-cni-k8s/pull/735) (#735, @jaypipes)
* Improvement - [Adding m6g instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/742) (#742, Srini Ramabadran)
* Improvement - [Remove deprecated session.New method](https://github.com/aws/amazon-vpc-cni-k8s/pull/729) (#729, @nithu0115)
* Improvement - [Scope watch on "pods" to only pods associated with the local node](https://github.com/aws/amazon-vpc-cni-k8s/pull/716) (#716, @jacksontj)
* Improvement - [Update ENI limits to match documentation](https://github.com/aws/amazon-vpc-cni-k8s/pull/710) (#710, @mogren)
* Improvement - [Reduce image layers and strip debug flags](https://github.com/aws/amazon-vpc-cni-k8s/pull/699) (#699, @mogren)
* Improvement - [Add run-integration-tests.sh script](https://github.com/aws/amazon-vpc-cni-k8s/pull/698) (#698, @nckturner)
* Improvement - [Return the error from ipamd to plugin](https://github.com/aws/amazon-vpc-cni-k8s/pull/688) (#688, @mogren)
* Improvement - [Bump aws-sdk-go to v1.23.13](https://github.com/aws/amazon-vpc-cni-k8s/pull/681) (#681, @mogren)
* Improvement - [Add support for m5n/m5dn/r5n/r5dn instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/657) (#657, @Jeffwan)
* Improvement - [Add IPs to the first ENI on startup](https://github.com/aws/amazon-vpc-cni-k8s/pull/648) (#648, @mogren)
* Improvement - [Add shutdown listener](https://github.com/aws/amazon-vpc-cni-k8s/pull/645) (#645, @mogren)
* Improvement - [Made timeouts exponential](https://github.com/aws/amazon-vpc-cni-k8s/pull/640) (#640, @Zyqsempai)
* Improvement - [Remove vendor folder](https://github.com/aws/amazon-vpc-cni-k8s/pull/635) (#635, @mogren)
* Improvement - [Update protobuf to v1.3.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/633) (#633, @mogren)
* Improvement - [Reduce log level to Trace for the most common Debug lines](https://github.com/aws/amazon-vpc-cni-k8s/pull/631) (#631, @mogren)
* Improvement - [Bump grpc version to v1.23.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/629) (#629, @mogren)
* Improvement - [Add inCoolingPeriod for AddressInfo](https://github.com/aws/amazon-vpc-cni-k8s/pull/627) (#627, @chendotjs)
* Improvement - [Added retryNbackoff for tagENI method](https://github.com/aws/amazon-vpc-cni-k8s/pull/626) (#626, @nithu0115)
* Improvement - [Update backoff code from upstream and use when detaching ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/623) (#623, @mogren)
* Improvement - [Update kubeconfig lookup with eksctl clusters](https://github.com/aws/amazon-vpc-cni-k8s/pull/513) (#513, @dkeightley)
* Improvement - [Fix introspection port in troubleshooting docs](https://github.com/aws/amazon-vpc-cni-k8s/pull/512) (#512, @drakedevel)
* Bug fix - [Log security groups correctly](https://github.com/aws/amazon-vpc-cni-k8s/pull/646) (#646, @mogren)
* Bug fix - [Fix WARM_ENI_TARGET=0](https://github.com/aws/amazon-vpc-cni-k8s/pull/587) (#587, @mogren)

# v1.5.7

* Improvement - [New AL2 image with iptables-1.8.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/894) (@mogren)
* Improvement - [Enable the `-buildmode=pie` flag for the binaries](https://github.com/aws/amazon-vpc-cni-k8s/pull/894) (@mogren)
* Improvement - [Disable IPv6 RA and ICMP redirects on host-side veth](https://github.com/aws/amazon-vpc-cni-k8s/pull/894) (@anguslees)

# v1.5.6

* arm64 preview custom build

# v1.5.5

* Bug fix - [Revert "Return delete success for pods that never got scheduled"](https://github.com/aws/amazon-vpc-cni-k8s/pull/672/commits/474479d7455f41c514ffcd58390a2a3ebae26de5) (#672, @mogren)
* Improvement - [Add support for r5dn instance family](https://github.com/aws/amazon-vpc-cni-k8s/pull/656) (#656, @mogren)
* Improvement - [Add support for m5n/m5dn/r5n instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/657) (#657, @Jeffwan)
* Improvement - [Update cni-metrics-helper to v1.5.5](https://github.com/aws/amazon-vpc-cni-k8s/pull/672) (#672, @mogren)
* Improvement - [Reduce image layers and strip debug flags](https://github.com/aws/amazon-vpc-cni-k8s/pull/699) (#699, @mogren)

## v1.5.4

* Improvement - [Add support for g4dn instance family](https://github.com/aws/amazon-vpc-cni-k8s/pull/621) (#621, @mogren)
* Improvement - [Set cniVersion in the config to 0.3.1 (required for Kubernetes 1.16)](https://github.com/aws/amazon-vpc-cni-k8s/pull/605) (#605, @mogren)
* Bug fix - [Return delete success for pods that never got scheduled](https://github.com/aws/amazon-vpc-cni-k8s/commit/b0b2fc1be3cdb5cdde9ff4b13094488bf2c39d28) (#623, @mogren)

## v1.5.3

* Bug fix - [Copy the binary and config after ipamd is ready](https://github.com/aws/amazon-vpc-cni-k8s/pull/576) (#576, @mogren)
* Improvement - [Update Calico version to v3.8.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/554) (#554, @lmm)
* Improvement - [Add env var to override introspection bind address](https://github.com/aws/amazon-vpc-cni-k8s/pull/501) (#501, @jacksontj)
* Improvement - [Remove unused env variable](https://github.com/aws/amazon-vpc-cni-k8s/pull/578) (#578, @mogren)
* Improvement - [Exit early if MAC address doesn't match](https://github.com/aws/amazon-vpc-cni-k8s/pull/582) (#582, @mogren)

## v1.5.2
* Bug fix - [Fix formatting flag](https://github.com/aws/amazon-vpc-cni-k8s/pull/521) (#521, @uthark)
* Bug fix - [Fix formatting issue](https://github.com/aws/amazon-vpc-cni-k8s/pull/524) (#524, @uthark)
* Bug fix - [Detach ENI before deleting](https://github.com/aws/amazon-vpc-cni-k8s/pull/538) (#538, @uthark)
* Improvement - [Adding healthz endpoint to IPamD](https://github.com/aws/amazon-vpc-cni-k8s/pull/548) (#548, @nithu0115)
* Improvement - [Adding new m5 and r5 instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/518) (#518, @mogren)
* Improvement - [t3a.small only have 2 ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/543) (#543, @mogren)
* Improvement - [Updating AWS Go SDK version](https://github.com/aws/amazon-vpc-cni-k8s/pull/549) (#549, Nordlund, Eric)
* Improvement - [Reduce the wait time when checking for pods without IPs](https://github.com/aws/amazon-vpc-cni-k8s/pull/552) (#552, @mogren)
* Improvement - [Update start script to wait for ipamd health](https://github.com/aws/amazon-vpc-cni-k8s/pull/472) (#552, @mogren)
* Improvement - [Hide health check output](https://github.com/aws/amazon-vpc-cni-k8s/pull/569) (#569, @mogren)
* Improvement - [Support c5.12xlarge and c5.24xlarge](https://github.com/aws/amazon-vpc-cni-k8s/pull/510) (#510, @mogren)

## v1.5.1

* Bug fix - [Ignore namespace for custom eniconfig watch](https://github.com/aws/amazon-vpc-cni-k8s/pull/561) (#561, @mogren)

## v1.5.0

* Bug fix - [Fix spelling on annotation](https://github.com/aws/amazon-vpc-cni-k8s/pull/482) (#482, @forsberg)
* Bug fix - [Avoid using force detach of ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/458) (#458, @mogren)
* Bug fix - [Flush logs before exiting](https://github.com/aws/amazon-vpc-cni-k8s/pull/451) (#451, @venkatesh-eb)
* Improvement - [Add IPs to existing ENIs first](https://github.com/aws/amazon-vpc-cni-k8s/pull/487) (#487, @mogren)
* Improvement - [Added error handling for GetENIipLimit](https://github.com/aws/amazon-vpc-cni-k8s/pull/484) (#484, @Zyqsempai)
* Improvement - [Moved all GetEnv's calls to init step](https://github.com/aws/amazon-vpc-cni-k8s/pull/445) (#445, @Zyqsempai)
* Improvement - [On start up, wait for pods with no IP](https://github.com/aws/amazon-vpc-cni-k8s/pull/480) (#480, @mogren)
* Improvement - [Don't modify maxENI](https://github.com/aws/amazon-vpc-cni-k8s/pull/472) (#472, @nckturner)
* Improvement - [Improve WARM_IP_TARGET handling](https://github.com/aws/amazon-vpc-cni-k8s/pull/461) (#461, @nckturner)
* Improvement - [Update logging format to align messages](https://github.com/aws/amazon-vpc-cni-k8s/pull/473) (#473, @mogren)
* Improvement - [Added -W (wait for xlock's) flag to iptables commands](https://github.com/aws/amazon-vpc-cni-k8s/pull/439) (#439, @Zyqsempai)
* Improvement - [Remove error message from Prometheus labels](https://github.com/aws/amazon-vpc-cni-k8s/pull/467) (#467, @bboreham)
* Improvement - [Update instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/459) (#459, @mogren)

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
