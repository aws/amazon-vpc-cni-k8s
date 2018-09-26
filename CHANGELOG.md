# Changelog

## 1.0.0

Initial release of **amazon-vpc-cni-k8s**  a cni plugin for use with Kubernetes that uses ENIs and secondary ip addresses.

See the [README](README.md) for additional information.

## 1.1.0

* Feature — Versioning with git SHA [#106](https://github.com/aws/amazon-vpc-cni-k8s/pull/106)
* Feature — Ability to configure secondary IP preallocation (https://github.com/aws/amazon-vpc-cni-k8s/pull/125)
* Feature — Allow pods communicate with outside VPC without NAT[#81](https://github.com/aws/amazon-vpc-cni-k8s/pull/81)
* Improvement — Added travis CI support [#116](https://github.com/aws/amazon-vpc-cni-k8s/pull/116), [#117](https://github.com/aws/amazon-vpc-cni-k8s/pull/117), [#118](https://github.com/aws/amazon-vpc-cni-k8s/pull/118)
* Improvement — Modify toleration to make aws-node schedule-able on all nodes [#128](https://github.com/aws/amazon-vpc-cni-k8s/pull/128)
* Improvement — Move from TagResources to CreateTags for ENI Tagging [#129](https://github.com/aws/amazon-vpc-cni-k8s/pull/129)
* Improvement — Updated troubleshooting guidelines
* Bug Fix — Release IP to datastore upon failure [#127](https://github.com/aws/amazon-vpc-cni-k8s/pull/127)

## 1.2.0

* Feature - Add hostPort support [#153](https://github.com/aws/amazon-vpc-cni-k8s/pull/153)
* Feature - Add a configuration knob to allow Pod to use different VPC SecurityGroups and Subnet [#165](https://github.com/aws/amazon-vpc-cni-k8s/pull/165)
* Feature - Fix return path of NodePort traffic [#130](https://github.com/aws/amazon-vpc-cni-k8s/pull/130)
* Improvement - Add more error messages during initialization [#174](https://github.com/aws/amazon-vpc-cni-k8s/pull/174)
* Improvement - Check to make it is a Pod object [#170](https://github.com/aws/amazon-vpc-cni-k8s/pull/170)
* Improvement - Maintain the right number of ENIs and its IP addresses in WARM-IP pool [#169](https://github.com/aws/amazon-vpc-cni-k8s/pull/169)
* Improvement - Add support for more instance types: r5, r5d, z1d, t3 [#145](https://github.com/aws/amazon-vpc-cni-k8s/pull/145)
