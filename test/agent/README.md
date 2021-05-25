### Test Agent
The test agent contains multiple binaries that are used by Ginkgo Automation tests. 

### List of Go Binaries in the Agent

Currently the agent supports the following Go Binaries.

#### Traffic Testing

For traffic validation across multiple pods we have the following 3 Go binaries.

**traffic-server**

The traffic server starts a TCP/UDP server that listens for incoming traffic from multiple clients. 

**traffic-client**

The traffic client takes a list of servers as input parameter and tests connection to those server and finally prints the success/failure status to stdout and optionally pushes the results to metric server.

**metric-server**

The metric server is supposed collects metrics from all traffic clients and allow the test suite to just query the metric server instead of getting stdout for each client.

One way to run the traffic test is as follows
- Deploy N Servers 
- Deploy M Client pods and pass the IP Address of N Servers as input.
- Query the metric server to see the Success/Failure Rate.

#### Networking Testing
Networking testing binary must be invoked on a pod that runs in Host Networking mode. It is capable of testing the Pod networking is setup correctly and once the Pod has been deleted the networking has been teared down correctly by the CNI Plugin.

### Using the local changes to Agent inside ginkgo 
Ginkgo tests take dependency on the Agent module for supplying PodNetworkingValidationInput as of now.
If you are making changes to the PodNetworkingValidationInput then gingko must use local copy of agent with your modifications instead of downloading the upstream module to reflect those changes.
You can modify the go.mod file located inside test folder as below to enable ginkgo to use local copy of the agent
```
replace github.com/aws/amazon-vpc-cni-k8s/test/agent => <absolute local path>/amazon-vpc-cni-k8s/test/agent>
```

### How to test the Agent locally
Apart from running the tests on your local environment. For some test cases where we want to run test on Pod (for instance Pod networking tests) we can copy over the binary to the Pod and execute it. For e2e testing, we can push docker image to ECR and use the image in automation test suite.

#### Running individual test component inside a Pod
- While development you could generate the binary for the component you want to test. Let's say you would like to test the pod networking setup on a host network pod.
  ```
  COMPONENT=networking #Example, when testing networking go binary
    
  CGO_ENABLED=0 \
  GOOS=linux \
  GOARCH=amd64 \
  GO111MODULE=on \
  go build -o $COMPONENT cmd/$COMPONENT/main.go
  ```
- Copy the binary to your tester pod.
  ```
  TESTER_POD=<pod-name>
  kubectl cp $COMPONENT TESTER_POD:/tmp/
  ```
- Execute the binary by doing an exec into the pod and see the desired results.
  ```
  kubectl exec -ti $TESTER_POD sh
  ./tmp/<component> --<flags>
  ```

#### Running the docker Image

Run the following command to build the agent image and push to ECR. This needs an existing repository with name "aws-vpc-cni-test-helper"
```
AWS_ACCOUNT=<account> AWS_REGION=<region> make docker-build docker-push
``` 
Change the agent image in the Go code and run the test case as usual.

#### Finalizing the changes
- Submit PR with the change to Agent.
- Once the PR is merged get the commit hash and generate a new version for the agent changes 
```
go get -d github.com/aws/amazon-vpc-cni-k8s/test/agent@<commit_hash>
```
Sample Output
```
go: github.com/aws/amazon-vpc-cni-k8s/test/agent 4d1931b480a23d2c6cb0eee8adef5bd3b2fbefac => v0.0.0-20210519174950-4d1931b480a2
go: downloading github.com/aws/amazon-vpc-cni-k8s/test/agent v0.0.0-20210519174950-4d1931b480a2
```
- Update the version in the go.mod file under test folder
```
github.com/aws/amazon-vpc-cni-k8s/test/agent v0.0.0-20210519174950-4d1931b480a2
```
- One of the AWS Maintainer will push the image to ECR (Till we have pipeline that does this for us)
- Use the updated image tag wherever you want to update the docker image in automation tests.

### Future Improvements
Currently the aws-vpc-cni Maintainers have to manually push the ECR image after any change to agent. In future, we would like to push a new image using a pipeline on any updates to the agent directory.
