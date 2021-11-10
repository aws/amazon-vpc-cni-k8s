module github.com/aws/amazon-vpc-cni-k8s/test/integration

go 1.14

replace k8s.io/api => k8s.io/api v0.21.5

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.5

replace k8s.io/apimachinery => k8s.io/apimachinery v0.21.6-rc.0

replace k8s.io/apiserver => k8s.io/apiserver v0.21.5

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.21.5

replace k8s.io/client-go => k8s.io/client-go v0.21.5

replace k8s.io/component-helpers => k8s.io/component-helpers v0.21.5

replace k8s.io/controller-manager => k8s.io/controller-manager v0.21.5

replace k8s.io/mount-utils => k8s.io/mount-utils v0.21.6-rc.0

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.21.5

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.5

replace k8s.io/code-generator => k8s.io/code-generator v0.21.6-rc.0

replace k8s.io/component-base => k8s.io/component-base v0.21.5

replace k8s.io/cri-api => k8s.io/cri-api v0.21.6-rc.0

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.21.5

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.5

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.21.5

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.21.5

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.21.5

replace k8s.io/kubectl => k8s.io/kubectl v0.21.5

replace k8s.io/kubelet => k8s.io/kubelet v0.21.5

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.21.5

replace k8s.io/metrics => k8s.io/metrics v0.21.5

replace k8s.io/node-api => k8s.io/node-api v0.21.5

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.21.5

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.21.5

replace k8s.io/sample-controller => k8s.io/sample-controller v0.21.5

replace github.com/mholt/caddy => github.com/caddyserver/caddy v1.0.3

require (
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.7.0
	k8s.io/api v0.21.5
	k8s.io/apimachinery v0.21.5
	k8s.io/client-go v0.21.5
	k8s.io/kubernetes v1.21.5
)
