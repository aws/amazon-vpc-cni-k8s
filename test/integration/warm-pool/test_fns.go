package warm_pool

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	. "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/api/apps/v1"
	"strconv"
)

// Scales the cluster from minPods up to maxPods and back down to minPods
func QuickScale(f *framework.Framework, deploymentSpec *manifest.DeploymentBuilder) (*v1.Deployment, error) {
	// create deployment
	deployment, err := changeReplicas(deploymentSpec, minPods)

	fmt.Fprintf(GinkgoWriter, "Scaling cluster up to %v pods\n", maxPods)
	deployment, err = changeReplicas(deploymentSpec, maxPods)

	if maxPods != busyboxPodCnt() {
		return deployment, err
	}

	fmt.Fprintf(GinkgoWriter, "Scaling cluster down to %v pods\n", minPods)
	deployment, err = changeReplicas(deploymentSpec, minPods)

	// return deployment so that it can be deleted later
	return deployment, err
}

// Scales the cluster to minPods, then it loops through a number of iterations. On each iteration, the cluster will add
// or subtract a random amount of pods depending on the value of randDigits. The behavior could also result in no change
// to the cluster.
func RandomScale(f *framework.Framework, deploymentSpec *manifest.DeploymentBuilder) (*v1.Deployment, error) {
	// create deployment
	var err error

	// create deployment
	deployment := deploymentSpec.Build()

	replicas := minPods

	for i := 0; i < iterations; i++ {
		By("Loop " + strconv.Itoa(i))
		result, op, randPods := randOpLoop(replicas)
		replicas = checkInRange(result)
		if op == "no change" {
			fmt.Fprintf(GinkgoWriter, "No change to cluster, %v pods", replicas)
			continue
		} else {
			fmt.Fprintf(GinkgoWriter, "Scaling cluster to %v pods by %v %v pods\n", replicas, op, randPods)
		}
		deployment, err = changeReplicas(deploymentSpec, replicas)
		if maxPods != busyboxPodCnt() {
			return deployment, err
		}
	}
	return deployment, err
}

// Scales the cluster up proportionately from minPods to maxPods dependent on what the value of scale is set to, and
// then back down to minPods at the same scale.
func ProportionateScaling(f *framework.Framework, deploymentSpec *manifest.DeploymentBuilder) (*v1.Deployment, error) {
	// create deployment
	var err error
	deployment, err := changeReplicas(deploymentSpec, minPods)

	scaleAmt := maxPods * scale
	replicas := minPods

	for replicas < maxPods {
		i := 0
		By("Loop " + strconv.Itoa(i))
		// Will scale to a maximum of maxPods
		replicas = min(replicas+int(scaleAmt), maxPods)
		fmt.Fprintf(GinkgoWriter, "Scaling cluster up to %v pods by adding %v pods\n", replicas, int(scaleAmt))
		deployment, err = changeReplicas(deploymentSpec, replicas)
		if maxPods != busyboxPodCnt() {
			return deployment, err
		}
		i++
	}

	for replicas > minPods {
		i := 0
		By("Loop " + strconv.Itoa(i))
		// Will scale to a minimum of minPods
		replicas = max(replicas-int(scaleAmt), minPods)
		fmt.Fprintf(GinkgoWriter, "Scaling cluster down to %v pods by subtracting %v pods\n", replicas,
			int(scaleAmt))
		deployment, err = changeReplicas(deploymentSpec, replicas)
		if maxPods != busyboxPodCnt() {
			return deployment, err
		}
		i++
	}
	return deployment, err
}
