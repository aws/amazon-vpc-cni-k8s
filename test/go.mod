module github.com/aws/amazon-vpc-cni-k8s/test

go 1.14

require (
	github.com/apparentlymart/go-cidr v1.0.1
	github.com/aws/amazon-vpc-cni-k8s v1.7.10
	github.com/aws/amazon-vpc-cni-k8s/test/agent v0.0.0-20211209222755-86ece934e91a
	github.com/aws/amazon-vpc-resource-controller-k8s v1.0.7
	github.com/aws/aws-sdk-go v1.40.6
	github.com/containerd/containerd v1.6.1 // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/onsi/ginkgo/v2 v2.3.1
	github.com/onsi/gomega v1.22.0
	github.com/pkg/errors v0.9.1
	gopkg.in/yaml.v2 v2.4.0
	helm.sh/helm/v3 v3.8.0
	k8s.io/api v0.23.5
	k8s.io/apimachinery v0.23.5
	k8s.io/cli-runtime v0.23.1
	k8s.io/client-go v0.23.5
	sigs.k8s.io/controller-runtime v0.11.2
)

replace github.com/emicklei/go-restful => github.com/emicklei/go-restful/v3 v3.8.0
