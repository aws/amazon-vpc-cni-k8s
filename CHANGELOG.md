# Changelog

## v1.15.3

* Bug - [Fully address CVE-2023-44487](https://github.com/aws/amazon-vpc-cni-k8s/pull/2626) (@jdn5126 )
* Improvement - [feat(chart): Made node agent optional](https://github.com/aws/amazon-vpc-cni-k8s/pull/2623) (@stevehipwell )
* Improvement - [Update Golang to 1.21.3](https://github.com/aws/amazon-vpc-cni-k8s/pull/2616) (@jdn5126 )
* Improvement - [Go module updates and Golang builder image update](https://github.com/aws/amazon-vpc-cni-k8s/pull/2615) (@jdn5126 )

## v1.15.1

* Bug - [Do not patch CNINode for custom networking unless SGPP is enabled](https://github.com/aws/amazon-vpc-cni-k8s/pull/2591) (@jdn5126 )
* Bug - [Pass CNINode scheme to k8s client only](https://github.com/aws/amazon-vpc-cni-k8s/pull/2570) (@jdn5126 )
* Bug - [fix(chart): Switch base64 encoded cniConfig.fileContents to the binaryData](https://github.com/aws/amazon-vpc-cni-k8s/pull/2552) (@VLZZZ )
* Cleanup - [chore: remove refs to deprecated io/ioutil](https://github.com/aws/amazon-vpc-cni-k8s/pull/2541) (@testwill )
* Documentation - [Update example table 'Pod per Prefixes' value](https://github.com/aws/amazon-vpc-cni-k8s/pull/2573) (@rlaisqls )
* Documentation - [Bandwidth plugin with NP is currently unsupported](https://github.com/aws/amazon-vpc-cni-k8s/pull/2572) (@jayanthvn )
* Documentation - [Update the use of privileged flag in aws-vpc-cni manifest](https://github.com/aws/amazon-vpc-cni-k8s/pull/2555) (@jaydeokar )
* Improvement - [Dependabot Updates](https://github.com/aws/amazon-vpc-cni-k8s/pull/2605) (@jdn5126 )
* Improvement - [Update Golang Builder image](https://github.com/aws/amazon-vpc-cni-k8s/pull/2586) (@jdn5126 )
* Improvement - [Add ENABLE_V4_EGRESS env var to control IPv4 egress in IPv6 clusters](https://github.com/aws/amazon-vpc-cni-k8s/pull/2577) (@jdn5126 )
* Improvement - [Reduce API calls](https://github.com/aws/amazon-vpc-cni-k8s/pull/2575) (@jchen6585 )
* Improvement - [Add cni version to userAgent](https://github.com/aws/amazon-vpc-cni-k8s/pull/2566) (@jchen6585 )
* Improvement - [bump controller runtime to 0.16.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/2548) (@jchen6585 )
* Improvement - [Instance limits api pkg](https://github.com/aws/amazon-vpc-cni-k8s/pull/2528) (@jchen6585 )
* Improvement - [Mimic VPC-RC limit struture](https://github.com/aws/amazon-vpc-cni-k8s/pull/2516) (@jchen6585 )
* Metrics - [rename warm pool metrics](https://github.com/aws/amazon-vpc-cni-k8s/pull/2569) (@lnhanks )
* Metrics - [Only metrics](https://github.com/aws/amazon-vpc-cni-k8s/pull/2557) (@lnhanks )
* Testing - [Remove self-managed node group from custom-networking suite](https://github.com/aws/amazon-vpc-cni-k8s/pull/2590) (@jdn5126 )
* Testing - [Integration test cleanup: Security Groups for Pods](https://github.com/aws/amazon-vpc-cni-k8s/pull/2547) (@jdn5126 )

## v1.15.0

* Feature - [Add support for VPC Resource Controller's CNINode (reintroduce #2442)](https://github.com/aws/amazon-vpc-cni-k8s/pull/2503) (@haouc )
* Feature - [Add DISABLE_CONTAINER_V6 to disable IPv6 networking in container network namespaces](https://github.com/aws/amazon-vpc-cni-k8s/pull/2499) (@jdn5126 )
* Feature - [IP_COOLDOWN_PERIOD environment variable for ip cooldown period configuration](https://github.com/aws/amazon-vpc-cni-k8s/pull/2492) (@jchen6585 )
* Improvement - [Fix test kubeconfig, upgrade helm](https://github.com/aws/amazon-vpc-cni-k8s/pull/2509) (@jdn5126 )
* Improvement - [Update instance limits for upcoming vpc-cni release](https://github.com/aws/amazon-vpc-cni-k8s/pull/2506) (@jchen6585 )
* Improvement - [Upgrade controller-runtime to v0.15.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/2481) (@jdn5126 )

## v1.14.1

* Improvement - [Update aws-eks-nodeagent image version to v1.0.2](https://github.com/aws/aws-network-policy-agent/pull/51) (@jayanthvn)

## v1.14.0

* Feature - `v1.14.0` introduces Kubernetes Network Policy support. This is accomplished via the `aws-eks-nodeagent` container, which is now present in the `aws-node` pod.

## v1.13.4

* Bug - [RefreshSecurityGroups must be called after unmanaged ENIs are set](https://github.com/aws/amazon-vpc-cni-k8s/pull/2475) (@jdn5126 )
* Bug - [Fix event recorder initialization and add check to log](https://github.com/aws/amazon-vpc-cni-k8s/pull/2467) (@jdn5126 )

## v1.13.3

* Bug - [Decrease memory usage by K8S Clients](https://github.com/aws/amazon-vpc-cni-k8s/pull/2463) (@jdn5126 )
* Documentation - [update docs and CNI logging](https://github.com/aws/amazon-vpc-cni-k8s/pull/2433) (@jdn5126 )
* Improvement - [Updates instance limits including c7gn](https://github.com/aws/amazon-vpc-cni-k8s/pull/2438) (@mmerkes )

## v1.13.2

* Bug - [Sync node security groups to cache before node initialization](https://github.com/aws/amazon-vpc-cni-k8s/pull/2427) (@jdn5126 )
* Improvement - [Fix hard-coded nitro instance types: p4de.24xlarge and c7g.metal](https://github.com/aws/amazon-vpc-cni-k8s/pull/2428) (@jdn5126 )
* Improvement - [Upgrade to Go 1.20 and apply dependabot updates](https://github.com/aws/amazon-vpc-cni-k8s/pull/2412)
* Improvement - [Set iptables mode automatically and deprecate ENABLE_NFTABLES](https://github.com/aws/amazon-vpc-cni-k8s/pull/2402) (@jdn5126 )
* Improvement - [Upgrade client-go and controller-runtime modules](https://github.com/aws/amazon-vpc-cni-k8s/pull/2396) (@jdn5126 )

## v1.13.0

* Bug - [Increase datastore pool at startup](https://github.com/aws/amazon-vpc-cni-k8s/pull/2354) (@jdn5126 )
* Bug - [Deallocate IP address according to warm IP target when multiple enis are present](https://github.com/aws/amazon-vpc-cni-k8s/pull/2368) (@bikashmishra100 )
* Bug - [Return success from CNI DEL when IPAMD is unreachable](https://github.com/aws/amazon-vpc-cni-k8s/pull/2350) (@jdn5126 )
* Bug - [Fix for aws-vpc-cni chart with tolerations to produce syntax valid yaml](https://github.com/aws/amazon-vpc-cni-k8s/pull/2345) (@Bourne-ID )
* Bug - [adding ip check for annotatePod in ipamd](https://github.com/aws/amazon-vpc-cni-k8s/pull/2328) (@jerryhe1999 )
* Feature - [Introduce DISABLE_LEAKED_ENI_CLEANUP to disable leaked ENI cleanup task](https://github.com/aws/amazon-vpc-cni-k8s/pull/2370) (@jdn5126 )
* Feature - [Add IPv6 egress support to eks IPv4 cluster](https://github.com/aws/amazon-vpc-cni-k8s/pull/2361) (@wanyufe )
* Feature - [feat(chart): Refactored image template logic for endpoint flexibility](https://github.com/aws/amazon-vpc-cni-k8s/pull/2335) (@stevehipwell )
* Feature - [add AWS_EC2_ENDPOINT variable for custom endpoint](https://github.com/aws/amazon-vpc-cni-k8s/pull/2326) (@jihunseol )
* Improvement - [Refactor egress-v4-cni plugin to support unit testing](https://github.com/aws/amazon-vpc-cni-k8s/pull/2353) (@wanyufe )
* Improvement - [Update instance limits and core plugins version in preparation for upcoming VPC CNI release](https://github.com/aws/amazon-vpc-cni-k8s/pull/2390) (@jdn5126 )
* Improvement - [refactoring eniconfig func to only take node as parameter](https://github.com/aws/amazon-vpc-cni-k8s/pull/2387) (@haouc )
* Improvement - [Remove go mod download from Dockerfiles](https://github.com/aws/amazon-vpc-cni-k8s/pull/2383) (@jdn5126 )
* Improvement - [Add apiVersion to MY_NODE_NAME](https://github.com/aws/amazon-vpc-cni-k8s/pull/2372) (@jdn5126 )
* Improvement - [install all core CNI plugins via init container](https://github.com/aws/amazon-vpc-cni-k8s/pull/2355) (@jdn5126 )
* Improvement - [Make all the aws vpc cni environmental variables case insensitive](https://github.com/aws/amazon-vpc-cni-k8s/pull/2334) (@jerryhe1999 )
* Improvement - [resource limit on init container in eks addon](https://github.com/aws/amazon-vpc-cni-k8s/issues/2191 ) (@pdeva )
* Testing - [Add integration test for POD v4/v6 egress traffic](https://github.com/aws/amazon-vpc-cni-k8s/pull/2371) (@wanyufe )

## v1.12.6

* Bug - [Fix MTU parameter in egress-v4-cni plugin](https://github.com/aws/amazon-vpc-cni-k8s/pull/2295) (@jdn5126 )
* Documentation - [Fixing the log message to be meaningful](https://github.com/aws/amazon-vpc-cni-k8s/pull/2260) (@rajeeshckr )
* Improvement - [Add bmn-sf1.metal instance support](https://github.com/aws/amazon-vpc-cni-k8s/pull/2286) (@vpineda1996 )
* Improvement - [Support routing to external IPs behind service](https://github.com/aws/amazon-vpc-cni-k8s/pull/2243) (@jdn5126 )
* Improvement - [Use Go 1.19; fix egress-v4-cni MTU parsing, update containerd](https://github.com/aws/amazon-vpc-cni-k8s/pull/2303) (@jdn5126 )
* Improvement - [Added enviroment variable to allow ipamd to manage the ENIs on a non schedulable node](https://github.com/aws/amazon-vpc-cni-k8s/pull/2296) (@rajeeshckr )
* Improvement - [Use GET for IAM Permissions event; update controller-runtime from 0.13.1 to 0.14.4 and client-go from v0.25.5 to v0.26.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/2304) (@jdn5126 )
* Improvement - [Remove old checkpoint migration logic; update containerd version](https://github.com/aws/amazon-vpc-cni-k8s/pull/2307) (@jdn5126 )

## v1.12.5

* Bug - [Handle private IP exceeded error](https://github.com/aws/amazon-vpc-cni-k8s/pull/2210) (@jayanthvn )
* Documentation - [doc: document AWS_VPC_K8S_CNI_LOGLEVEL for cni-metric-helper helm chart](https://github.com/aws/amazon-vpc-cni-k8s/pull/2226) (@csantanapr )
* Documentation - [Added cni-metrics-helper docs](https://github.com/aws/amazon-vpc-cni-k8s/pull/2187) (@0xquark )
* Improvement - [Update golang builder image](https://github.com/aws/amazon-vpc-cni-k8s/pull/2255) (@jdn5126 )
* Improvement - [Update golang builder image](https://github.com/aws/amazon-vpc-cni-k8s/pull/2271) (@jdn5126 )
* Improvement - [run make generate-limits](https://github.com/aws/amazon-vpc-cni-k8s/pull/2235) (@jdn5126 )
* Improvement - [Add M7g, R7g instance](https://github.com/aws/amazon-vpc-cni-k8s/pull/2250) (@Issacwww )
* Improvement - [Update client-go and k8s packages](https://github.com/aws/amazon-vpc-cni-k8s/pull/2204) (@jaydeokar )
* Improvement - [Refactor cni-metrics-helper chart for eks charts release](https://github.com/aws/amazon-vpc-cni-k8s/pull/2201) (@jdn5126 )
* Improvement - [fix: Upgrade to golang.org/x/net@v0.7.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/2273) (@ellistarn )

## v1.12.2

* Bug - [Cherry-pick prometheus/client_golang module update to address CVE](https://github.com/aws/amazon-vpc-cni-k8s/pull/2239) (@jdn5126 )
* Improvement - [Minimal base image for cni-metrics-helper minimal base image](https://github.com/aws/amazon-vpc-cni-k8s/pull/2189) (@jdn5126 )

## v1.12.1

* Bug - [Cleanup pod networking resources when IPAMD is unreachable to prevent rule leaking.](https://github.com/aws/amazon-vpc-cni-k8s/pull/2145) (@jdn5126 )
* Bug - [Skip add-on installation when an add-on information is not available](https://github.com/aws/amazon-vpc-cni-k8s/pull/2131) (@sushrk )
* Bug - [Add missing rules when NodePort support is disabled](https://github.com/aws/amazon-vpc-cni-k8s/pull/2026)(@antoninbas )
* Bug - [Fix logging in publisher package](https://github.com/aws/amazon-vpc-cni-k8s/pull/2119) (@jdn5126 )
* Bug - [Fix Crypto package vulnerability](https://github.com/aws/amazon-vpc-cni-k8s/pull/2183) (@jaydeokar )
* Bug - [Fix Crypto package vulnerability](https://github.com/aws/amazon-vpc-cni-k8s/pull/2174) (@jaydeokar )
* Cleanup - [Merging makefile and go.mod from test directory to root directory](https://github.com/aws/amazon-vpc-cni-k8s/pull/2129) (@jerryhe1999 )
* Documentation - [Update troubleshooting docs for node operating system](https://github.com/aws/amazon-vpc-cni-k8s/pull/2132) (@jdn5126 )
* Feature - [ Reporting EC2 API calls metrics through CNI metrics helper](https://github.com/aws/amazon-vpc-cni-k8s/pull/2142) (@jaydeokar )
* Feature - [Added resources block to cni-metrics-helper helm chart](https://github.com/aws/amazon-vpc-cni-k8s/pull/2141) (@jcogilvie )
* Feature - [CLUSTER_ENDPOINT can now be specified to allow the VPC CNI to initialize before kube-proxy has finished setting up cluster IP routes ](https://github.com/aws/amazon-vpc-cni-k8s/pull/2138) (@bwagner5 )
* Improvement - [Move VPC CNI and VPC CNI init images to use EKS minimal base image.](https://github.com/aws/amazon-vpc-cni-k8s/pull/2146) (@jdn5126 )
* Improvement - [Updating helm chart as per helm v3 standard](https://github.com/aws/amazon-vpc-cni-k8s/pull/2144) (@jaydeokar )
* Improvement - [Update golang to 1.19.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/2147) (@jayanthvn )
* Testing - [Fixes to automation runs](https://github.com/aws/amazon-vpc-cni-k8s/pull/2148) (@jdn5126 )
* Testing - [Fix environment variable name in update-cni-image script](https://github.com/aws/amazon-vpc-cni-k8s/pull/2136) (@sushrk )

## v1.12.0

* Bug - [Remove extra decrement of totalIP count](https://github.com/aws/amazon-vpc-cni-k8s/pull/2042/files) (@jayanthvn )
* Documentation - [Update readme with slack channel ](https://github.com/aws/amazon-vpc-cni-k8s/pull/2111) (@jayanthvn )
* Documentation - [Fix ENIConfig keys in values.yaml](https://github.com/aws/amazon-vpc-cni-k8s/pull/1989) (@chotiwat )
* Improvement - [switch to use state file for IP allocation pool management](https://github.com/aws/amazon-vpc-cni-k8s/pull/2110) (@M00nF1sh )
* Improvement - [explicitly request NET_RAW capabilities in CNI manifests ](https://github.com/aws/amazon-vpc-cni-k8s/pull/2063) (@JingmingGuo )
* Improvement - [Reduce startup latency by removing some unneeded sleeps](https://github.com/aws/amazon-vpc-cni-k8s/pull/2104) (@bwagner5 )
* New Instance Support - [Add trn1 limits](https://github.com/aws/amazon-vpc-cni-k8s/pull/2092) (@cartermckinnon )
* Testing - [fix metrics-helper test to detach role policy early](https://github.com/aws/amazon-vpc-cni-k8s/pull/2121) (@sushrk )
* Testing - [Use GetNodes in metrics-helper; explicitly install latest addon](https://github.com/aws/amazon-vpc-cni-k8s/pull/2093/files) (@jdn5126 )
* Testing - [refine all github workflows](https://github.com/aws/amazon-vpc-cni-k8s/pull/2090) (@M00nF1sh )
* Testing - [Resolve flakiness in IPAMD warm target tests](https://github.com/aws/amazon-vpc-cni-k8s/pull/2112) (@jdn5126 )
* Testing - [VPC CNI Integration Test Fixes](https://github.com/aws/amazon-vpc-cni-k8s/pull/2105)  (@jdn5126 )
* Testing - [Update CNI canary integration test and cleanup for ginkgo v2](https://github.com/aws/amazon-vpc-cni-k8s/pull/2088) (@jdn5126 )

## v1.11.5

* Bug - [Handle pod deletion when PrevResult has VLAN 0](https://github.com/aws/amazon-vpc-cni-k8s/pull/2323) (@jdn5126 )

## v1.11.4

* Improvement - [update aws-node clusterrole permissions](https://github.com/aws/amazon-vpc-cni-k8s/pull/2058) (@sushrk)
* Improvement - [IPAMD optimizations and makefile changes](https://github.com/aws/amazon-vpc-cni-k8s/pull/1975) (@jayanthvn)
* Documentation - [Fix minor typo on documentation](https://github.com/aws/amazon-vpc-cni-k8s/pull/2059) (@guikcd)
* Documentation - [Fixing prefixes per ENI value in example](https://github.com/aws/amazon-vpc-cni-k8s/pull/2060) (@mkarakas)
* New release - [multus manifest for release v3.9.0-eksbuild.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/2057) (@sushrk)
* Bug - [Setting AWS_VPC_K8S_CNI_RANDOMIZESNAT to the default value](https://github.com/aws/amazon-vpc-cni-k8s/pull/2028) (@vgunapati)
* New instance support - [Updated new instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/2062) (@jayanthvn)

## v1.11.3

* Improvement -  [Increase cpu requests limit](https://github.com/aws/amazon-vpc-cni-k8s/pull/2040) (@vikasmb)
* Bugfix      -  [Re-use logger instance](https://github.com/aws/amazon-vpc-cni-k8s/pull/2031) (@vikasmb)
* Improvement -  [Add event recorder utils to raise aws-node pod events](https://github.com/aws/amazon-vpc-cni-k8s/pull/1536) (@sushrk)
* Improvement -  [chart: Add extraVolumes and extraVolumeMounts](https://github.com/aws/amazon-vpc-cni-k8s/pull/1949) (@jkroepke)
* Bugfix -       [Fix cni panic due to pod.Annotations is a nil map](https://github.com/aws/amazon-vpc-cni-k8s/pull/1947) (@Downager)

## v1.11.2

* Improvement -  [Updated golang to Go 1.18](https://github.com/aws/amazon-vpc-cni-k8s/pull/1991) (@orsenthil)
* Improvement -  [Updated containernetworking/cni version to 0.8.1 to address CVE-2021-20206](https://github.com/aws/amazon-vpc-cni-k8s/pull/1996) (@orsenthil)
* Improvement -  [Updated CNI Plugins to v1.1.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/1997) (@orsenthil)

# v1.11.1

Was Skipped

## v1.11.0

* Feature - [Support new SGPP standard mode](https://github.com/aws/amazon-vpc-cni-k8s/pull/1907) (@M00nF1sh )
* Feature - [IPv4 Randomize SNAT support for IPv6 pods](https://github.com/aws/amazon-vpc-cni-k8s/pull/1903) (@achevuru)
* Feature - [Respect existing ENIConfig label if set on node](https://github.com/aws/amazon-vpc-cni-k8s/pull/1596) (@backjo)
* Improvement - [Timeout and reconcile when checking API server connectivity](https://github.com/aws/amazon-vpc-cni-k8s/pull/1943)
(@prateekgogia)
* Improvement - [Improve startup performance of IPAMD](https://github.com/aws/amazon-vpc-cni-k8s/pull/1855) (@backjo)
* Improvement - [Record pod metadata and allocationTime in IP allocation state file](https://github.com/aws/amazon-vpc-cni-k8s/pull/1958) (@M00nF1sh )
* Bug - [Fixes node label error handling](https://github.com/aws/amazon-vpc-cni-k8s/pull/1892) & [revert to use update for node label update](https://github.com/aws/amazon-vpc-cni-k8s/pull/1959) (@jayanthvn, @M00nF1sh )
       (#1959)
* Bug - [IPAMD throw an error on configuration validation failure](https://github.com/aws/amazon-vpc-cni-k8s/pull/1698) (@veshij)
* Cleanup - [refactoring DataStore.GetStats to simplify adding new fields](https://github.com/aws/amazon-vpc-cni-k8s/pull/1704) (@veshij)

## v1.10.3

* Improvement - [Upgrade AWS SDK GO](https://github.com/aws/amazon-vpc-cni-k8s/pull/1944) (@jayanthvn)
* Improvement - [C7g instances support](https://github.com/aws/amazon-vpc-cni-k8s/pull/1940) (@jayanthvn)
* Improvement - [Enable Prefix Delegation on Bare metal instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/1937) (@achevuru)
* Bugfix - [Fix dependabot high sev issue caused by GoGo protobuf](https://github.com/aws/amazon-vpc-cni-k8s/pull/1942) (@jayanthvn)
* Bugfix - [Fixed empty netns bug](https://github.com/aws/amazon-vpc-cni-k8s/pull/1941 ) (@cgchinmay)

## v1.10.2
* Improvement - [Fetch Region and CLUSTER_ID information from cni-metrics-helper env](https://github.com/aws/amazon-vpc-cni-k8s/pull/1715) (@cgchinmay )
* Improvement - [Add VlanId in the cmdAdd Result struct](https://github.com/aws/amazon-vpc-cni-k8s/pull/1705) (@cgchinmay )
* Improvement - [Update Insufficient IP address logic in ipamd](https://github.com/aws/amazon-vpc-cni-k8s/pull/1773) (@cgchinmay )
* Improvement - [go version updated to 1.17](https://github.com/aws/amazon-vpc-cni-k8s/pull/1832) (@cgchinmay )
* Improvement - [use public ecr for AL2](https://github.com/aws/amazon-vpc-cni-k8s/pull/1804) (@vikasmb )
* Improvement - [remove set -x from bash, add -Ss to curl](https://github.com/aws/amazon-vpc-cni-k8s/pull/1802) (@skpy )
* Bug - [Fix condition for disable provisioning](https://github.com/aws/amazon-vpc-cni-k8s/pull/1823) (@jayanthvn )

## v1.10.1
* Bug - [Use IMDSv2 token when fetching node ip in entrypoint](https://github.com/aws/amazon-vpc-cni-k8s/pull/1727) (#1727, [@chlunde](https://github.com/chlunde)) 

## v1.10.0
* Feature - [IPv6 Support](https://github.com/aws/amazon-vpc-cni-k8s/pull/1587) (#1587, [@achevuru](https://github.com/achevuru))
* Enhancement - [Handle delays tied to V6 interfaces](https://github.com/aws/amazon-vpc-cni-k8s/pull/1631) (#1631, [@achevuru](https://github.com/achevuru))
* Enhancement - [Support for Bandwidth Plugin](https://github.com/aws/amazon-vpc-cni-k8s/pull/1560) (#1560, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Knob to enable bandwidth plugin](https://github.com/aws/amazon-vpc-cni-k8s/pull/1580) (#1580, [@jayanthvn](https://github.com/jayanthvn))
* Testing - [IPv6 Integration test suite](https://github.com/aws/amazon-vpc-cni-k8s/pull/1658) (#1658, [@achevuru](https://github.com/achevuru))

## v1.9.3
* Improvement - [Update golang](https://github.com/aws/amazon-vpc-cni-k8s/pull/1665) (#1665, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [Pod startup latency with Calico and EKS](https://github.com/aws/amazon-vpc-cni-k8s/pull/1629) (#1629, [@jayanthvn](https://github.com/jayanthvn))
* Bug - [Make error count granular](https://github.com/aws/amazon-vpc-cni-k8s/pull/1651) (#1651, [@jayanthvn](https://github.com/jayanthvn))
* Bug - [ServiceAccount should precede DaemonSet in yaml aws](https://github.com/aws/amazon-vpc-cni-k8s/pull/1637) (#1637, [@sramabad1](https://github.com/sramabad1))
* Testing - [Enable unit tests upon PR to release branch](https://github.com/aws/amazon-vpc-cni-k8s/pull/1684) (#1684, [@vikasmb](https://github.com/vikasmb))
* Testing - [Upgrade EKS cluster version](https://github.com/aws/amazon-vpc-cni-k8s/pull/1680) (#1680, [@vikasmb](https://github.com/vikasmb)) 

## v1.9.1
* Enhancement - [Support DISABLE_NETWORK_RESOURCE_PROVISIONING](https://github.com/aws/amazon-vpc-cni-k8s/pull/1586) (#1586, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Allow reconciler retry for InsufficientCIDR EC2 error](https://github.com/aws/amazon-vpc-cni-k8s/pull/1585) (#1585, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Support for setting no_manage=false](https://github.com/aws/amazon-vpc-cni-k8s/pull/1607) (#1607, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Support for m6i instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/1601) (#1601, [@causton81](https://github.com/causton81))
* Bug - [Fallback for get hypervisor type and eni ipv4 limits](https://github.com/aws/amazon-vpc-cni-k8s/pull/1616) (#1616, [@jayanthvn](https://github.com/jayanthvn))
* Bug - [fix typo and regenerate limits file ](https://github.com/aws/amazon-vpc-cni-k8s/pull/1597) (#1597, [@jayanthvn](https://github.com/jayanthvn))
* Testing - [UTs for no_manage=false](https://github.com/aws/amazon-vpc-cni-k8s/pull/1612) (#1612, [@jayanthvn](https://github.com/jayanthvn))
* Testing - [Run integration test on release branch](https://github.com/aws/amazon-vpc-cni-k8s/pull/1615) (#1615, [@vikasmb](https://github.com/vikasmb))

## v1.9.0
* Enhancement - [EC2 sdk model override](https://github.com/aws/amazon-vpc-cni-k8s/pull/1508) (#1508, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Prefix Delegation feature support](https://github.com/aws/amazon-vpc-cni-k8s/pull/1516) (#1516, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Header formatting for env variable](https://github.com/aws/amazon-vpc-cni-k8s/pull/1522) (#1522, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [non-nitro instances init issues](https://github.com/aws/amazon-vpc-cni-k8s/pull/1527) (#1527, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Add metrics for total prefix count and ips used per cidr](https://github.com/aws/amazon-vpc-cni-k8s/pull/1530) (#1530, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Update documentation for PD](https://github.com/aws/amazon-vpc-cni-k8s/pull/1540) (#1540, [@jayanthvn](https://github.com/jayanthvn))
* Enhancement - [Update SDK Go version](https://github.com/aws/amazon-vpc-cni-k8s/pull/1544) (#1544, [@jayanthvn](https://github.com/jayanthvn))

## v1.8.0
* Bug - [Use symmetric return path for non-VPC traffic - alternate solution](https://github.com/aws/amazon-vpc-cni-k8s/pull/1475) (#1475, [@kishorj](https://github.com/kishorj))
* Bug - [Gracefully handle failed ENI SG update](https://github.com/aws/amazon-vpc-cni-k8s/pull/1341) (#1341, [@jayanthvn](https://github.com/jayanthvn))
* Bug - [Fix CNI crashing when there is no available IP addresses](https://github.com/aws/amazon-vpc-cni-k8s/pull/1499) (#1499, [@M00nF1sh](https://github.com/M00nF1sh))
* Bug - [Use primary ENI SGs if SG is null for Custom networking](https://github.com/aws/amazon-vpc-cni-k8s/pull/1259) (#1259, [@jayanthvn](https://github.com/jayanthvn))
* Bug - [Don't cache dynamic VPC IPv4 CIDR info](https://github.com/aws/amazon-vpc-cni-k8s/pull/1113) (#1113, [@anguslees](https://github.com/anguslees))
* Improvement - [Address Excessive API Server calls from CNI Pods](https://github.com/aws/amazon-vpc-cni-k8s/pull/1419) (#1419, [@achevuru](https://github.com/achevuru))
* Improvement - [refine ENI tagging logic](https://github.com/aws/amazon-vpc-cni-k8s/pull/1482) (#1482, [@M00nF1sh](https://github.com/M00nF1sh))
* Improvement - [Change tryAssignIPs to assign up to configured WARM_IP_TARGET](https://github.com/aws/amazon-vpc-cni-k8s/pull/1279) (#1279, [@jacksontj](https://github.com/jacksontj))
* Improvement - [Use regional STS endpoint](https://github.com/aws/amazon-vpc-cni-k8s/pull/1332) (#1332, [@nithu0115](https://github.com/nithu0115))
* Improvement - [Update containernetworking dependencies](https://github.com/aws/amazon-vpc-cni-k8s/pull/1200) (#1200, [@mogren](https://github.com/mogren))
* Improvement - [Split Calico manifest into two](https://github.com/aws/amazon-vpc-cni-k8s/pull/1410) (#1410, [@caseydavenport](https://github.com/caseydavenport))
* Improvement - [Update Calico manifest to support ARM & AMD](https://github.com/aws/amazon-vpc-cni-k8s/pull/1282) (#1282, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [Auto gen of AWS CNI, metrics helper and calico artifacts through helm](https://github.com/aws/amazon-vpc-cni-k8s/pull/1271) (#1271, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [Refactor EC2 Metadata IMDS code](https://github.com/aws/amazon-vpc-cni-k8s/pull/1225) (#1225, [@anguslees](https://github.com/anguslees))
* Improvement - [Unnecessary logging for each CNI invocation](https://github.com/aws/amazon-vpc-cni-k8s/pull/1469) (#1469, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [New instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/1463) (#1463, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [Use 'exec' ENTRYPOINTs](https://github.com/aws/amazon-vpc-cni-k8s/pull/1432) (#1432, [@anguslees](https://github.com/anguslees))
* Improvement - [Fix logging texts for ENI cleanup](https://github.com/aws/amazon-vpc-cni-k8s/pull/1209) (#1209, [@mogren](https://github.com/mogren))
* Improvement - [Remove Duplicated vlan IPTable rules](https://github.com/aws/amazon-vpc-cni-k8s/pull/1208) (#1208, [@mogren](https://github.com/mogren))
* Improvement - [Minor code cleanup](https://github.com/aws/amazon-vpc-cni-k8s/pull/1198) (#1198, [@mogren](https://github.com/mogren))
* HelmChart - [Adding flags to support overriding container runtime endpoint.](https://github.com/aws/amazon-vpc-cni-k8s/pull/1443) (#1443, [@haouc](https://github.com/haouc))
* HelmChart - [Add podLabels to amazon-vpc-cni chart](https://github.com/aws/amazon-vpc-cni-k8s/pull/1440) (#1440, [@haouc](https://github.com/haouc))
* HelmChart - [Add workflow to sync aws-vpc-cni helm chart to eks-charts](https://github.com/aws/amazon-vpc-cni-k8s/pull/1430) (#1430, [@fawadkhaliq](https://github.com/fawadkhaliq))
* Testing - [Remove validation of VPC CIDRs from ip rules](https://github.com/aws/amazon-vpc-cni-k8s/pull/1476) (#1476, [@kishorj](https://github.com/kishorj))
* Testing - [Updated agent version](https://github.com/aws/amazon-vpc-cni-k8s/pull/1474) (#1474, [@cgchinmay](https://github.com/cgchinmay))
* Testing - [Fix for CI failure](https://github.com/aws/amazon-vpc-cni-k8s/pull/1470) (#1470, [@achevuru](https://github.com/achevuru))
* Testing - [Binary for mtu and veth prefix check](https://github.com/aws/amazon-vpc-cni-k8s/pull/1458) (#1458, [@cgchinmay](https://github.com/cgchinmay))
* Testing - [add test to verify cni-metrics-helper puts metrics to CW](https://github.com/aws/amazon-vpc-cni-k8s/pull/1461) (#1461, [@abhipth](https://github.com/abhipth))
* Testing - [add e2e test for security group for pods](https://github.com/aws/amazon-vpc-cni-k8s/pull/1459) (#1459, [@abhipth](https://github.com/abhipth))
* Testing - [Added Test cases for EnvVars check on CNI daemonset](https://github.com/aws/amazon-vpc-cni-k8s/pull/1431) (#1431, [@cgchinmay](https://github.com/cgchinmay))
* Testing - [add test to verify host networking setup & cleanup](https://github.com/aws/amazon-vpc-cni-k8s/pull/1457) (#1457, [@abhipth](https://github.com/abhipth))
* Testing - [Runners failing because of docker permissions](https://github.com/aws/amazon-vpc-cni-k8s/pull/1456) (#1456, [@jayanthvn](https://github.com/jayanthvn))
* Testing - [decouple test helper input struct from netlink library](https://github.com/aws/amazon-vpc-cni-k8s/pull/1455) (#1455, [@abhipth](https://github.com/abhipth))
* Testing - [add custom networking e2e test suite](https://github.com/aws/amazon-vpc-cni-k8s/pull/1445) (#1445, [@abhipth](https://github.com/abhipth))
* Testing - [add integration test for ipamd env variables](https://github.com/aws/amazon-vpc-cni-k8s/pull/1453) (#1453, [@abhipth](https://github.com/abhipth))
* Testing - [add agent for testing pod networking](https://github.com/aws/amazon-vpc-cni-k8s/pull/1448) (#1448, [@abhipth](https://github.com/abhipth))
* Testing - [fix format of commited code to fix unit test step](https://github.com/aws/amazon-vpc-cni-k8s/pull/1449) (#1449, [@abhipth](https://github.com/abhipth))
* Testing - [Unblocks Github Action Integration Tests](https://github.com/aws/amazon-vpc-cni-k8s/pull/1435) (#1435, [@couralex6](https://github.com/couralex6))
* Testing - [add warm ENI/IP target integration tests](https://github.com/aws/amazon-vpc-cni-k8s/pull/1438) (#1438, [@abhipth](https://github.com/abhipth))
* Testing - [add service connectivity test](https://github.com/aws/amazon-vpc-cni-k8s/pull/1436) (#1436, [@abhipth](https://github.com/abhipth))
* Testing - [add network connectivity test](https://github.com/aws/amazon-vpc-cni-k8s/pull/1424) (#1424, [@abhipth](https://github.com/abhipth))
* Testing - [add ginkgo automation framework](https://github.com/aws/amazon-vpc-cni-k8s/pull/1416) (#1416, [@abhipth](https://github.com/abhipth))
* Testing - [Add some test coverage to allocating ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/1234) (#1234, [@mogren](https://github.com/mogren))
* Testing - [Add some minimal tests to metrics](https://github.com/aws/amazon-vpc-cni-k8s/pull/1228) (#1228, [@mogren](https://github.com/mogren))


## v1.7.10

* Improvement - Multi card support - Prevent route override for primary ENI across multi-cards ENAs (#1396 , [@jayanthvn](https://github.com/Jayanthvn))

## v1.7.9

* Improvement - [Adds http timeout to aws sessions](https://github.com/aws/amazon-vpc-cni-k8s/pull/1370) (#1370 , [@couralex6](https://github.com/couralex6))
* Improvement - [Switch calico to be deployed with the Tigera operator](https://github.com/aws/amazon-vpc-cni-k8s/pull/1297) (#1297 by [@tmjd](https://github.com/tmjd))
* Improvement - [Update calico to v3.17.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/1328) (#1328 , [@lwr20](https://github.com/lwr20))
* Improvement - [update plugins to v0.9.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/1362) (#1362 , [@fr0stbyte](https://github.com/fr0stbyte))
* Improvement - [update github.com/containernetworking/plugins to v0.9.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/1350) (#1350 , [@fr0stbyte](https://github.com/fr0stbyte))
* Bug - [Fix regex match for getting primary interface](https://github.com/aws/amazon-vpc-cni-k8s/pull/1311) (#1311 , [@jayanthvn](https://github.com/Jayanthvn))
* Bug - [Output to stderr when no log file path is passed](https://github.com/aws/amazon-vpc-cni-k8s/pull/1275) (#1275 , [@couralex6](https://github.com/couralex6))
* Bug - [Fix deletion of hostVeth rule for pods using security group](https://github.com/aws/amazon-vpc-cni-k8s/pull/1376) (#1376 , [@SaranBalaji90](https://github.com/SaranBalaji90))

## v1.7.8

* Improvement - [Replace DescribeNetworkInterfaces with paginated version](https://github.com/aws/amazon-vpc-cni-k8s/pull/1333) (#1333, [@haouc](https://github.com/haouc))

## v1.7.7

* Bug - [Rearrange Pod deletion workflow](https://github.com/aws/amazon-vpc-cni-k8s/pull/1315) (#1315, [@SaranBalaji90](https://github.com/SaranBalaji90))

## v1.7.6

* Improvement - [Avoid detaching EFA ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/1237) (#1237 , [@mogren](https://github.com/mogren))
* Improvement - [Add t4g instance type](https://github.com/aws/amazon-vpc-cni-k8s/pull/1219) (#1219 , [@mogren](https://github.com/mogren))
* Improvement - [Add p4d.24xlarge instance type](https://github.com/aws/amazon-vpc-cni-k8s/pull/1238) (#1238 , [@mogren](https://github.com/mogren))
* Improvement - [Update calico to v3.16.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/1235) (#1235 , [@lwr20](https://github.com/lwr20))
* Improvement - [Update readme on stdout support for plugin log file](https://github.com/aws/amazon-vpc-cni-k8s/pull/1251) (#1251 , [@jayanthvn](https://github.com/jayanthvn))
* Bug - [Make p3dn.24xlarge examples more realistic](https://github.com/aws/amazon-vpc-cni-k8s/pull/1263) (#1263 , [@mogren](https://github.com/mogren))
* Bug - [Make sure we have space for a trunk ENI](https://github.com/aws/amazon-vpc-cni-k8s/pull/1210) (#1210 , [@mogren](https://github.com/mogren))
* Bug - [Update README for DISABLE_TCP_EARLY_DEMUX](https://github.com/aws/amazon-vpc-cni-k8s/pull/1273) (#1273 , [@SaranBalaji90](https://github.com/SaranBalaji90))
* Bug - [Update p4 instance limits](https://github.com/aws/amazon-vpc-cni-k8s/pull/1289) (#1289 , [@jayanthvn](https://github.com/Jayanthvn))

## v1.7.5

* Bug - [Match primary ENI IP correctly](https://github.com/aws/amazon-vpc-cni-k8s/pull/1247) (#1247 , [@mogren](https://github.com/mogren))

## v1.7.4

* Bug - [Ignore error on enabling TCP early demux for old kernels](https://github.com/aws/amazon-vpc-cni-k8s/pull/1242) (#1242, [@mogren](https://github.com/mogren))

## v1.7.3

* Bug - [Add support to toggle TCP early demux](https://github.com/aws/amazon-vpc-cni-k8s/pull/1212) (#1212, [@SaranBalaji90](https://github.com/SaranBalaji90))

## v1.7.2

* Bug - [Avoid deleting ENIs being created by older CNI versions](https://github.com/aws/amazon-vpc-cni-k8s/pull/1109) (#1109, [@jayanthvn](https://github.com/jayanthvn))
* Bug - [Add iptables fix and update to v1.7.x](https://github.com/aws/amazon-vpc-cni-k8s/pull/1187) (#1187, [@mogren](https://github.com/mogren))
* Bug - [Handle stale IMDS metadata for secondary IPs](https://github.com/aws/amazon-vpc-cni-k8s/pull/1177) (#1177, [@mogren](https://github.com/mogren))
* Bug - [Mount /run/xtables.lock to prevent unwanted race conditions](https://github.com/aws/amazon-vpc-cni-k8s/pull/1186) (#1186, [@kgtw](https://github.com/kgtw))
* Bug - [Make a deep copy for introspection](https://github.com/aws/amazon-vpc-cni-k8s/pull/1179) (#1179, [@mogren](https://github.com/mogren))
* Bug - [Wait for ENI and secondary IPs](https://github.com/aws/amazon-vpc-cni-k8s/pull/1174) (#1174, [@mogren](https://github.com/mogren))
* Improvement - [Update Calico images to v3.15.1 & set routeSource=WorkloadIPs for v1.7](https://github.com/aws/amazon-vpc-cni-k8s/pull/1182) (#1182, [@realgaurav](https://github.com/realgaurav))
* Improvement - [Update Calico to v3.15.1 & set routeSource=WorkloadIPs](https://github.com/aws/amazon-vpc-cni-k8s/pull/1165) (#1165, [@realgaurav](https://github.com/realgaurav))
* Improvement - [Clean up go lint warnings](https://github.com/aws/amazon-vpc-cni-k8s/pull/1162) (#1162, [@mogren](https://github.com/mogren))
* Improvement - [Update SG on secondary ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/1098) (#1098, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [Fix device number and update table name the device index](https://github.com/aws/amazon-vpc-cni-k8s/pull/1071) (#1071, [@mogren](https://github.com/mogren))

## v1.7.1

* Bug - [Calico deletes routes when using CNI v1.7.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/1166) (#1166, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [enable manual override for VERSION in images](https://github.com/aws/amazon-vpc-cni-k8s/pull/1156) (#1156, [@nprab428](https://github.com/nprab428)) 

## v1.7.0

* Improvement - [Reject version skew between gRPC client and server](https://github.com/aws/amazon-vpc-cni-k8s/pull/1141) (#1141, [@anguslees](https://github.com/anguslees))
* Improvement - [Write to IPAM checkpoint file immediately after reading from CRI](https://github.com/aws/amazon-vpc-cni-k8s/pull/1140) (#1140, [@anguslees](https://github.com/anguslees))
* Improvement - [Fix a log message](https://github.com/aws/amazon-vpc-cni-k8s/pull/1138) (#1138, [@anguslees](https://github.com/anguslees))
* Improvement - [Add ipamd changes for sg support](https://github.com/aws/amazon-vpc-cni-k8s/pull/1126) (#1126, [@mogren](https://github.com/mogren))
* Improvement - [Add support to setup pod network using VLANss](https://github.com/aws/amazon-vpc-cni-k8s/pull/1125) (#1125, [@SaranBalaji90](https://github.com/SaranBalaji90))
* Improvement - [Improve CRI->checkpoint logic in the face of downgrades](https://github.com/aws/amazon-vpc-cni-k8s/pull/1123) (#1123, [@anguslees](https://github.com/anguslees))
* Improvement - [Slash and burn unused code](https://github.com/aws/amazon-vpc-cni-k8s/pull/1115) (#1115, [@anguslees](https://github.com/anguslees))
* Improvement - [Remove references to unused metadata `owner-id`](https://github.com/aws/amazon-vpc-cni-k8s/pull/1111) (#1111, [@anguslees](https://github.com/anguslees))
* Improvement - [Remove old pre-1.3 migration code](https://github.com/aws/amazon-vpc-cni-k8s/pull/1110) (#1110, [@anguslees](https://github.com/anguslees))
* Improvement - [Enable log config for the metrics agent](https://github.com/aws/amazon-vpc-cni-k8s/pull/1104) (#1104, [@mogren](https://github.com/mogren))
* Improvement - [Refactor ENI limit struct](https://github.com/aws/amazon-vpc-cni-k8s/pull/1035) (#1035, [@mogren](https://github.com/mogren))
* Improvement - [Use sed as a stream editor and redirect to file](https://github.com/aws/amazon-vpc-cni-k8s/pull/1069) (#1069, [@willejs](https://github.com/willejs))
* Improvement - [JSON output format for the entrypoint script](https://github.com/aws/amazon-vpc-cni-k8s/pull/1066) (#1066, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [Use install command instead of cp](https://github.com/aws/amazon-vpc-cni-k8s/pull/1061) (#1061, [@mogren](https://github.com/mogren))
* Improvement - [Updated manifest configs with default env vars](https://github.com/aws/amazon-vpc-cni-k8s/pull/1057) (#1057, [@saiteja313](https://github.com/saiteja313))
* Improvement - [Default to random-fully](https://github.com/aws/amazon-vpc-cni-k8s/pull/1048) (#1048, [@mogren](https://github.com/mogren))
* Improvement - [Update probe settings](https://github.com/aws/amazon-vpc-cni-k8s/pull/1028) (#1028, [@mogren](https://github.com/mogren))
* Improvement - [Added warning if delete on termination is set to false for the primary ENI](https://github.com/aws/amazon-vpc-cni-k8s/pull/1024) (#1024, [@jayanthvn](https://github.com/jayanthvn))
* Improvement - [Limit scope of logs writable by ipamd container](https://github.com/aws/amazon-vpc-cni-k8s/pull/987) (#987, [@anguslees](https://github.com/anguslees))
* Improvement - [Autogenerate per-region YAML manifests from a common template](https://github.com/aws/amazon-vpc-cni-k8s/pull/986) (#986, [@anguslees](https://github.com/anguslees))
* Improvement - [Persist IPAM state to local file and use across restarts](https://github.com/aws/amazon-vpc-cni-k8s/pull/972) (#972, [@anguslees](https://github.com/anguslees))
* Improvement - [Add init container](https://github.com/aws/amazon-vpc-cni-k8s/pull955) (#955, [@mogren](https://github.com/mogren))
* Improvement - [Refresh subnet/CIDR information periodically](https://github.com/aws/amazon-vpc-cni-k8s/pull/903) (#903, [@nithu0115](https://github.com/nithu0115))
* Docs - [Changed data type for variables in README](https://github.com/aws/amazon-vpc-cni-k8s/pull/1116) (#1116, [@abhinavmpandey08](https://github.com/abhinavmpandey08))
* Docs - [Fix docs links for cni-metrics-agent](https://github.com/aws/amazon-vpc-cni-k8s/pull/1072) (#1072, [@mogren](https://github.com/mogren))
* Testing - [Create script to run all release tests](https://github.com/aws/amazon-vpc-cni-k8s/pull/1106) (#1106, [@bnapolitan](https://github.com/bnapolitan))
* Testing - [Cover bottlerocket cluster test](https://github.com/aws/amazon-vpc-cni-k8s/pull/1096) (#1096, [@bnapolitan](https://github.com/bnapolitan))
* Testing - [Introduce automated performance testing](https://github.com/aws/amazon-vpc-cni-k8s/pull/1068) (#1068, [@bnapolitan](https://github.com/bnapolitan))
* Testing - [scripts/lib: bump up tester to v1.4.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/1065) (#1065, [@gyuho](https://github.com/gyuho))
* Testing - [Add parallel testing to conformance](https://github.com/aws/amazon-vpc-cni-k8s/pull/1018) (#1018, [@bnapolitan](https://github.com/bnapolitan))
* Testing - [Cache go packages in CircleCI](https://github.com/aws/amazon-vpc-cni-k8s/pull/1017) (#1017, [@bnapolitan](https://github.com/bnapolitan))
* Testing - [Create roles by default for e2e test cluster creation](https://github.com/aws/amazon-vpc-cni-k8s/pull/994) (#994, [@bnapolitan](https://github.com/bnapolitan))
* Bug - [Use limits from API for g4dn.16xlarge](https://github.com/aws/amazon-vpc-cni-k8s/pull/1086) (#1086, [@mogren](https://github.com/mogren))
* Bug - [Make metrics-helper docker logging statement multi-arch compatible](https://github.com/aws/amazon-vpc-cni-k8s/pull/1067) (#1067, [@nprab428](https://github.com/nprab428))

## v1.6.3
* Bug - [Handle stale instance metadata](https://github.com/aws/amazon-vpc-cni-k8s/pull/1011) (#1011, [@mogren](https://github.com/mogren))
* Improvement - [Add support for c5a and c5ad](https://github.com/aws/amazon-vpc-cni-k8s/pull/1003) (#1003, [@mogren](https://github.com/mogren))
* Improvement - [Make the aws-cni-support.sh executable](https://github.com/aws/amazon-vpc-cni-k8s/pull/1007) (#1007, [@jayanthvn](https://github.com/jayanthvn))

## v1.6.2
* Bug - [Add WithNoProxy to ignore proxies in gRPC connections when using unix sockets](https://github.com/aws/amazon-vpc-cni-k8s/pull/980) (#980, [@nithu0115](https://github.com/nithu0115))
* Improvement - [Fix order of file copies in entrypoint.sh](https://github.com/aws/amazon-vpc-cni-k8s/pull/935) (#935, [@dthorsen](https://github.com/dthorsen))
* Improvement - [Check all errors and log appropriately](https://github.com/aws/amazon-vpc-cni-k8s/pull/939) (#939, [@mogren](https://github.com/mogren))
* Improvement - [Add MTU and RPFilter configs to debug](https://github.com/aws/amazon-vpc-cni-k8s/pull/954) (#954, [@mogren](https://github.com/mogren))
* Improvement - [Bump aws-k8s-tester to v1.2.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/978) (#978, [@gyuho](https://github.com/gyuho))
* Improvement - [Add context and user agent to EC2 requests](https://github.com/aws/amazon-vpc-cni-k8s/pull/979) (#979, [@mogren](https://github.com/mogren))
* Improvement - [Update limits for m6g, c6g and r6g](https://github.com/aws/amazon-vpc-cni-k8s/pull/996) (#996, [@mogren](https://github.com/mogren))

## v1.6.1
* Feature - [Support architecture targeted builds](https://github.com/aws/amazon-vpc-cni-k8s/pull/837) (#837, [@jahkeup](https://github.com/jahkeup))
* Feature - [Zap logger](https://github.com/aws/amazon-vpc-cni-k8s/pull/824) (#824, [@nithu0115](https://github.com/nithu0115))
* Improvement - [Run conformance test as part of PR/Release certification](https://github.com/aws/amazon-vpc-cni-k8s/pull/851) (#851, [@SaranBalaji90](https://github.com/SaranBalaji90))
* Improvement - [Use eks:cluster-name as clusterId](https://github.com/aws/amazon-vpc-cni-k8s/pull/856) (#856, [@groodt](https://github.com/groodt))
* Improvement - [Bump Calico to v3.13.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/857) (#857, [@lmm](https://github.com/lmm))
* Improvement - [Use go.mod version of mockgen](https://github.com/aws/amazon-vpc-cni-k8s/pull/863) (#863, [@anguslees](https://github.com/anguslees))
* Improvement - [Mock /proc/sys](https://github.com/aws/amazon-vpc-cni-k8s/pull/870) (#870, [@anguslees](https://github.com/anguslees))
* Improvement - [Replace debug script with updated script from EKS AMI](https://github.com/aws/amazon-vpc-cni-k8s/pull/864) (#864, [@mogren](https://github.com/mogren))
* Improvement - [Update cluster-proportional-autoscaler to 1.7.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/885) (#885, [@ricardochimal](https://github.com/ricardochimal))
* Improvement - [Remove unnecessary/incorrect ClusterRole resource](https://github.com/aws/amazon-vpc-cni-k8s/pull/883) (#883, [@anguslees](https://github.com/anguslees))
* Improvement - [Disable IPv6 RA and ICMP redirects](https://github.com/aws/amazon-vpc-cni-k8s/pull/897) (#897, [@anguslees](https://github.com/anguslees))
* Improvement - [scripts/lib/aws.sh: use "aws-k8s-tester" v1.0.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/900) (#900, [@gyuho](https://github.com/gyuho))
* Improvement - [Configure rp_filter based on env variable](https://github.com/aws/amazon-vpc-cni-k8s/pull/902) (#902, [@SaranBalaji90](https://github.com/SaranBalaji90))
* Improvement - [Less verbose logging](https://github.com/aws/amazon-vpc-cni-k8s/pull/908) (#908, [@mogren](https://github.com/mogren))
* Improvement - [Reduce number of calls to EC2 API](https://github.com/aws/amazon-vpc-cni-k8s/pull/909) (#909, [@mogren](https://github.com/mogren))
* Improvement - [Bump containernetworking dependencies](https://github.com/aws/amazon-vpc-cni-k8s/pull/916) (#916, [@mogren](https://github.com/mogren))
* Improvement - [Use -buildmode=pie for binaries](https://github.com/aws/amazon-vpc-cni-k8s/pull/919) (#919, [@mogren](https://github.com/mogren))
* Bug - [Add missing permissions in typha-cpha sa (Calico)](https://github.com/aws/amazon-vpc-cni-k8s/pull/892) (#892, [@marcincuber](https://github.com/marcincuber))
* Bug - [Fix logging to stdout](https://github.com/aws/amazon-vpc-cni-k8s/pull/904) (#904, [@mogren](https://github.com/mogren))
* Bug - [Ensure non-nil Attachment in getENIAttachmentID](https://github.com/aws/amazon-vpc-cni-k8s/pull/915) (#915, [@jaypipes](https://github.com/jaypipes))

## v1.6.0

* Feature - [Add fallback to fetch limits from EC2 API](https://github.com/aws/amazon-vpc-cni-k8s/pull/782) (#782, [@mogren](https://github.com/mogren))
* Feature - [Additional tags to ENI](https://github.com/aws/amazon-vpc-cni-k8s/pull/734) (#734, [@nithu0115](https://github.com/nithu0115))
* Feature - [Add support for a 'no manage' tag](https://github.com/aws/amazon-vpc-cni-k8s/pull/726) (#726, [@euank](https://github.com/euank))
* Feature - [Use CRI to obtain pod sandbox IDs instead of Kubernetes API](https://github.com/aws/amazon-vpc-cni-k8s/pull/714) (#714, [@drakedevel](https://github.com/drakedevel))
* Feature - [Add support for listening on unix socket for introspection endpoint](https://github.com/aws/amazon-vpc-cni-k8s/pull/713) (#713, [@adammw](https://github.com/adammw))
* Feature - [Add MTU to the plugin config](https://github.com/aws/amazon-vpc-cni-k8s/pull/676) (#676, [@mogren](https://github.com/mogren))
* Feature - [Clean up leaked ENIs on startup](https://github.com/aws/amazon-vpc-cni-k8s/pull/624) (#624, [@mogren](https://github.com/mogren))
* Feature - [Introduce a minimum target for ENI IPs](https://github.com/aws/amazon-vpc-cni-k8s/pull/612) (#612, [@asheldon](https://github.com/asheldon))
* Feature - [Allow peered VPC CIDRs to be excluded from SNAT](https://github.com/aws/amazon-vpc-cni-k8s/pull/520) (#520, @totahuanocotl, @rewiko, [@yorg1st](https://github.com/yorg1st))
* Feature - [Get container ID from kube rather than docker](https://github.com/aws/amazon-vpc-cni-k8s/pull/371) (#371, [@rudoi](https://github.com/rudoi))
* Improvement - [Make entrypoint script fail if any step fails](https://github.com/aws/amazon-vpc-cni-k8s/pull/839) (#839, [@drakedevel](https://github.com/drakedevel))
* Improvement - [Place binaries in cmd/ and packages in pkg/](https://github.com/aws/amazon-vpc-cni-k8s/pull/815) (#815, [@jaypipes](https://github.com/jaypipes))
* Improvement - [De-dupe calls to DescribeNetworkInterfaces](https://github.com/aws/amazon-vpc-cni-k8s/pull/808) (#808, [@jaypipes](https://github.com/jaypipes))
* Improvement - [Update RollingUpdate strategy to allow 10% unavailable](https://github.com/aws/amazon-vpc-cni-k8s/pull/805) (#805, [@gavinbunney](https://github.com/gavinbunney))
* Improvement - [Bump github.com/vishvananda/netlink version from 1.0.0 to 1.1.0](https://github.com/aws/amazon-vpc-cni-k8s/pull/801) (#802, [@ajayk](https://github.com/ajayk))
* Improvement - [Adding node affinity for Fargate](https://github.com/aws/amazon-vpc-cni-k8s/pull/792) (#792, [@nithu0115](https://github.com/nithu0115))
* Improvement - [Force ENI/IP reconciliation to delete from the datastore](https://github.com/aws/amazon-vpc-cni-k8s/pull/754) (#754, [@tatatodd](https://github.com/tatatodd))
* Improvement - [Use dockershim.sock for CRI](https://github.com/aws/amazon-vpc-cni-k8s/pull/751) (#751, [@mogren](https://github.com/mogren))
* Improvement - [Treating ErrUnknownPod from ipamd to be a noop and not returning error](https://github.com/aws/amazon-vpc-cni-k8s/pull/750) (#750, [@uruddarraju](https://github.com/uruddarraju))
* Improvement - [Copy CNI plugin and config in entrypoint not agent](https://github.com/aws/amazon-vpc-cni-k8s/pull/735) (#735, [@jaypipes](https://github.com/jaypipes))
* Improvement - [Adding m6g instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/742) (#742, Srini Ramabadran)
* Improvement - [Remove deprecated session.New method](https://github.com/aws/amazon-vpc-cni-k8s/pull/729) (#729, [@nithu0115](https://github.com/nithu0115))
* Improvement - [Scope watch on "pods" to only pods associated with the local node](https://github.com/aws/amazon-vpc-cni-k8s/pull/716) (#716, [@jacksontj](https://github.com/jacksontj))
* Improvement - [Update ENI limits to match documentation](https://github.com/aws/amazon-vpc-cni-k8s/pull/710) (#710, [@mogren](https://github.com/mogren))
* Improvement - [Reduce image layers and strip debug flags](https://github.com/aws/amazon-vpc-cni-k8s/pull/699) (#699, [@mogren](https://github.com/mogren))
* Improvement - [Add run-integration-tests.sh script](https://github.com/aws/amazon-vpc-cni-k8s/pull/698) (#698, [@nckturner](https://github.com/nckturner))
* Improvement - [Return the error from ipamd to plugin](https://github.com/aws/amazon-vpc-cni-k8s/pull/688) (#688, [@mogren](https://github.com/mogren))
* Improvement - [Bump aws-sdk-go to v1.23.13](https://github.com/aws/amazon-vpc-cni-k8s/pull/681) (#681, [@mogren](https://github.com/mogren))
* Improvement - [Add support for m5n/m5dn/r5n/r5dn instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/657) (#657, [@Jeffwan](https://github.com/Jeffwan))
* Improvement - [Add IPs to the first ENI on startup](https://github.com/aws/amazon-vpc-cni-k8s/pull/648) (#648, [@mogren](https://github.com/mogren))
* Improvement - [Add shutdown listener](https://github.com/aws/amazon-vpc-cni-k8s/pull/645) (#645, [@mogren](https://github.com/mogren))
* Improvement - [Made timeouts exponential](https://github.com/aws/amazon-vpc-cni-k8s/pull/640) (#640, [@Zyqsempai](https://github.com/Zyqsempai))
* Improvement - [Remove vendor folder](https://github.com/aws/amazon-vpc-cni-k8s/pull/635) (#635, [@mogren](https://github.com/mogren))
* Improvement - [Update protobuf to v1.3.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/633) (#633, [@mogren](https://github.com/mogren))
* Improvement - [Reduce log level to Trace for the most common Debug lines](https://github.com/aws/amazon-vpc-cni-k8s/pull/631) (#631, [@mogren](https://github.com/mogren))
* Improvement - [Bump grpc version to v1.23.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/629) (#629, [@mogren](https://github.com/mogren))
* Improvement - [Add inCoolingPeriod for AddressInfo](https://github.com/aws/amazon-vpc-cni-k8s/pull/627) (#627, [@chendotjs](https://github.com/chendotjs))
* Improvement - [Added retryNbackoff for tagENI method](https://github.com/aws/amazon-vpc-cni-k8s/pull/626) (#626, [@nithu0115](https://github.com/nithu0115))
* Improvement - [Update backoff code from upstream and use when detaching ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/623) (#623, [@mogren](https://github.com/mogren))
* Improvement - [Update kubeconfig lookup with eksctl clusters](https://github.com/aws/amazon-vpc-cni-k8s/pull/513) (#513, [@dkeightley](https://github.com/dkeightley))
* Improvement - [Fix introspection port in troubleshooting docs](https://github.com/aws/amazon-vpc-cni-k8s/pull/512) (#512, [@drakedevel](https://github.com/drakedevel))
* Bug fix - [Log security groups correctly](https://github.com/aws/amazon-vpc-cni-k8s/pull/646) (#646, [@mogren](https://github.com/mogren))
* Bug fix - [Fix WARM_ENI_TARGET=0](https://github.com/aws/amazon-vpc-cni-k8s/pull/587) (#587, [@mogren](https://github.com/mogren))

# v1.5.7

* Improvement - [New AL2 image with iptables-1.8.2](https://github.com/aws/amazon-vpc-cni-k8s/pull/894) ([@mogren](https://github.com/mogren))
* Improvement - [Enable the `-buildmode=pie` flag for the binaries](https://github.com/aws/amazon-vpc-cni-k8s/pull/894) ([@mogren](https://github.com/mogren))
* Improvement - [Disable IPv6 RA and ICMP redirects on host-side veth](https://github.com/aws/amazon-vpc-cni-k8s/pull/894) ([@anguslees](https://github.com/anguslees))

# v1.5.6

* arm64 preview custom build

# v1.5.5

* Bug fix - [Revert "Return delete success for pods that never got scheduled"](https://github.com/aws/amazon-vpc-cni-k8s/pull/672/commits/474479d7455f41c514ffcd58390a2a3ebae26de5) (#672, [@mogren](https://github.com/mogren))
* Improvement - [Add support for r5dn instance family](https://github.com/aws/amazon-vpc-cni-k8s/pull/656) (#656, [@mogren](https://github.com/mogren))
* Improvement - [Add support for m5n/m5dn/r5n instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/657) (#657, [@Jeffwan](https://github.com/Jeffwan))
* Improvement - [Update cni-metrics-helper to v1.5.5](https://github.com/aws/amazon-vpc-cni-k8s/pull/672) (#672, [@mogren](https://github.com/mogren))
* Improvement - [Reduce image layers and strip debug flags](https://github.com/aws/amazon-vpc-cni-k8s/pull/699) (#699, [@mogren](https://github.com/mogren))

## v1.5.4

* Improvement - [Add support for g4dn instance family](https://github.com/aws/amazon-vpc-cni-k8s/pull/621) (#621, [@mogren](https://github.com/mogren))
* Improvement - [Set cniVersion in the config to 0.3.1 (required for Kubernetes 1.16)](https://github.com/aws/amazon-vpc-cni-k8s/pull/605) (#605, [@mogren](https://github.com/mogren))
* Bug fix - [Return delete success for pods that never got scheduled](https://github.com/aws/amazon-vpc-cni-k8s/commit/b0b2fc1be3cdb5cdde9ff4b13094488bf2c39d28) (#623, [@mogren](https://github.com/mogren))

## v1.5.3

* Bug fix - [Copy the binary and config after ipamd is ready](https://github.com/aws/amazon-vpc-cni-k8s/pull/576) (#576, [@mogren](https://github.com/mogren))
* Improvement - [Update Calico version to v3.8.1](https://github.com/aws/amazon-vpc-cni-k8s/pull/554) (#554, [@lmm](https://github.com/lmm))
* Improvement - [Add env var to override introspection bind address](https://github.com/aws/amazon-vpc-cni-k8s/pull/501) (#501, [@jacksontj](https://github.com/jacksontj))
* Improvement - [Remove unused env variable](https://github.com/aws/amazon-vpc-cni-k8s/pull/578) (#578, [@mogren](https://github.com/mogren))
* Improvement - [Exit early if MAC address doesn't match](https://github.com/aws/amazon-vpc-cni-k8s/pull/582) (#582, [@mogren](https://github.com/mogren))

## v1.5.2
* Bug fix - [Fix formatting flag](https://github.com/aws/amazon-vpc-cni-k8s/pull/521) (#521, [@uthark](https://github.com/uthark))
* Bug fix - [Fix formatting issue](https://github.com/aws/amazon-vpc-cni-k8s/pull/524) (#524, [@uthark](https://github.com/uthark))
* Bug fix - [Detach ENI before deleting](https://github.com/aws/amazon-vpc-cni-k8s/pull/538) (#538, [@uthark](https://github.com/uthark))
* Improvement - [Adding healthz endpoint to IPamD](https://github.com/aws/amazon-vpc-cni-k8s/pull/548) (#548, [@nithu0115](https://github.com/nithu0115))
* Improvement - [Adding new m5 and r5 instances](https://github.com/aws/amazon-vpc-cni-k8s/pull/518) (#518, [@mogren](https://github.com/mogren))
* Improvement - [t3a.small only have 2 ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/543) (#543, [@mogren](https://github.com/mogren))
* Improvement - [Updating AWS Go SDK version](https://github.com/aws/amazon-vpc-cni-k8s/pull/549) (#549, Nordlund, Eric)
* Improvement - [Reduce the wait time when checking for pods without IPs](https://github.com/aws/amazon-vpc-cni-k8s/pull/552) (#552, [@mogren](https://github.com/mogren))
* Improvement - [Update start script to wait for ipamd health](https://github.com/aws/amazon-vpc-cni-k8s/pull/472) (#552, [@mogren](https://github.com/mogren))
* Improvement - [Hide health check output](https://github.com/aws/amazon-vpc-cni-k8s/pull/569) (#569, [@mogren](https://github.com/mogren))
* Improvement - [Support c5.12xlarge and c5.24xlarge](https://github.com/aws/amazon-vpc-cni-k8s/pull/510) (#510, [@mogren](https://github.com/mogren))

## v1.5.1

* Bug fix - [Ignore namespace for custom eniconfig watch](https://github.com/aws/amazon-vpc-cni-k8s/pull/561) (#561, [@mogren](https://github.com/mogren))

## v1.5.0

* Bug fix - [Fix spelling on annotation](https://github.com/aws/amazon-vpc-cni-k8s/pull/482) (#482, [@forsberg](https://github.com/forsberg))
* Bug fix - [Avoid using force detach of ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/458) (#458, [@mogren](https://github.com/mogren))
* Bug fix - [Flush logs before exiting](https://github.com/aws/amazon-vpc-cni-k8s/pull/451) (#451, [@venkatesh-eb](https://github.com/venkatesh-eb))
* Improvement - [Add IPs to existing ENIs first](https://github.com/aws/amazon-vpc-cni-k8s/pull/487) (#487, [@mogren](https://github.com/mogren))
* Improvement - [Added error handling for GetENIipLimit](https://github.com/aws/amazon-vpc-cni-k8s/pull/484) (#484, [@Zyqsempai](https://github.com/Zyqsempai))
* Improvement - [Moved all GetEnv's calls to init step](https://github.com/aws/amazon-vpc-cni-k8s/pull/445) (#445, [@Zyqsempai](https://github.com/Zyqsempai))
* Improvement - [On start up, wait for pods with no IP](https://github.com/aws/amazon-vpc-cni-k8s/pull/480) (#480, [@mogren](https://github.com/mogren))
* Improvement - [Don't modify maxENI](https://github.com/aws/amazon-vpc-cni-k8s/pull/472) (#472, [@nckturner](https://github.com/nckturner))
* Improvement - [Improve WARM_IP_TARGET handling](https://github.com/aws/amazon-vpc-cni-k8s/pull/461) (#461, [@nckturner](https://github.com/nckturner))
* Improvement - [Update logging format to align messages](https://github.com/aws/amazon-vpc-cni-k8s/pull/473) (#473, [@mogren](https://github.com/mogren))
* Improvement - [Added -W (wait for xlock's) flag to iptables commands](https://github.com/aws/amazon-vpc-cni-k8s/pull/439) (#439, [@Zyqsempai](https://github.com/Zyqsempai))
* Improvement - [Remove error message from Prometheus labels](https://github.com/aws/amazon-vpc-cni-k8s/pull/467) (#467, [@bboreham](https://github.com/bboreham))
* Improvement - [Update instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/459) (#459, [@mogren](https://github.com/mogren))

## v1.4.1

* Feature - [Add flag to disable metrics and introspection](https://github.com/aws/amazon-vpc-cni-k8s/pull/436) (#436, [@mogren](https://github.com/mogren))
* Bug fix - [Adding additional CRD for Calico that was missing](https://github.com/aws/amazon-vpc-cni-k8s/pull/410) (#410, [@wmorgan6796](https://github.com/wmorgan6796))
* Improvement - [Update CNI metrics](https://github.com/aws/amazon-vpc-cni-k8s/pull/413) (#413, [@mogren](https://github.com/mogren))

## v1.4.0

* Feature - [Add an environment variable to limit the number of ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/251) (#251, [@pdbogen](https://github.com/pdbogen))
    - Makes it possible to limit how many ENIs that are allocated per node.
* Feature - [Randomize outgoing port for connections in the SNAT iptables rule](https://github.com/aws/amazon-vpc-cni-k8s/pull/246) (#246, [@taylorb-syd](https://github.com/taylorb-syd))
    - To avoid a race condition when using SNAT, select ports randomly instead of sequentially.
* Feature - [ENIConfig set by custom annotation or label names](https://github.com/aws/amazon-vpc-cni-k8s/pull/280) (#280, [@etopeter](https://github.com/etopeter))
    - Enables users to set a custom annotation or label key to define ENIConfig name.
* Improvement - [Update Calico to 3.3.6](https://github.com/aws/amazon-vpc-cni-k8s/pull/368) (#368, [@2ffs2nns](https://github.com/2ffs2nns))
* Improvement - [Add new instance types](https://github.com/aws/amazon-vpc-cni-k8s/pull/366) (#366, [@mogren](https://github.com/mogren))
    - Adds m5ad and r5ad families.
* Improvement - [Actually enable prometheus metrics](https://github.com/aws/amazon-vpc-cni-k8s/pull/361) (#361, [@mogren](https://github.com/mogren))
* Improvement - [Retry LinkByMac when link not found](https://github.com/aws/amazon-vpc-cni-k8s/pull/360) (#360, [@peterbroadhurst](https://github.com/peterbroadhurst))
    - Sometimes it takes a few seconds for a new ENI to be available, so we retry 5 times.
* Improvement - [Run `yum clean all` to reduce image size](https://github.com/aws/amazon-vpc-cni-k8s/pull/351) (#351, [@mogren](https://github.com/mogren))
* Improvement - [Renaming Prometheus metrics with "awscni_" prefix](https://github.com/aws/amazon-vpc-cni-k8s/pull/348) (#348, [@max-rocket-internet](https://github.com/max-rocket-internet))
* Improvement - [Allow configuring docker image when running make](https://github.com/aws/amazon-vpc-cni-k8s/pull/178) (#178, [@mikkeloscar](https://github.com/mikkeloscar))
* Improvement - [Add support for stdout logging](https://github.com/aws/amazon-vpc-cni-k8s/pull/342) (#342, [@rudoi](https://github.com/rudoi))
    - Adds the environment variable `AWS_VPC_K8S_CNI_LOG_FILE` that can be set to `stdout` or a file path.
* Improvement - [Some cleanups related to #234](https://github.com/aws/amazon-vpc-cni-k8s/pull/244) (#244, [@mogren](https://github.com/mogren))
* Improvement - [Use apps/v1 for DaemonSet](https://github.com/aws/amazon-vpc-cni-k8s/pull/341) (#341, [@errordeveloper](https://github.com/errordeveloper))
* Improvement - [Clean up aws-cni-support.sh and update the documentation](https://github.com/aws/amazon-vpc-cni-k8s/pull/320) (#320, [@mogren](https://github.com/mogren))
* Improvement - [Fix tiny typo in log message](https://github.com/aws/amazon-vpc-cni-k8s/pull/324) (#323, #324, [@ankon](https://github.com/ankon))
* Improvement - [Collect rp_filter from all network interface in aws-cni-support.sh](https://github.com/aws/amazon-vpc-cni-k8s/pull/338) (#338, [@nak3](https://github.com/nak3))
* Improvement - [Use device number 0 for primary device in unit test](https://github.com/aws/amazon-vpc-cni-k8s/pull/247) (#247, [@nak3](https://github.com/nak3))
* Improvement - [Collect iptables -nvL -t mangle in support script](https://github.com/aws/amazon-vpc-cni-k8s/pull/304) (#304, [@nak3](https://github.com/nak3))
* Improvement - [Return the err from f.Close()](https://github.com/aws/amazon-vpc-cni-k8s/pull/249) (#249, [@mogren](https://github.com/mogren))
* Improvement - [Explicitly set the IP on secondary ENIs](https://github.com/aws/amazon-vpc-cni-k8s/pull/271) (#271, [@ewbankkit](https://github.com/ewbankkit))
    - Fixes IP bug on older kernels.
* Improvement - [Update instance ENI and IP mapping table](https://github.com/aws/amazon-vpc-cni-k8s/pull/275) (#275, [@hmizuma](https://github.com/hmizuma))
    - Adds a1 and c5n instances. (Already included in v1.3.2) 
* Improvement - [Add ENI entries for p3dn.24xlarge instance](https://github.com/aws/amazon-vpc-cni-k8s/pull/274) (#274, [@hmizuma](https://github.com/hmizuma))
    - p3dn.24xlarge was already included in v1.3.2 
* Improvement - [Use InClusterConfig when CreateKubeClient() was called without args](https://github.com/aws/amazon-vpc-cni-k8s/pull/293) (#293, [@nak3](https://github.com/nak3))
* Improvement - [Expose configuration variables via ipamD to make it debug friendly](https://github.com/aws/amazon-vpc-cni-k8s/pull/287) (#287, [@nak3](https://github.com/nak3))
* Improvement - [Allow cross compile on different platform ](https://github.com/aws/amazon-vpc-cni-k8s/pull/292) (#292, [@nak3](https://github.com/nak3))
* Improvement - [Add changes to support multiple platform build](https://github.com/aws/amazon-vpc-cni-k8s/pull/286) (#286, [@mbartsch](https://github.com/mbartsch))
    - arm64 build support
* Improvement - [Improve setup advice in README around ENI / IP ](https://github.com/aws/amazon-vpc-cni-k8s/pull/276) (#276 [@sftim](https://github.com/sftim))
* Improvement - [Use `unix.RT_TABLE_MAIN` for main routing table number](https://github.com/aws/amazon-vpc-cni-k8s/pull/269) (#269, [@nak3](https://github.com/nak3))
* Improvement - [Detect if mockgen and goimports are in the path](https://github.com/aws/amazon-vpc-cni-k8s/pull/278) (#278, [@nak3](https://github.com/nak3))
* Improvement - [Increment IP address safely](https://github.com/aws/amazon-vpc-cni-k8s/pull/258) (#258, [@nak3](https://github.com/nak3))
    - Calculate the gateway IP in a safe way.
* Improvement - [Remove unused options from rpc.proto](https://github.com/aws/amazon-vpc-cni-k8s/pull/252) (#252, [@nak3](https://github.com/nak3))
* Improvement - [Add missing unit tests execution to Makefile](https://github.com/aws/amazon-vpc-cni-k8s/pull/253) (#253, [@nak3](https://github.com/nak3))
* Improvement - [Bump TravisCI to use 1.11](https://github.com/aws/amazon-vpc-cni-k8s/pull/243) (#243, [@mogren](https://github.com/mogren))
* Bug fix - [Fix typos in json types for ENIConfig](https://github.com/aws/amazon-vpc-cni-k8s/pull/393) (#393, [@tiffanyfay](https://github.com/tiffanyfay))
* Bug fix - [Avoid unbound variable error in aws-cni-support.sh](https://github.com/aws/amazon-vpc-cni-k8s/pull/382) (#382, [@StevenACoffman](https://github.com/StevenACoffman))
* Bug fix - [Output CIDR in correct format](https://github.com/aws/amazon-vpc-cni-k8s/pull/267) (#267, [@nak3](https://github.com/nak3))
* Bug fix - [Use replace when adding host route](https://github.com/aws/amazon-vpc-cni-k8s/pull/367) (#367, [@mogren](https://github.com/mogren))
* Bug fix - [Update k8sapi to use operator-framework inClusterConfig](https://github.com/aws/amazon-vpc-cni-k8s/pull/364) (#364, [@tiffanyfay](https://github.com/tiffanyfay))
    - If the environment variables are missing, fall back to DNS lookup.
* Bug fix - [Set mainENIRule mask](https://github.com/aws/amazon-vpc-cni-k8s/pull/340) (#340, [@tustvold](https://github.com/tustvold))
    - In order to match the connmark correctly, we need to mask it out when checking.
* Bug fix - [Use primary interface to add iptables for connmark entry](https://github.com/aws/amazon-vpc-cni-k8s/pull/305) (#305, [@nak3](https://github.com/nak3))
* Bug fix - [Stop wrapping and returning nil](https://github.com/aws/amazon-vpc-cni-k8s/pull/245) (#245, [@nak3](https://github.com/nak3))
* Bug fix - [Fix return path of NodePort traffic when using Calico network policy](https://github.com/aws/amazon-vpc-cni-k8s/pull/263) (#263, [@ikatson](https://github.com/ikatson))
* Bug fix - [Remove scope: Cluster from spec.names](https://github.com/aws/amazon-vpc-cni-k8s/pull/199) (#199, [@rickardrosen](https://github.com/rickardrosen))
* Bug fix - [Remove unneeded spec entry in v1.3 manifest](https://github.com/aws/amazon-vpc-cni-k8s/pull/262) (#262, [@hmizuma](https://github.com/hmizuma))
* Bug fix - [Add formatter to errors.Wrapf in driver](https://github.com/aws/amazon-vpc-cni-k8s/pull/241) (#241, [@nak3](https://github.com/nak3))

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
