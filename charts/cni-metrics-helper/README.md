# CNI METRICS HELPER

This chart provides a Kubernetes deployment for the Amazon VPC CNI Metrics Helper, which is used to collect metrics for the Amazon VPC CNI plugin for Kubernetes.

## Prerequisites

- Kubernetes 1.11+ running on AWS
- Helm 3.0+

## Installing the Chart

Clone the Amazon VPC CNI for Kubernetes repository to your local machine.

```shell
$ git clone https://github.com/aws/amazon-vpc-cni-k8s.git
```
Use the helm install command to install the chart into your Kubernetes cluster

```shell
$ helm install cni-metrics-helper ./amazon-vpc-cni-k8s/charts/cni-metrics-helper
```

## Configuration

The following table lists the configurable parameters for this chart and their default values.

| Parameter                  | Description                                                   | Default            |
|----------------------------|---------------------------------------------------------------|--------------------|
| fullnameOverride           | Override the fullname of the chart                            | cni-metrics-helper |
| image.region               | ECR repository region to use. Should match your cluster       | us-west-2          |
| image.tag                  | Image tag                                                     | v1.11.4            |
| image.account              | ECR repository account number                                 | 602401143452       |
| image.domain               | ECR repository domain                                         | amazonaws.com      |
| env.USE_CLOUDWATCH         | Whether to export CNI metrics to CloudWatch                   | true               |
| env.AWS_CLUSTER_ID         | ID of the cluster to use when exporting metrics to CloudWatch | default            |
| metricUpdateInterval       | Interval at which to update CloudWatch metrics, in seconds    | 60                 |
| serviceAccount.name        | The name of the ServiceAccount to use                         | nil                |
| serviceAccount.create      | Specifies whether a ServiceAccount should be created          | true               |
| serviceAccount.annotations | Specifies the annotations for ServiceAccount                  | {}                 |


Specify each parameter using the `--set key=value[,key=value]` argument to `helm install` or provide a YAML file containing the values for the above parameters:

```shell
$ helm install my-release ./amazon-vpc-cni-k8s/charts/cni-metrics-helper --set useCloudwatch=false --values values.yaml

```

## Resources

You can specify resource limits and requests for the cni-metrics-helper pods by setting the following values:

| Parameter                 | Description                                    | Default |
|---------------------------|------------------------------------------------|---------|
| resources.limits.cpu      | CPU limit for the cni-metrics-helper pods      | 100m    |
| resources.limits.memory   | Memory limit for the cni-metrics-helper pods   | 128Mi   |
| resources.requests.cpu    | CPU request for the cni-metrics-helper pods    | 100m    |
| resources.requests.memory | Memory request for the cni-metrics-helper pods | 128Mi   |

for example, to set a CPU limit of 200m and a memory limit of 256Mi for the cni-metrics-helper pods, you can use the following command:

```shell
$ helm install my-release ./amazon-vpc-cni-k8s/charts/cni-metrics-helper --set resources.limits.cpu=200m,resources.limits.memory=256Mi
```