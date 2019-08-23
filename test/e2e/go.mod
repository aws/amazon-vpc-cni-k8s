module github.com/aws/amazon-vpc-cni-k8s/test/e2e

go 1.12

require (
	github.com/aws/amazon-vpc-cni-k8s v1.5.3
	github.com/aws/aws-k8s-tester v0.0.0-20190829184027-9a2c913f3bd9
	github.com/aws/aws-sdk-go v1.23.6
	github.com/cihub/seelog v0.0.0-20170130134532-f561c5e57575
	github.com/davecgh/go-spew v1.1.1
	github.com/evanphx/json-patch v4.5.0+incompatible // indirect
	github.com/onsi/ginkgo v1.9.0
	github.com/onsi/gomega v1.6.0
	github.com/prometheus/client_golang v1.1.0
	github.com/stretchr/testify v1.4.0 // indirect
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	k8s.io/api v0.0.0-20190115191648-dd23d9f710e2
	k8s.io/apimachinery v0.0.0-20190111195121-fa6ddc151d63
	k8s.io/client-go v2.0.0-alpha.0.0.20190112054256-b831b8de7155+incompatible
)
