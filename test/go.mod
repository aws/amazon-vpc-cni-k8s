module github.com/aws/amazon-vpc-cni-k8s/test

go 1.14

require (
	github.com/apparentlymart/go-cidr v1.0.1
	github.com/aws/amazon-vpc-cni-k8s v1.9.0
	github.com/aws/amazon-vpc-cni-k8s/test/agent v0.0.0-20210519174950-4d1931b480a2
	github.com/aws/amazon-vpc-resource-controller-k8s v1.0.7
	github.com/aws/aws-sdk-go v1.40.6
	github.com/google/gopacket v1.1.17
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.14.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.26.0
	github.com/vishvananda/netlink v1.1.1-0.20201029203352-d40f9887b852
	gopkg.in/yaml.v2 v2.4.0
	helm.sh/helm/v3 v3.6.1
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/cli-runtime v0.21.0
	k8s.io/client-go v0.21.3
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/controller-runtime v0.9.5
)
