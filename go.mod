module github.com/aws/amazon-vpc-cni-k8s

go 1.14

require (
	github.com/aws/aws-sdk-go v1.40.6
	github.com/containernetworking/cni v0.8.0
	github.com/containernetworking/plugins v0.9.0
	github.com/coreos/go-iptables v0.4.5
	github.com/golang/mock v1.4.1
	github.com/golang/protobuf v1.4.2
	github.com/google/go-jsonnet v0.16.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.0.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.4.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.5.1
	github.com/vishvananda/netlink v1.1.1-0.20201029203352-d40f9887b852
	go.uber.org/zap v1.15.0
	golang.org/x/lint v0.0.0-20210508222113-6edffad5e616 // indirect
	golang.org/x/net v0.0.0-20210805182204-aaa1db679c0d
	golang.org/x/sys v0.0.0-20210616094352-59db8d763f22
	golang.org/x/tools v0.1.5 // indirect
	google.golang.org/grpc v1.29.0
	google.golang.org/protobuf v1.23.0
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	k8s.io/api v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	k8s.io/cri-api v0.0.0-20191107035106-03d130a7dc28
	sigs.k8s.io/controller-runtime v0.6.3
)
