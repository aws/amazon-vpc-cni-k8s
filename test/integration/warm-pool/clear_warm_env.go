package warm_pool

import (
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	. "github.com/onsi/ginkgo/v2"
)

// Environment variables are not reset before and after each test so that way multiple tests can be run to
// evaluate behavior. You can run this test which will unset all warm pool environment variables. Or, if you
// want to test the behavior with some of those environment variables set, alter them in that file and run it once before
// you run the desired tests.
var _ = Describe("clear warm env", func() {
	Context("Clear out environment variables for warm pool for testing", func() {

		It("Unsetting env variables", func() {
			k8sUtils.UpdateEnvVarOnDaemonSetAndWaitUntilReady(f, "aws-node", "kube-system",
				"aws-node", map[string]string{},
				map[string]struct{}{
					"WARM_ENI_TARGET":    {},
					"WARM_IP_TARGET":     {},
					"MINIMUM_IP_TARGET":  {},
					"WARM_PREFIX_TARGET": {},
				})
		})
	})
})
