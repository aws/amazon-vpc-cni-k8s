module github.com/aws/amazon-vpc-cni-k8s/test

go 1.14

require (
	github.com/aws/amazon-vpc-cni-k8s v1.7.10
	github.com/aws/aws-sdk-go v1.37.23
	github.com/google/gopacket v1.1.19 // indirect
	github.com/gophercloud/gophercloud v0.1.0 // indirect
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.11.0
	github.com/pkg/errors v0.9.1
	github.com/vishvananda/netlink v1.1.1-0.20201029203352-d40f9887b852
	k8s.io/api v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	sigs.k8s.io/controller-runtime v0.6.3
)
