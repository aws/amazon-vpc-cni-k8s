# E2E testing

The `e2e` package consists of two main parts which get built and run as pods:
* `testpod`
* `cni`

### `testpod`
* `testpod` runs as a service. Each pod first runs a server. Then it runs a test that checks whether each running testpod pod in the cluster can hit the test endpoint on the pod, if it can do a DNS lookup, and if the associated cluster and pods in the service can be reached. The test also creates Prometheus metrics for the failure counters.
* The deployment and service specs are located at: https://github.com/aws/aws-k8s-tester/blob/master/e2e/resources/testpod.go

### `cni`
* `cni` has the main bulk of the e2e test code and launches deployments/pods and services for `prometheus` and `testpod` as well as checking the expected number of allocated ENIs and IPs for a node. 
* It also annotates the CNI plugin, daemonset `aws-node`, with the environment variable 

## Create RBAC resources
Several RBAC resources are needed in order to run the E2E test. They can be applied via the following command:
```
kubectl apply  -f config/setup.yaml
```

### Build images
First, you need to build the images for `aws-node`, `cni-e2e`, and `testpod`. These then need to be pushed a registry such as Amazon ECR.
```bash
# Build CNI image
make docker 

# Build CNI E2E test image
make build-docker-e2e

# Build testpod image
make build-docker-testpod
```
### Update CNI version
To update the CNI pod image with the one you just built and pushed, replace the image path and run the following:
```bash
kubectl patch daemonset aws-node -n kube-system -p '{"spec": {"template": {"spec": {"containers": [{"image": "<amazon-k8s-cni-image>","name":"aws-node"}]}}}}'
```

### Run CNI E2E test
The E2E test runs as a pod with a [Gingko](https://onsi.github.io/ginkgo/) test. Replace the `testpod` image with the one you built and pushed. 

First, you need to create RBAC permissions for running the test.
```
kubectl apply  -f config/setup.yaml
```

Substitute the images and run the test
```bash
export CNI_E2E_TEST_IMAGE_URI=<image>
export TESTPOD_IMAGE_URI=<image>
sed -i \
    -e "s,{{CNI_E2E_TEST_IMAGE_URI}},$CNI_E2E_TEST_IMAGE_URI," \
    -e "s,{{TESTPOD_IMAGE_URI}},$TESTPOD_IMAGE_URI," \
    config/cni-e2e.yaml \

kubectl apply  -f config/e2e.yaml
```

Check logs as test runs
```
kubectl logs -n cni-test -f cni-e2e
```