# CNI METRICS HELPER

This chart provides a Kubernetes deployment for the Amazon VPC CNI Metrics Helper, which is used to collect metrics for the Amazon VPC CNI plugin for Kubernetes.

## Prerequisites

- Kubernetes 1.11+ running on AWS
- Helm 3.0+

## Installing the Chart

First add the EKS repository to Helm:

```shell
$ helm repo add eks https://aws.github.io/eks-charts
```

Ensure helm repository up to date

```shell
$ helm repo update eks
```

To identify the version you are going to apply

```shell
$ helm search repo eks/cni-metrics-helper --versions
```

To install the latest chart with the release name `cni-metrics-helper` and default configuration:

```shell
$ helm install cni-metrics-helper --namespace kube-system eks/cni-metrics-helper
```

To install manually, clone the Amazon VPC CNI for Kubernetes repository to your local machine:

```shell
$ git clone https://github.com/aws/amazon-vpc-cni-k8s.git
```

Use the helm install command to install the chart into your Kubernetes cluster:

```shell
$ helm install cni-metrics-helper --namespace kube-system ./charts/cni-metrics-helper
```

To uninstall:

```shell
$ helm uninstall cni-metrics-helper --namespace kube-system
```

## Configuration

The following table lists the configurable parameters for this chart and their default values.


| Parameter                      | Description                                                   | Default                             |
| -------------------------------|---------------------------------------------------------------|-------------------------------------|
| `affinity`                     | Map of node/pod affinities                                    | `{}`                                |
| `fullnameOverride`             | Override the fullname of the chart                            | `cni-metrics-helper`                |
| `image.tag`                    | Image tag                                                     | `v1.20.1`                           |
| `image.domain`                 | ECR repository domain                                         | `amazonaws.com`                     |
| `image.region`                 | ECR repository region to use. Should match your cluster       | `us-west-2`                         |
| `image.account`                | ECR repository account number                                 | `602401143452`                      |
| `env.USE_CLOUDWATCH`           | Whether to export CNI metrics to CloudWatch                   | `true`                              |
| `env.USE_PROMETHEUS`           | Whether to export CNI metrics to Prometheus                   | `false`                             |
| `env.AWS_CLUSTER_ID`           | ID of the cluster to use when exporting metrics to CloudWatch | `default`                           |
| `env.AWS_VPC_K8S_CNI_LOGLEVEL` | Log verbosity level (ie. FATAL, ERROR, WARN, INFO, DEBUG)     | `INFO`                              |
| `env.METRIC_UPDATE_INTERVAL`   | Interval at which to update CloudWatch metrics, in seconds.   |                                     |
|                                | Metrics are published to CloudWatch at 2x the interval        | `30`                                |
| `serviceAccount.name`          | The name of the ServiceAccount to use                         | `nil`                               |
| `serviceAccount.create`        | Specifies whether a ServiceAccount should be created          | `true`                              |
| `serviceAccount.annotations`   | Specifies the annotations for ServiceAccount                  | `{}`                                |
| `podAnnotations`               | Specifies the annotations for pods                            | `{}`                                |
| `revisionHistoryLimit`         | The number of revisions to keep                               | `10`                                |
| `podSecurityContext`           | SecurityContext to set on the pod                             | `{}`                                |
| `containerSecurityContext`     | SecurityContext to set on the container                       | `{}`                                |
| `tolerations`                  | Optional deployment tolerations                               | `[]`                                |
| `updateStrategy`               | Optional update strategy                                      | `{}`                                |
| `imagePullSecrets`             | Docker registry pull secret                                   | `[]`                                |
| `nodeSelector`                 | Node labels for pod assignment                                | `{}`                                |
| `tolerations`                  | Optional deployment tolerations                               | `[]`                                |


Specify each parameter using the `--set key=value[,key=value]` argument to `helm install` or provide a YAML file containing the values for the above parameters:

```shell
$ helm install cni-metrics-handler --namespace kube-system eks/cni-metrics-handler --values values.yaml
```

Manual install:
```shell
$ helm install cni-metrics-helper --namespace kube-system ./charts/cni-metrics-helper --values values.yaml
```

## Resources

| Parameter                 | Description                                    | Default |
|---------------------------|------------------------------------------------|---------|
|    resources              | Resources for the pods.                        |   `{}`  |

For example, to set a CPU limit of 200m and a memory limit of 256Mi for the cni-metrics-helper pods, you can use the following command:

```shell
$ helm install cni-metrics-helper ./charts/cni-metrics-helper --namespace kube-system --set resources.limits.cpu=200m,resources.limits.memory=256Mi
```
