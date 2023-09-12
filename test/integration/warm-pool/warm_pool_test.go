package warm_pool

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	"time"
)

// This function represents the test pattern for each test case
var _ = Describe("...", func() {
	Context("test functions", func() {
		var testFn func(f *framework.Framework, deployBuilder *manifest.DeploymentBuilder) (*v1.Deployment, error)

		JustBeforeEach(func() {

			// create deployment spec
			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Name("busybox").
				Namespace(utils.DefaultTestNamespace).
				Replicas(minPods)

			start := time.Now().Unix()
			// run test function
			deployment, err := testFn(f, deploymentSpec)
			Expect(err).ToNot(HaveOccurred())
			end := time.Now().Unix()

			createCurlPod()
			getPrometheusMetrics(start, end)
			// deletes deployment returned by test case
			deleteDeployment(deployment)
			deleteCurlPod()
		})

		Context("test fn 1", func() {
			BeforeEach(func() {
				testFn = QuickScale
			})
			It("running test 1", func() {})
		})

		//Context("test fn 2", func() {
		//	BeforeEach(func() {
		//		testFn = RandomScale
		//	})
		//	It("running test 2", func() {})
		//})
		//
		//Context("test fn 3", func() {
		//	BeforeEach(func() {
		//		testFn = ProportionateScaling
		//	})
		//	It("running test 3", func() {})
		//})
	})
})
