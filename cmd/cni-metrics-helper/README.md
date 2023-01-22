# cni-metrics-helper

The `cni-metrics-helper` is a tool that can be used to scrape ENI and IP information, aggregate it on a cluster 
level and publish the metrics to CloudWatch. The following IAM permission is required by the worker nodes to 
publish metrics:
```
"cloudwatch:PutMetricData"
```

By default ipamd will publish prometheus metrics on `:61678/metrics`.

The following diagram shows how `cni-metrics-helper` works in a cluster:

![](../../docs/images/cni-metrics-helper.png)

As you can see in the diagram, the `cni-metrics-helper` connects to the API Server
over https (`tcp/443`), and another connection is created from the API Server to the worker node over http (tcp/61678). If you deploy Amazon EKS with recommended [Restricting cluster traffic](https://docs.aws.amazon.com/eks/latest/userguide/sec-group-reqs.html#security-group-restricting-cluster-traffic) then make sure that a security group is in place that allows the inbound connection from the API Server to the Worker Nodes over `tcp/61678`.

### Using IRSA
As per [AWS EKS Security Best Practice](https://docs.aws.amazon.com/eks/latest/userguide/best-practices-security.html), if you are using IRSA for pods then following requirements must be satisfied to succesfully publish metrics to CloudWatch

1. The IAM Role for your SA [(IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) must have following policy attached

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "cloudwatch:PutMetricData"
            ],
            "Resource": "*"
        }
    ]
}
```

2. You should have similar ClusterRole and ClusterRoleBinding for the IRSA 

``` 
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cni-metrics-helper
rules:
  - apiGroups: [""]
    resources:
      - pods
      - pods/proxy
    verbs: ["get", "watch", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cni-metrics-helper
  labels:
    app.kubernetes.io/name: cni-metrics-helper
    app.kubernetes.io/instance: cni-metrics-helper
    app.kubernetes.io/version: "v1.10.2"
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cni-metrics-helper
subjects:
  - kind: ServiceAccount
    name: <IRSA name>
    namespace: kube-system
```

3. Specify the IRSA name in the cni-metrics-helper deployment spec alongwith the AWS_CLUSTER_ID (as described below). The value that you specify here will show up under the dimension 'CLUSTER_ID' for your published metrics. Specifying value for this field is mandatory only if you are blocking IMDS access  

#### `AWS_CLUSTER_ID`

Type: String

Default: `""`

An Identifier for your Cluster which will be used as the dimension for published metrics. Ideally it should be ClusterName or ClusterID.

```
kind: Deployment
apiVersion: apps/v1
metadata:
  name: cni-metrics-helper
  namespace: kube-system
  labels:
    k8s-app: cni-metrics-helper
spec:
  selector:
    matchLabels:
      k8s-app: cni-metrics-helper
  template:
    metadata:
      labels:
        k8s-app: cni-metrics-helper
    spec:
      containers:
      - env:
        - name: USE_CLOUDWATCH
          value: "true"
        - name: AWS_CLUSTER_ID
          value: ""  
        name: cni-metrics-helper
        image: <image>
      serviceAccountName: <IRSA name>
```
With IRSA, the above deployment spec will be auto-injected with AWS_REGION parameter and it will be used to fetch Region information when we publish metrics. 
Possible Scenarios for above configuration
1. If you are not using IRSA, then Region and CLUSTER_ID information will be fetched using IMDS (should have access)
2. If you are using IRSA but have not specified AWS_CLUSTER_ID, we will fetch the value for CLUSTER_ID if IMDS access is not blocked
3. If you have blocked IMDS access, then you must specify a value for AWS_CLUSTER_ID in the deployment spec
4. If you have not blocked IMDS access but have specified AWS_CLUSTER_ID value, then this value will be used. 

### Installing the cni-metrics-helper
```
kubectl apply -f v1.6/cni-metrics-helper.yaml
```

Adding the CNI metrics helper will publish the following metrics to CloudWatch:
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
"podENIErr",
"reconcileCount",
"totalIPAddresses",
"totalIPv4Prefixes",
"totalAssignedIPv4sPerCidr"
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
