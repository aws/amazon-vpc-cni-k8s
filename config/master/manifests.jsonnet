local objectValues(obj) = [obj[k] for k in std.objectFields(obj)];
local objectItems(obj) = [[k, obj[k]] for k in std.objectFields(obj)];

local regions = {
  default: {
    version:: "v1.10.1", // or eg "v1.6.2"
    ecrRegion:: "us-west-2",
    ecrAccount:: "602401143452",
    ecrDomain:: "amazonaws.com",
    ecrRepo:: "%s.dkr.ecr.%s.%s" % [self.ecrAccount, self.ecrRegion, self.ecrDomain],
  },

  "us-gov-east-1": self.default {
    ecrRegion: "us-gov-east-1",
    ecrAccount: "151742754352",
  },

  "us-gov-west-1": self.default {
    ecrRegion: "us-gov-west-1",
    ecrAccount: "013241004608",
  },

  "cn": self.default {
    ecrRegion: "cn-northwest-1",
    ecrAccount: "961992271922",
    ecrDomain: "amazonaws.com.cn",
  },
};

local awsnode = {
  clusterRole: {
    apiVersion: "rbac.authorization.k8s.io/v1",
    kind: "ClusterRole",
    metadata: {name: "aws-node"},
    rules: [
      {
        apiGroups: ["crd.k8s.amazonaws.com"],
        resources: ["eniconfigs"],
        verbs: ["get", "list", "watch"],
      },
      {
        apiGroups: [""],
        resources: ["namespaces"],
        verbs: ["list", "watch", "get"],
      },
      {
        apiGroups: [""],
        resources: ["pods"],
        verbs: ["list", "watch", "get", "patch"],
      },
      {
        apiGroups: [""],
        resources: ["nodes"],
        verbs: ["list", "watch", "get", "update"],
      },
      {
        apiGroups: ["extensions"],
        resources: ["*"],
        verbs: ["list", "watch"],
      }
    ],
  },

  account: {
    apiVersion: "v1",
    kind: "ServiceAccount",
    metadata: {
      name: "aws-node",
      namespace: "kube-system",
    },
  },

  binding: {
    apiVersion: "rbac.authorization.k8s.io/v1",
    kind: "ClusterRoleBinding",
    metadata: {
      name: "aws-node",
    },
    roleRef: {
      apiGroup: "rbac.authorization.k8s.io",
      kind: $.clusterRole.kind,
      name: $.clusterRole.metadata.name,
    },
    subjects: [{
      kind: $.account.kind,
      name: $.account.metadata.name,
      namespace: $.account.metadata.namespace,
    }],
  },

  daemonset: {
    kind: "DaemonSet",
    apiVersion: "apps/v1",
    metadata: {
      name: "aws-node",
      namespace: "kube-system",
      labels: {
        "k8s-app": "aws-node",
      },
    },
    spec: {
      local spec = self,
      updateStrategy: {
        type: "RollingUpdate",
        rollingUpdate: {maxUnavailable: "10%"},
      },
      selector: {
        matchLabels: spec.template.metadata.labels,
      },
      template: {
        metadata: {
          labels: {
            "k8s-app": "aws-node",
          },
        },
        spec: {
          priorityClassName: "system-node-critical",
          terminationGracePeriodSeconds: 10,
          affinity: {
            nodeAffinity: {
              requiredDuringSchedulingIgnoredDuringExecution: {
                nodeSelectorTerms: [{ 
                  matchExpressions: [
                    {
                      key: "kubernetes.io/os",
                      operator: "In",
                      values: ["linux"],
                    },
                    {
                      key: "kubernetes.io/arch",
                      operator: "In",
                      values: ["amd64", "arm64"],
                    },
                    {
                      key: "eks.amazonaws.com/compute-type",
                      operator: "NotIn",
                      values: ["fargate"],
                    },
                  ],
                }],
              },
            },
          },
          serviceAccountName: $.account.metadata.name,
          hostNetwork: true,
          tolerations: [{operator: "Exists"}],
          containers_:: {
            awsnode: {
              image: "%s/amazon-k8s-cni:%s" % [$.ecrRepo, $.version],
              ports: [{
                containerPort: 61678,
                name: "metrics",
                protocol: "TCP",
              }],
              name: "aws-node",
              readinessProbe: {
                exec: {
                  command: ["/app/grpc-health-probe", "-addr=:50051", "-connect-timeout=5s", "-rpc-timeout=5s"],
                },
                initialDelaySeconds: 1,
                timeoutSeconds: 10,
              },
              livenessProbe: self.readinessProbe + {
                initialDelaySeconds: 60,
              },
              env_:: {
                ADDITIONAL_ENI_TAGS: "{}",
                AWS_VPC_CNI_NODE_PORT_SUPPORT: "true",
                AWS_VPC_ENI_MTU: "9001",
                AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER: "false",
                AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG: "false",
                AWS_VPC_K8S_CNI_EXTERNALSNAT: "false",
                AWS_VPC_K8S_CNI_LOGLEVEL: "DEBUG",
                AWS_VPC_K8S_CNI_LOG_FILE: "/host/var/log/aws-routed-eni/ipamd.log",
                AWS_VPC_K8S_CNI_RANDOMIZESNAT: "prng",
                AWS_VPC_K8S_CNI_VETHPREFIX: "eni",
                AWS_VPC_K8S_PLUGIN_LOG_FILE: "/var/log/aws-routed-eni/plugin.log",
                AWS_VPC_K8S_PLUGIN_LOG_LEVEL: "DEBUG",
                DISABLE_INTROSPECTION: "false",
                DISABLE_METRICS: "false",
                ENABLE_POD_ENI: "false",
                ENABLE_IPv4: "true",
                ENABLE_IPv6: "false",
                ENABLE_PREFIX_DELEGATION: "false",
                DISABLE_NETWORK_RESOURCE_PROVISIONING: "false",
                MY_NODE_NAME: {
                  valueFrom: {
                    fieldRef: {fieldPath: "spec.nodeName"},
                  },
                },
                WARM_ENI_TARGET: "1",
                WARM_PREFIX_TARGET: "1",
              },
              env: [
                {name: kv[0]} + if std.isObject(kv[1]) then kv[1] else {value: kv[1]}
                for kv in objectItems(self.env_)
              ],
              resources: {
                requests: {cpu: "25m"},
              },
              securityContext: {
                capabilities: {add: ["NET_ADMIN"]},
              },
              volumeMounts: [
                {mountPath: "/host/opt/cni/bin", name: "cni-bin-dir"},
                {mountPath: "/host/etc/cni/net.d", name: "cni-net-dir"},
                {mountPath: "/host/var/log/aws-routed-eni", name: "log-dir"},
                {mountPath: "/var/run/aws-node", name: "run-dir"},
                {mountPath: "/var/run/dockershim.sock", name: "dockershim"},
                {mountPath: "/run/xtables.lock", name: "xtables-lock"},
              ],
            },
          },
          containers: objectValues(self.containers_),
          volumes: [
            {name: "cni-bin-dir", hostPath: {path: "/opt/cni/bin"}},
            {name: "cni-net-dir", hostPath: {path: "/etc/cni/net.d"}},
            {name: "dockershim", hostPath: {path: "/var/run/dockershim.sock"}},
            {name: "xtables-lock", hostPath: {path: "/run/xtables.lock"}},
            {name: "log-dir",
              hostPath: {
                path: "/var/log/aws-routed-eni",
                type: "DirectoryOrCreate",
              },
            },
            {name: "run-dir",
              hostPath: {
                path: "/var/run/aws-node",
                type: "DirectoryOrCreate",
              },
            },
          ],
          initContainers: [
            {
              name: "aws-vpc-cni-init",
              image: "%s/amazon-k8s-cni-init:%s" % [$.ecrRepo, $.version],
              securityContext: {privileged: true},
              env_:: {
                DISABLE_TCP_EARLY_DEMUX: "false",
                ENABLE_IPv6: "false",
              },
              env: [
                {name: kv[0]} + if std.isObject(kv[1]) then kv[1] else {value: kv[1]}
                for kv in objectItems(self.env_)
               ],
              resources: {
                requests: {cpu: "10m", memory: "32Mi"},
                limits: {cpu: "50m", memory: "64Mi"},
              },
              volumeMounts: [
                {mountPath: "/host/opt/cni/bin", name: "cni-bin-dir"},
              ],
            },
          ],
        },
      },
    },
  },

  crd: {
    apiVersion: "apiextensions.k8s.io/v1",
    kind: "CustomResourceDefinition",
    metadata: {
      name: "eniconfigs.crd.k8s.amazonaws.com",
    },
    spec: {
      scope: "Cluster",
      group: "crd.k8s.amazonaws.com",
      preserveUnknownFields: false,
      versions: [{
        name: "v1alpha1",
        served: true,
        storage: true,
        schema: {
            openAPIV3Schema: {
              type: "object",
              "x-kubernetes-preserve-unknown-fields": true,
            }},
      }],
      names: {
        plural: "eniconfigs",
        singular: "eniconfig",
        kind: "ENIConfig",
      },
    },
  },
};

