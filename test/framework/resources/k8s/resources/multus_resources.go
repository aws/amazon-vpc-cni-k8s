package resources

import (
	"context"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"

	apiextensionsV1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MultusResourceManager interface {
	CreateClusterRole() error
	CreateClusterRoleBinding() error
	CreateMultusDaemonset() (appsv1.DaemonSet, error)
	CreateServiceAccount() error
	CreateNetworkAttachmentDefinitionCRD() error
	DeleteClusterRole() error
	DeleteClusterRoleBinding() error
	DeleteServiceAccount() error
	DeleteMultusDaemonset() error
	DeleteNetworkAttachmentDefinitionCRD() error
}

var (
	networkAttachmentDefinition apiextensionsV1.CustomResourceDefinition
	clusterRole                 v1.ClusterRole
	clusterRoleBinding          v1.ClusterRoleBinding
	serviceAccount              corev1.ServiceAccount
	ds                          appsv1.DaemonSet
)

type defaultMultusResourceManager struct {
	k8sClient              client.DelegatingClient
	ApiextensionsClientSet *apiextensionsclient.Clientset
}

func NewDefaultMultusResourceManager(k8sClient client.DelegatingClient, config *rest.Config) MultusResourceManager {
	return &defaultMultusResourceManager{
		k8sClient:              k8sClient,
		ApiextensionsClientSet: apiextensionsclient.NewForConfigOrDie(config),
	}

}

func (d defaultMultusResourceManager) CreateNetworkAttachmentDefinitionCRD() error {
	networkAttachmentDefinition = apiextensionsV1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind: "network-attachment-definitions.k8s.cni.cncf.io",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "network-attachment-definitions.k8s.cni.cncf.io",
		},
		Spec: apiextensionsV1.CustomResourceDefinitionSpec{
			Group: "k8s.cni.cncf.io",
			Scope: apiextensionsV1.NamespaceScoped,
			Names: apiextensionsV1.CustomResourceDefinitionNames{
				Plural:   "network-attachment-definitions",
				Singular: "network-attachment-definition",
				Kind:     "NetworkAttachmentDefinition",
				ShortNames: []string{
					"net-attach-def",
				},
			},
			Versions: []apiextensionsV1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsV1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsV1.JSONSchemaProps{
							Description: "NetworkAttachmentDefinition is a CRD schema specified by the Network Plumbing Working Group to express the intent for attaching pods to one or more logical or physical networks. More information available at: https://github.com/k8snetworkplumbingwg/multi-net-spec",
							Type:        "object",
							Properties: map[string]apiextensionsV1.JSONSchemaProps{
								"apiVersion": {
									Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
									Type:        "string",
								},
								"kind": {
									Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
									Type:        "string",
								},
								"metadata": {
									Type: "object",
								},
								"spec": {
									Description: "NetworkAttachmentDefinition spec defines the desired state of a network attachment",
									Type:        "object",
									Properties: map[string]apiextensionsV1.JSONSchemaProps{
										"config": {
											Description: "NetworkAttachmentDefinition config is a JSON-formatted CNI configuration",
											Type:        "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	_, err := d.ApiextensionsClientSet.ApiextensionsV1().CustomResourceDefinitions().Create(context.Background(), &networkAttachmentDefinition, metav1.CreateOptions{})
	return err
}

func (d defaultMultusResourceManager) CreateClusterRoleBinding() error {
	clusterRoleBinding = v1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "multus",
		},
		RoleRef: v1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "multus",
		},
		Subjects: []v1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "multus",
				Namespace: "kube-system",
			},
		},
	}
	return d.k8sClient.Create(context.Background(), &clusterRoleBinding)
}

func (d defaultMultusResourceManager) CreateServiceAccount() error {
	serviceAccount = corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1.ServiceAccountKind,
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multus",
			Namespace: "kube-system",
		},
	}
	return d.k8sClient.Create(context.Background(), &serviceAccount)
}

func (d defaultMultusResourceManager) CreateClusterRole() error {
	clusterRole = v1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "multus",
		},
		Rules: []v1.PolicyRule{
			{
				APIGroups: []string{
					"k8s.cni.cncf.io",
				},
				Resources: []string{
					"*",
				},
				Verbs: []string{
					"*",
				},
			},
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"pods",
					"pods/status",
				},
				Verbs: []string{
					"get",
					"update",
				},
			},
			{
				APIGroups: []string{
					"",
					"events.k8s.io",
				},
				Resources: []string{
					"events",
				},
				Verbs: []string{
					"create",
					"patch",
					"update",
				},
			},
		},
	}
	return d.k8sClient.Create(context.Background(), &clusterRole)
}

func (d defaultMultusResourceManager) CreateMultusDaemonset() (appsv1.DaemonSet, error) {
	ds = appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-multus-ds",
			Namespace: "kube-system",
			Labels: map[string]string{
				"tier": "node",
				"app":  "multus",
				"name": "multus",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "multus",
				},
			},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"tier": "node",
						"app":  "multus",
						"name": "multus",
					},
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "kubernetes.io/os",
												Operator: corev1.NodeSelectorOpIn,
												Values: []string{
													"linux",
												},
											},
											{
												Key:      "eks.amazonaws.com/compute-type",
												Operator: corev1.NodeSelectorOpNotIn,
												Values: []string{
													"fargate",
												},
											},
										},
									},
								},
							},
						},
					},
					HostNetwork: true,
					Tolerations: []corev1.Toleration{
						{
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					ServiceAccountName: "multus",
					Containers: []corev1.Container{
						manifest.NewMultusContainer("kube-multus", "602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/multus-cni:v3.7.2-eksbuild.2"),
					},
					TerminationGracePeriodSeconds: &[]int64{10}[0],
					Volumes: []corev1.Volume{
						{
							Name: "cni",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/cni/net.d",
								},
							},
						},
						{
							Name: "cnibin",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/opt/cni/bin",
								},
							},
						},
					},
				},
			},
		},
	}
	err := d.k8sClient.Create(context.Background(), &ds)
	return ds, err
}

func (d defaultMultusResourceManager) DeleteClusterRole() error {
	return d.k8sClient.Delete(context.Background(), &clusterRole)
}

func (d defaultMultusResourceManager) DeleteClusterRoleBinding() error {
	return d.k8sClient.Delete(context.Background(), &clusterRoleBinding)
}

func (d defaultMultusResourceManager) DeleteServiceAccount() error {
	return d.k8sClient.Delete(context.Background(), &serviceAccount)
}

func (d defaultMultusResourceManager) DeleteNetworkAttachmentDefinitionCRD() error {
	return d.ApiextensionsClientSet.ApiextensionsV1().CustomResourceDefinitions().Delete(context.Background(), networkAttachmentDefinition.Name, metav1.DeleteOptions{})
}

func (d defaultMultusResourceManager) DeleteMultusDaemonset() error {
	return d.k8sClient.Delete(context.Background(), &ds)
}
