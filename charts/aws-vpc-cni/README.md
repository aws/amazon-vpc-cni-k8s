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
| `enableWindowsPrefixDelegation` | Enable windows prefix delegation support for your cluster | `false`                   |
| `warmWindowsPrefixTarget` | Warm prefix target value for Windows prefix delegation | `0`                                |
| `warmWindowsIPTarget`   | Warm IP target value for Windows prefix delegation      | `1`                                 |
| `minimumWindowsIPTarget`| Minimum IP target value for Windows prefix delegation   | `3`                                 |
| `branchENICooldown`     | Number of seconds that branch ENIs remain in cooldown   | `60`                                |
| `fullnameOverride`      | Override the fullname of the chart                      | `aws-node`                          |
| `image.tag`             | Image tag                                               | `v1.20.4`                           |
| `image.domain`          | ECR repository domain                                   | `amazonaws.com`                     |
| `image.region`          | ECR repository region to use. Should match your cluster | `us-west-2`                         |
| `image.endpoint`        | ECR repository endpoint to use.                         | `ecr`                               |
| `image.account`         | ECR repository account number                           | `602401143452`                      |
| `image.pullPolicy`      | Container pull policy                                   | `IfNotPresent`                      |
| `image.overrideRepository` | Repository override for the image (does not change the tag) | `nil`                        |
| `image.override`        | A custom docker image to use                            | `nil`                               |
| `imagePullSecrets`      | Docker registry pull secret                             | `[]`                                |
| `init.image.tag`        | Image tag                                               | `v1.20.4`                           |
| `init.image.domain`     | ECR repository domain                                   | `amazonaws.com`                     |
| `init.image.region`     | ECR repository region to use. Should match your cluster | `us-west-2`                         |
| `init.image.endpoint`   | ECR repository endpoint to use.                         | `ecr`                               |
| `init.image.account`    | ECR repository account number                           | `602401143452`                      |
| `init.image.pullPolicy` | Container pull policy                                   | `IfNotPresent`                      |
| `init.image.overrideRepository` | Repository override for the image (does not change the tag) | `nil`                   |
| `init.image.override`   | A custom docker image to use                            | `nil`                               |
| `init.env`              | List of init container environment variables. See [here](https://github.com/aws/amazon-vpc-cni-k8s#cni-configuration-variables) for options | (see `values.yaml`) |
| `init.securityContext`  | Init container Security context                         | `privileged: true`                  |
| `init.resources`        | Init container resources, will defualt to .Values.resources if not set | `{}`                 |
| `originalMatchLabels`   | Use the original daemonset matchLabels                  | `false`                             |
| `nameOverride`          | Override the name of the chart                          | `aws-node`                          |
| `nodeAgent.enabled`     | If the Node Agent container should be created           | `true`                              |
| `nodeAgent.image.tag`   | Image tag for Node Agent                                | `v1.2.7`                            |
| `nodeAgent.image.domain`| ECR repository domain                                   | `amazonaws.com`                     |
| `nodeAgent.image.region`| ECR repository region to use. Should match your cluster | `us-west-2`                         |
| `nodeAgent.image.endpoint`   | ECR repository endpoint to use.                    | `ecr`                               |
| `nodeAgent.image.account`    | ECR repository account number                      | `602401143452`                      |
| `nodeAgent.image.pullPolicy` | Container pull policy                              | `IfNotPresent`                      |
| `nodeAgent.image.overrideRepository` | Repository override for the image (does not change the tag) | `nil`              |
| `nodeAgent.image.override`   | A custom docker image to use                       | `nil`                               |
| `nodeAgent.securityContext`  | Node Agent container Security context              | `capabilities: add: - "NET_ADMIN" privileged: true` |
| `nodeAgent.enableCloudWatchLogs`  | Enable CW logging for Node Agent              | `false`                             |
| `nodeAgent.networkPolicyAgentLogFileLocation`  | Log File location of Network Policy Agent | `/var/log/aws-routed-eni/network-policy-agent.log` |
| `nodeAgent.enablePolicyEventLogs` | Enable policy decision logs for Node Agent    | `false`                             |
| `nodeAgent.metricsBindAddr` | Node Agent port for metrics                         | `8162`                              |
| `nodeAgent.healthProbeBindAddr` | Node Agent port for health probes               | `8163`                              |
| `nodeAgent.conntrackCacheCleanupPeriod` | Cleanup interval for network policy agent conntrack cache | 300               |
| `nodeAgent.enableIpv6`  | Enable IPv6 support for Node Agent                      | `false`                             |
| `nodeAgent.resources`   | Node Agent resources, will defualt to .Values.resources if not set | `{}`                     |
| `nodeAgent.logLevel`    | Node Agent logging verbosity level.                     | `debug`                             |
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

If you do not want to delete the existing aws-node resources in your cluster that run the aws-vpc-cni and then install this helm chart, you can adopt the resources into a release instead. Refer to the script below to import existing resources into helm. Once you have annotated and labeled all the resources this chart specifies, enable the `originalMatchLabels` flag. If you have been careful, this should not diff and leave all the resources unmodified and now under management of helm.

```
#!/usr/bin/env bash

set -euo pipefail

for kind in daemonSet clusterRole clusterRoleBinding serviceAccount; do
  echo "setting annotations and labels on $kind/aws-node"
  kubectl -n kube-system annotate --overwrite $kind aws-node meta.helm.sh/release-name=aws-vpc-cni
  kubectl -n kube-system annotate --overwrite $kind aws-node meta.helm.sh/release-namespace=kube-system
  kubectl -n kube-system label --overwrite $kind aws-node app.kubernetes.io/managed-by=Helm
done

kubectl -n kube-system annotate --overwrite configmap amazon-vpc-cni meta.helm.sh/release-name=aws-vpc-cni
kubectl -n kube-system annotate --overwrite configmap amazon-vpc-cni meta.helm.sh/release-namespace=kube-system
kubectl -n kube-system label --overwrite configmap amazon-vpc-cni app.kubernetes.io/managed-by=Helm

Kubernetes recommends using server-side apply for more control over the field manager. After adopting the chart resources, you can run the following command to apply the chart:
```
helm template aws-vpc-cni --include-crds --namespace kube-system eks/aws-vpc-cni --set originalMatchLabels=true | kubectl apply --server-side --force-conflicts --field-manager Helm -f -
```

## Migrate from Helm v2 to Helm v3
You can use the [Helm 2to3 plugin](https://github.com/helm/helm-2to3) to migrate releases from Helm v2 to Helm v3. For a more detailed explanation with some examples about this migration plugin, refer to Helm blog post: [How to migrate from Helm v2 to Helm v3](https://helm.sh/blog/migrate-from-helm-v2-to-helm-v3/).