local metricsHelper = {
  clusterRole: {
    apiVersion: "rbac.authorization.k8s.io/v1",
    kind: "ClusterRole",
    metadata: {
      name: "cni-metrics-helper",
    },
    rules: [
      {
        apiGroups: [""],
        resources: [
          "pods",
          "pods/proxy"
        ],
        verbs: ["list", "watch", "get"],
      },
    ],
  },

  account: {
    apiVersion: "v1",
    kind: "ServiceAccount",
    metadata: {
      name: "cni-metrics-helper",
      namespace: "kube-system",
    },
  },

  binding: {
    apiVersion: "rbac.authorization.k8s.io/v1",
    kind: "ClusterRoleBinding",
    metadata: {
      name: "cni-metrics-helper",
    },
    roleRef: {
      apiGroup: "rbac.authorization.k8s.io",
      kind: $.clusterRole.kind,
      name: $.clusterRole.metadata.name,
    },
    subjects: [{
      kind: $.account.kind,
      name: $.account.metadata.name,
      namespace: $.account.metadata.namespace,
    }],
  },

  deployment: {
    apiVersion: "apps/v1",
    kind: "Deployment",
    metadata: {
      name: "cni-metrics-helper",
      namespace: "kube-system",
      labels: {
        "k8s-app": "cni-metrics-helper",
      },
    },
    spec: {
      local spec = self,
      selector: {
        matchLabels: spec.template.metadata.labels,
      },
      template: {
        metadata: {
          labels: {
            "k8s-app": "cni-metrics-helper",
          },
        },
        spec: {
          serviceAccountName: $.account.metadata.name,
          containers_:: {
            metricshelper: {
              image: "%s/cni-metrics-helper:%s" % [$.ecrRepo, $.version],
              name: "cni-metrics-helper",
              env_:: {
                USE_CLOUDWATCH: "true",
                AWS_CLUSTER_ID: "",
              },
              env: [
                {name: kv[0]} + if std.isObject(kv[1]) then kv[1] else {value: kv[1]}
                for kv in objectItems(self.env_)
              ],
            },
          },
          containers: objectValues(self.containers_),
        },
      },
    },
  },
};

local byRegion(basename, template) = {
  [
    basename + (if kv[0] == "default" then "" else "-" + kv[0])
  ]: template + kv[1]
  for kv in objectItems(regions)
};

// Output values, as jsonnet objects
local output =
byRegion("aws-k8s-cni", awsnode) +
byRegion("cni-metrics-helper", metricsHelper);

// Yaml-ified output values
{
  [kv[0] + ".yaml"]: std.manifestYamlStream(objectValues(kv[1]))
  for kv in objectItems(output)
}
