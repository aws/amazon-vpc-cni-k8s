# cni-metrics-helper

The `cni-metrics-helper` is a tool that can be used to scrape ENI and IP information, aggregate it on a cluster 
level and publish the metrics to CloudWatch. The following IAM permission is required by the worker nodes to 
publish metrics:
```
"cloudwatch:PutMetricData"
```

By default ipamd will publish prometheus metrics on `:61678/metrics`.

The following diagram shows how `cni-metrics-helper` works in a cluster:

![](../docs/images/cni-metrics-helper.png)

### Installing the cni-metrics-helper
```
kubectl apply -f v1.5/cni-metrics-helper.yaml
```

Adding this will publish the following metrics to CloudWatch:
```
"addReqCount",
"assignIPAddresses",
"awsAPIErr",
"awsAPILatency",
"awsUtilErr",
"delReqCount",
"eniAllocated",
"eniMaxAvailable",
"ipamdActionInProgress",
"ipamdErr",
"maxIPAddresses",
"reconcileCount",
"totalIPAddresses",
```

### Get cni-metrics-helper logs

```
kubectl get pod -n kube-system
NAME                                  READY     STATUS    RESTARTS   AGE
aws-node-248ns                        1/1       Running   0          6h
aws-node-257bn                        1/1       Running   0          2h
...
cni-metrics-helper-6dcff5ddf4-v5l6d   1/1       Running   0          7h
kube-dns-75fddcb66f-48tzn             3/3       Running   0          1d
```

```
kubectl logs cni-metrics-helper-6dcff5ddf4-v5l6d -n kube-system
```
### cni-metrics-helper key log messages

Example of some aggregated metrics
```
I0516 17:11:58.489648       7 metrics.go:350] Produce GAUGE metrics: ipamdActionInProgress, value: 0.000000
I0516 17:11:58.489657       7 metrics.go:350] Produce GAUGE metrics: assignIPAddresses, value: 2.000000
I0516 17:11:58.489665       7 metrics.go:350] Produce GAUGE metrics: totalIPAddresses, value: 11186.000000
I0516 17:11:58.489674       7 metrics.go:350] Produce GAUGE metrics: eniMaxAvailable, value: 800.000000
I0516 17:11:58.489685       7 metrics.go:340] Produce COUNTER metrics: ipamdErr, value: 1.000000
I0516 17:11:58.489695       7 metrics.go:350] Produce GAUGE metrics: eniAllocated, value: 799.000000
I0516 17:11:58.489715       7 metrics.go:350] Produce GAUGE metrics: maxIPAddresses, value: 11200.000000
```

## How to build

In the base folder of the project:
```
make docker-metrics
```

### To run tests
```
make docker-metrics-test
```
