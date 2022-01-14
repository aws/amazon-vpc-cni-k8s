package crd

import (
	"context"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	apiextensionsV1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type CrdManager interface {
	MultusCRD
	GetCRD(string) (*apiextensionsV1.CustomResourceDefinition, error)
	DeleteCRD(string) error
}

type MultusCRD interface {
	CreateNetworkAttachmentDefinitionCRD() error
}

type defaultCrdManager struct {
	ApiextensionsClientSet *apiextensionsclient.Clientset
}

func NewCrdManager(config *rest.Config) CrdManager {
	return &defaultCrdManager{
		ApiextensionsClientSet: apiextensionsclient.NewForConfigOrDie(config),
	}
}

func (d defaultCrdManager) GetCRD(name string) (*apiextensionsV1.CustomResourceDefinition, error) {
	ctx := context.Background()
	crd, err := d.ApiextensionsClientSet.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return &apiextensionsV1.CustomResourceDefinition{}, err
	}
	return crd, nil
}

func (d defaultCrdManager) CreateNetworkAttachmentDefinitionCRD() error {
	networkAttachmentDefinitionSpec := apiextensionsV1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind: utils.NetworkAttachmentDefinitionName,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: utils.NetworkAttachmentDefinitionName,
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
	_, err := d.ApiextensionsClientSet.ApiextensionsV1().CustomResourceDefinitions().Create(context.Background(), &networkAttachmentDefinitionSpec, metav1.CreateOptions{})
	return err
}

func (d defaultCrdManager) DeleteCRD(name string) error {
	return d.ApiextensionsClientSet.ApiextensionsV1().CustomResourceDefinitions().Delete(context.Background(), name, metav1.DeleteOptions{})
}
