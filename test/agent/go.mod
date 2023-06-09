module github.com/aws/amazon-vpc-cni-k8s/test/agent

go 1.20

require (
	github.com/coreos/go-iptables v0.6.0
	github.com/vishvananda/netlink v1.1.0
	golang.org/x/sys v0.8.0
)

require github.com/vishvananda/netns v0.0.0-20191106174202-0a2b9b5464df // indirect
