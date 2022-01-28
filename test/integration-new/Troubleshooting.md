## Troubleshooting Test Failures

To make debugging efforts easier, you can do the following:

1. Adding focus to individual failing tests to selectively run them in case of troubleshooting. The most straightforward way would be to add an F in front of your It for the failing test. Another simple way would be to add --focus=<focus_value> in ginkgo command where the <focus_value> provided would be matched to the string description of the tests.
2. Comment out the AfterEach or AfterSuite section which cleans up your test resources, rerun the failing test in focussed mode and inspect for any errors using pod logs or pod events
3. If a test fails, resource cleanup might not happen. To recover you can just delete the test namespace

```
kubectl delete ns cni-automation
```

### Few other things to check if you run into Failures
**cni tests**  
Ensure that you pass 'ng-name-label-key' and 'ng-name-label-val' to trigger ginkgo tests

```
ginkgo -v -r -- --cluster-kubeconfig=<kubeconfig> --cluster-name=<cluster-name> --aws-region=<region> --aws-vpc-id=<vpc_id> --ng-name-label-key=eks.amazonaws.com/nodegroup --ng-name-label-val=nodegroup
``` 
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

For pod_traffic_test you need to have atleast 2 pods running on primary and seconday ENI of the node being tested. So we already schedule pods to the max limit of the Node. It's okay if some of the pods are stuck in Container Creating. That shouldn't cause any test failures

**ipamd tests**  
Delete coredns pods if your test fails, as those pods may be using secondary ENI and it would cause problems if you are testing MAX_ENI as 1. 

Ensure that you do not have any Trunk ENI, you might not be running any SG pods but this ENI won't get detached. You will have to manually remove it

**ipv6 tests**  
This test requires to be run on an IPv6 Cluster. You can create one using this [guide](https://docs.aws.amazon.com/eks/latest/userguide/cni-ipv6.html#deploy-ipv6-cluster)

Ensure that you pass 'ng-name-label-key' and 'ng-name-label-val' to trigger ginkgo tests, similar to what was described for cni tests





