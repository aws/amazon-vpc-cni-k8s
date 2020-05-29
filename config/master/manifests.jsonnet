local objectValues(obj) = [obj[k] for k in std.objectFields(obj)];
local objectItems(obj) = [[k, obj[k]] for k in std.objectFields(obj)];

local regions = {
  default: {
    version:: "latest", // or eg "v1.6.1"
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
        resources: ["*"],
        verbs: ["*"],
      },
      {
        apiGroups: [""],
        resources: ["pods", "nodes", "namespaces"],
        verbs: ["list", "watch", "get"],
      },
      {
        apiGroups: ["extensions"],
        resources: ["daemonsets"],
        verbs: ["list", "watch"],
      },
    ],
  },

  serviceAccount: {
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
      kind: $.serviceAccount.kind,
      name: $.serviceAccount.metadata.name,
      namespace: $.serviceAccount.metadata.namespace,
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
          affinity: {
            nodeAffinity: {
              requiredDuringSchedulingIgnoredDuringExecution: {
                nodeSelectorTerms: [
                  {
                    matchExpressions: [
                      {
                        key: prefix + "kubernetes.io/os",
                        operator: "In",
                        values: ["linux"],
                      },
                      {
                        key: prefix + "kubernetes.io/arch",
                        operator: "In",
                        values: ["amd64"],
                      },
                      {
                        key: "eks.amazonaws.com/compute-type",
                        operator: "NotIn",
                        values: ["fargate"],
                      },
                    ],
                  } for prefix in ["beta.", ""]
                ],
              },
            },
          },
          serviceAccountName: $.serviceAccount.metadata.name,
          hostNetwork: true,
          tolerations: [{operator: "Exists"}],
          containers_:: {
            awsnode: {
              image: "%s/amazon-k8s-cni:%s" % [$.ecrRepo, $.version],
              imagePullPolicy: "Always",
              ports: [{
                containerPort: 61678,
                name: "metrics"
              }],
              name: "aws-node",
              readinessProbe: {
                exec: {
                  command: ["/app/grpc-health-probe", "-addr=:50051"],
                },
                initialDelaySeconds: 35,
              },
              livenessProbe: self.readinessProbe,
              env_:: {
                AWS_VPC_ENI_MTU: "9001",
                AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER: "false",
                AWS_VPC_K8S_CNI_LOGLEVEL: "DEBUG",
                AWS_VPC_K8S_CNI_VETHPREFIX: "eni",
                MY_NODE_NAME: {
                  valueFrom: {
                    fieldRef: {fieldPath: "spec.nodeName"},
                  },
                },
              },
              env: [
                {name: kv[0]} + if std.isObject(kv[1]) then kv[1] else {value: kv[1]}
                for kv in objectItems(self.env_)
              ],
              resources: {
                requests: {cpu: "10m"},
              },
              securityContext: {
                capabilities: {add: ["NET_ADMIN"]},
              },
              volumeMounts: [
                {mountPath: "/host/opt/cni/bin", name: "cni-bin-dir"},
                {mountPath: "/host/etc/cni/net.d", name: "cni-net-dir"},
                {mountPath: "/host/var/log", name: "log-dir"},
                {mountPath: "/var/run/docker.sock", name: "dockersock"},
                {mountPath: "/var/run/dockershim.sock", name: "dockershim"},
              ],
            },
          },
          containers: objectValues(self.containers_),
          volumes: [
            {name: "cni-bin-dir", hostPath: {path: "/opt/cni/bin"}},
            {name: "cni-net-dir", hostPath: {path: "/etc/cni/net.d"}},
            {name: "log-dir", hostPath: {path: "/var/log"}},
            {name: "dockersock", hostPath: {path: "/var/run/docker.sock"}},
            {name: "dockershim", hostPath: {path: "/var/run/dockershim.sock"}},
          ],
          initContainers: [
            {
              name: "aws-vpc-cni-init",
              image: "%s/amazon-k8s-cni-init:%s" % [$.ecrRepo, $.version],
              imagePullPolicy: "Always",
              securityContext: {privileged: true},
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
    apiVersion: "apiextensions.k8s.io/v1beta1",
    kind: "CustomResourceDefinition",
    metadata: {
      name: "eniconfigs.crd.k8s.amazonaws.com",
    },
    spec: {
      scope: "Cluster",
      group: "crd.k8s.amazonaws.com",
      versions: [{
        name: "v1alpha1",
        served: true,
        storage: true,
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
          "nodes",
          "pods",
          "pods/proxy",
          "services",
          "resourcequotas",
          "replicationcontrollers",
          "limitranges",
          "persistentvolumeclaims",
          "persistentvolumes",
          "namespaces",
          "endpoints",
        ],
        verbs: ["list", "watch", "get"],
      },
      {
        apiGroups: ["extensions"],
        resources: ["daemonsets", "deployments", "replicasets"],
        verbs: ["list", "watch"],
      },
      {
        apiGroups: ["apps"],
        resources: ["statefulsets"],
        verbs: ["list", "watch"],
      },
      {
        apiGroups: ["batch"],
        resources: ["cronjobs", "jobs"],
        verbs: ["list", "watch"],
      },
      {
        apiGroups: ["autoscaling"],
        resources: ["horizontalpodautoscalers"],
        verbs: ["list", "watch"],
      },
    ],
  },

  serviceAccount: {
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
      kind: $.serviceAccount.kind,
      name: $.serviceAccount.metadata.name,
      namespace: $.serviceAccount.metadata.namespace,
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
          serviceAccountName: $.serviceAccount.metadata.name,
          containers_:: {
            metricshelper: {
              image: "%s/cni-metrics-helper:%s" % [$.ecrRepo, $.version],
              imagePullPolicy: "Always",
              name: "cni-metrics-helper",
              env_:: {
                USE_CLOUDWATCH: "true",
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
