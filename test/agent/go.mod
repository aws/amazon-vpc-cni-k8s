module github.com/aws/amazon-vpc-cni-k8s/test/agent

go 1.24.1

require (
	github.com/coreos/go-iptables v0.8.0
	github.com/vishvananda/netlink v1.3.1
	golang.org/x/sys v0.33.0
)

require github.com/vishvananda/netns v0.0.5 // indirect
