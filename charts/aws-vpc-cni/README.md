# AWS VPC CNI

This chart installs the AWS CNI Daemonset: https://github.com/aws/amazon-vpc-cni-k8s

## Prerequisites

- Kubernetes 1.11+ running on AWS
- Helm v3

## Installing the Chart

First add the EKS repository to Helm:

```shell
helm repo add eks https://aws.github.io/eks-charts
```

To install the chart with the release name `aws-vpc-cni` and default configuration:

```shell
$ helm install aws-vpc-cni --namespace kube-system eks/aws-vpc-cni
```

To install into an EKS cluster where the CNI is already installed, see [this section below](#adopting-the-existing-aws-node-resources-in-an-eks-cluster)

To migrate helm release for aws-vpc-cni chart from v2 to v3, see [ Migrate from helm v2 to helm v3 ](#migrate-from-helm-v2-to-helm-v3)

## Configuration

The following table lists the configurable parameters for this chart and their default values.

| Parameter               | Description                                             | Default                             |
| ------------------------|---------------------------------------------------------|-------------------------------------|
| `affinity`              | Map of node/pod affinities                              | `{}`                                |
| `cniConfig.enabled`     | Enable overriding the default 10-aws.conflist file      | `false`                             |
| `cniConfig.fileContents`| The contents of the custom cni config file              | `nil`                               |
| `eniConfig.create`      | Specifies whether to create ENIConfig resource(s)       | `false`                             |
| `eniConfig.region`      | Region to use when generating ENIConfig resource names  | `us-west-2`                         |
| `eniConfig.subnets`     | A map of AZ identifiers to config per AZ                | `nil`                               |
| `eniConfig.subnets.id`  | The ID of the subnet within the AZ which will be used in the ENIConfig | `nil`                |
| `eniConfig.subnets.securityGroups`  | The IDs of the security groups which will be used in the ENIConfig | `nil`        |
| `env`                   | List of environment variables. See [here](https://github.com/aws/amazon-vpc-cni-k8s#cni-configuration-variables) for options | (see `values.yaml`) |
| `enableWindowsIpam`     | Enable windows support for your cluster                 | `false`                             |
| `enableNetworkPolicy`   | Enable Network Policy Controller and Agent for your cluster | `false`                         |
| `fullnameOverride`      | Override the fullname of the chart                      | `aws-node`                          |
| `image.tag`             | Image tag                                               | `v1.15.1`                           |
| `image.domain`          | ECR repository domain                                   | `amazonaws.com`                     |
| `image.region`          | ECR repository region to use. Should match your cluster | `us-west-2`                         |
| `image.endpoint`        | ECR repository endpoint to use.                         | `ecr`                               |
| `image.account`         | ECR repository account number                           | `602401143452`                      |
| `image.pullPolicy`      | Container pull policy                                   | `IfNotPresent`                      |
| `image.override`        | A custom docker image to use                            | `nil`                               |
| `imagePullSecrets`      | Docker registry pull secret                             | `[]`                                |
| `init.image.tag`        | Image tag                                               | `v1.15.1`                           |
| `init.image.domain`     | ECR repository domain                                   | `amazonaws.com`                     |
| `init.image.region`     | ECR repository region to use. Should match your cluster | `us-west-2`                         |
| `init.image.endpoint`   | ECR repository endpoint to use.                         | `ecr`                               |
| `init.image.account`    | ECR repository account number                           | `602401143452`                      |
| `init.image.pullPolicy` | Container pull policy                                   | `IfNotPresent`                      |
| `init.image.override`   | A custom docker image to use                            | `nil`                               |
| `init.env`              | List of init container environment variables. See [here](https://github.com/aws/amazon-vpc-cni-k8s#cni-configuration-variables) for options | (see `values.yaml`) |
| `init.securityContext`  | Init container Security context                         | `privileged: true`                  |
| `originalMatchLabels`   | Use the original daemonset matchLabels                  | `false`                             |
| `nameOverride`          | Override the name of the chart                          | `aws-node`                          |
| `nodeAgent.image.tag`   | Image tag for Node Agent                                | `v1.0.4`                            |
| `nodeAgent.image.domain`| ECR repository domain                                   | `amazonaws.com`                     |
| `nodeAgent.image.region`| ECR repository region to use. Should match your cluster | `us-west-2`                         |
| `nodeAgent.image.endpoint`   | ECR repository endpoint to use.                    | `ecr`                               |
| `nodeAgent.image.account`    | ECR repository account number                      | `602401143452`                      |
| `nodeAgent.image.pullPolicy` | Container pull policy                              | `IfNotPresent`                      |
| `nodeAgent.securityContext`  | Node Agent container Security context              | `capabilities: add: - "NET_ADMIN" privileged: true` |
| `nodeAgent.enableCloudWatchLogs`  | Enable CW logging for Node Agent              | `false`                             |
| `nodeAgent.enablePolicyEventLogs` | Enable policy decision logs for Node Agent    | `false`                             |
| `nodeAgent.metricsBindAddr` | Node Agent port for metrics                         | `8162`                              |
| `nodeAgent.healthProbeBindAddr` | Node Agent port for health probes               | `8163`                              |
| `nodeAgent.enableIpv6`  | Enable IPv6 support for Node Agent                      | `false`                             |
| `extraVolumes`          | Array to add extra volumes                              | `[]`                                |
| `extraVolumeMounts`     | Array to add extra mount                                | `[]`                                |
| `nodeSelector`          | Node labels for pod assignment                          | `{}`                                |
| `podSecurityContext`    | Pod Security Context                                    | `{}`                                |
| `podAnnotations`        | annotations to add to each pod                          | `{}`                                |
| `podLabels`             | Labels to add to each pod                               | `{}`                                |
| `priorityClassName`     | Name of the priorityClass                               | `system-node-critical`              |
| `resources`             | Resources for containers in pod                         | `requests.cpu: 25m`                 |
| `securityContext`       | Container Security context                              | `capabilities: add: - "NET_ADMIN" - "NET_RAW"` |
| `serviceAccount.name`   | The name of the ServiceAccount to use                   | `nil`                               |
| `serviceAccount.create` | Specifies whether a ServiceAccount should be created    | `true`                              |
| `serviceAccount.annotations` | Specifies the annotations for ServiceAccount       | `{}`                                |
| `livenessProbe`         | Livenness probe settings for daemonset                  | (see `values.yaml`)                 |
| `readinessProbe`        | Readiness probe settings for daemonset                  | (see `values.yaml`)                 |
| `tolerations`           | Optional deployment tolerations                         | `[{"operator": "Exists"}]`          |
| `updateStrategy`        | Optional update strategy                                | `type: RollingUpdate`               |

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install` or provide a YAML file containing the values for the above parameters:

```shell
$ helm install aws-vpc-cni --namespace kube-system eks/aws-vpc-cni --values values.yaml
```

## Adopting the existing aws-node resources in an EKS cluster

If you do not want to delete the existing aws-node resources in your cluster that run the aws-vpc-cni and then install this helm chart, you can adopt the resources into a release instead. Refer to the script below to import existing resources into helm. Once you have annotated and labeled all the resources this chart specifies, enable the `originalMatchLabels` flag. If you have been careful this should not diff and leave all the resources unmodified and now under management of helm.

WARNING: Substitute YOUR_HELM_RELEASE_NAME_HERE with the name of your helm release.
```
#!/usr/bin/env bash

set -euo pipefail

for kind in daemonSet clusterRole clusterRoleBinding serviceAccount; do
  echo "setting annotations and labels on $kind/aws-node"
  kubectl -n kube-system annotate --overwrite $kind aws-node meta.helm.sh/release-name=YOUR_HELM_RELEASE_NAME_HERE
  kubectl -n kube-system annotate --overwrite $kind aws-node meta.helm.sh/release-namespace=kube-system
  kubectl -n kube-system label --overwrite $kind aws-node app.kubernetes.io/managed-by=Helm
done
```

## Migrate from Helm v2 to Helm v3
You can use the [Helm 2to3 plugin](https://github.com/helm/helm-2to3) to migrate releases from Helm v2 to Helm v3. For a more detailed explanation with some examples about this migration plugin, refer to Helm blog post: [How to migrate from Helm v2 to Helm v3](https://helm.sh/blog/migrate-from-helm-v2-to-helm-v3/).
