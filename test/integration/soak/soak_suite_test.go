package soak_test

import (
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var f *framework.Framework

func TestSoak(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Soak Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)
})
