package resources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceAccountManager interface {
	GetServiceAccount(string, string) (*corev1.ServiceAccount, error)
	CreateServiceAccount(string, string) error
	DeleteServiceAccount(string, string) error
}

type defaultServiceAccountManager struct {
	k8sClient client.DelegatingClient
}

func NewServiceAccountManager(k8sclient client.DelegatingClient) ServiceAccountManager {
	return &defaultServiceAccountManager{
		k8sClient: k8sclient,
	}
}

func (d defaultServiceAccountManager) GetServiceAccount(name string, namespace string) (*corev1.ServiceAccount, error) {
	ctx := context.Background()
	serviceAccount := &corev1.ServiceAccount{}
	err := d.k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, serviceAccount)
	return serviceAccount, err
}

func (d defaultServiceAccountManager) CreateServiceAccount(name string, namespace string) error {
	serviceAccount := corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1.ServiceAccountKind,
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := d.k8sClient.Create(context.Background(), &serviceAccount); err != nil {
		return err
	}
	return nil
}

func (d defaultServiceAccountManager) DeleteServiceAccount(name string, namespace string) error {
	serviceAccount, err := d.GetServiceAccount(name, namespace)
	if err != nil {
		return err
	}
	return d.k8sClient.Delete(context.Background(), serviceAccount)
}
