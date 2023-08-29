package warm_pool

import (
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	"strconv"
)

// Environment variables are not reset before and after each test so that way multiple tests can be run to
// evaluate behavior. You can run this test which will unset all warm pool environment variables. Or, if you
// want to test the behavior with some of those environment variables set, alter them in that file and run it once before
// you run the desired tests.
var _ = Describe("set warm env", func() {
	Context("Sets env variables", func() {

		It("Sets env variables", func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{
					"WARM_IP_TARGET":           strconv.Itoa(0),
					"ENABLE_DYNAMIC_WARM_POOL": strconv.FormatBool(true),
				})
		})
	})
})
