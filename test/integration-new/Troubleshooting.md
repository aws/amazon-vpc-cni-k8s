## Troubleshooting Test Failures

To make debugging efforts easier, you can do the following:

1. Adding focus to individual failing tests to selectively run them in case of troubleshooting. The most straightforward way would be to add an F in front of your It for the failing test. Another simple way would be to add --focus=<focus_value> in ginkgo command where the <focus_value> provided would be matched to the string description of the tests.

```
FIt("connection should be established", func() {
			CheckConnectivityForMultiplePodPlacement(
				interfaceToPodListOnPrimaryNode, interfaceToPodListOnSecondaryNode,
				serverPort, testerExpectedStdOut, testerExpectedStdErr, testConnectionCommandFunc)

			By("verifying connection fails for unreachable port")
			VerifyConnectivityFailsForNegativeCase(interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
				interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[1], serverPort,
				testFailedConnectionCommandFunc)
		})

Running Suite: CNI Pod Networking Suite
=======================================
Random Seed: 1643400657
Will run 2 of 15 specs
```
With 'Focus' added, ginkgo will run only those specs

2. Comment out the AfterEach or AfterSuite section which cleans up your test resources, rerun the failing test in focussed mode and inspect for any errors using pod logs or pod events
3. If a test fails, resource cleanup might not happen. To recover you can just delete the test namespace

```
kubectl delete ns cni-automation
```

### Few other things to check if you run into Failures
**cni tests**  
Ensure that you pass 'ng-name-label-key' and 'ng-name-label-val' to trigger ginkgo tests  
You can get any label that identifies your nodes by describing your nodes   

```
kubectl describe nodes

kubectl describe nodes
Name:               ip-192-168-45-96.us-west-2.compute.internal
Roles:              <none>
Labels:             alpha.eksctl.io/cluster-name=cni-rc-test
                    alpha.eksctl.io/nodegroup-name=nodegroup
                    beta.kubernetes.io/arch=amd64
                    beta.kubernetes.io/instance-type=m5.large
                    beta.kubernetes.io/os=linux
                    eks.amazonaws.com/capacityType=ON_DEMAND
                    **eks.amazonaws.com/nodegroup=nodegroup**
                    eks.amazonaws.com/nodegroup-image=ami-0b214cc768d59fa03
.....
.....
.....
```

<em>Recommended to run tests from individual files by adding focus instead of running tests from all files at once</em>.  
For instance, you could run all tests from host_networking_test.go first, followed by pod_traffic_test_PD_enabled.go and so on.

```
cd test/integration-new/cni
Added Focus for all specs in host_networking_test.go

ginkgo -v -r -- --cluster-kubeconfig=<kubeconfig> --cluster-name=<cluster-name> --aws-region=<region> --aws-vpc-id=<vpc_id> --ng-name-label-key=eks.amazonaws.com/nodegroup --ng-name-label-val=nodegroup

Running Suite: CNI Pod Networking Suite
=======================================
Random Seed: 1643401684
Will run 3 of 15 specs (Running only tests from host_networking_test.go)
``` 

For pod_traffic_test you need to have atleast 2 pods running on primary and seconday ENI of the node being tested. So we already schedule pods to the max limit of the Node.

**ipamd tests**  
Delete coredns pods if your test fails, as those pods may be using secondary ENI and it would cause problems if you are testing MAX_ENI as 1. 

Ensure that you do not have any Trunk ENI, you might not be running any SG pods but this ENI won't get detached. You will have to manually remove it

**ipv6 tests**  
This test requires to be run on an IPv6 Cluster. You can create one using this [guide](https://docs.aws.amazon.com/eks/latest/userguide/cni-ipv6.html#deploy-ipv6-cluster)

Ensure that you pass 'ng-name-label-key' and 'ng-name-label-val' to trigger ginkgo tests, similar to what was described for cni tests

**metrics-helper tests**  
Ensure that you dont have any existing clusterrole,clusterrolebinding or serviceaccount with following name 'cni-metrics-helper' in your cluster. Else the test will fail to install cni-metrics-helper via helm

```
Unexpected error:
      <*errors.withStack | 0xc0020c4390>: {
          error: <*errors.withMessage | 0xc0020c1600>{
              cause: <*errors.errorString | 0xc0012875f0>{
                  s: "ClusterRole \"cni-metrics-helper\" in namespace \"\" exists and cannot be imported into the current release: invalid ownership metadata; label validation error: missing key \"app.kubernetes.io/managed-by\": must be set to \"Helm\"; annotation validation error: missing key \"meta.helm.sh/release-name\": must be set to \"cni-metrics-helper\"; annotation validation error: missing key \"meta.helm.sh/release-namespace\": must be set to \"kube-system\"",
              },
              msg: "rendered manifests contain a resource that already exists. Unable to continue with install",
          },
          stack: [0x21cd848, 0x21d45b8, 0x21d4b0a, 0x2775ba6, 0x1c2215a, 0x1c21b25, 0x1c22686, 0x1c297a4, 0x1c29353, 0x1c2b5f2, 0x1c2d2a5, 0x1c2cf6b, 0x2774d45, 0x110bba2, 0x106abe1],
      }
      rendered manifests contain a resource that already exists. Unable to continue with install: ClusterRole "cni-metrics-helper" in namespace "" exists and cannot be imported into the current release: invalid ownership metadata; label validation error: missing key "app.kubernetes.io/managed-by": must be set to "Helm"; annotation validation error: missing key "meta.helm.sh/release-name": must be set to "cni-metrics-helper"; annotation validation error: missing key "meta.helm.sh/release-namespace": must be set to "kube-system"
  occurred
```


