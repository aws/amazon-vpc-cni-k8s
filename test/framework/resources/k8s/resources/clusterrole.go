package resources

import (
	"context"

	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterRoleManager interface {
	CreateClusterRole(string, []v1.PolicyRule) error
	GetClusterRole(string) (*v1.ClusterRole, error)
	DeleteClusterRole(string) error
	CreateClusterRoleBinding(string, string, []v1.Subject) error
	GetClusterRoleBinding(string) (*v1.ClusterRoleBinding, error)
	DeleteClusterRoleBinding(string) error
}

type defaultClusterRoleManager struct {
	k8sClient client.DelegatingClient
}

func NewClusterRoleManager(k8sClient client.DelegatingClient) ClusterRoleManager {
	return &defaultClusterRoleManager{k8sClient: k8sClient}
}

func (d defaultClusterRoleManager) GetClusterRole(name string) (*v1.ClusterRole, error) {
	ctx := context.Background()
	clusterRole := &v1.ClusterRole{}
	if err := d.k8sClient.Get(ctx, types.NamespacedName{
		Namespace: "",
		Name:      name,
	}, clusterRole); err != nil {
		return clusterRole, err
	}
	return clusterRole, nil
}

func (d defaultClusterRoleManager) GetClusterRoleBinding(name string) (*v1.ClusterRoleBinding, error) {
	ctx := context.Background()
	clusterRoleBinding := &v1.ClusterRoleBinding{}
	if err := d.k8sClient.Get(ctx, types.NamespacedName{
		Namespace: "",
		Name:      name,
	}, clusterRoleBinding); err != nil {
		return clusterRoleBinding, err
	}
	return clusterRoleBinding, nil
}

func (d defaultClusterRoleManager) CreateClusterRoleBinding(name string, clusterRoleRefName string, subjects []v1.Subject) error {
	var subjectList []v1.Subject
	subjectList = append(subjectList, subjects...)

	clusterRoleBinding := v1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		RoleRef: v1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleRefName,
		},
		Subjects: subjectList,
	}
	if err := d.k8sClient.Create(context.Background(), &clusterRoleBinding); err != nil {
		return err
	}
	return nil
}

func (d defaultClusterRoleManager) DeleteClusterRoleBinding(name string) error {
	clusterRoleBinding, err := d.GetClusterRoleBinding(name)
	if err != nil {
		return err
	}
	return d.k8sClient.Delete(context.Background(), clusterRoleBinding)
}

func (d defaultClusterRoleManager) CreateClusterRole(name string, rules []v1.PolicyRule) error {
	var ruleList []v1.PolicyRule

	ruleList = append(ruleList, rules...)

	clusterRole = v1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: ruleList,
	}

	if err := d.k8sClient.Create(context.Background(), &clusterRole); err != nil {
		return err
	}
	return nil
}

func (d defaultClusterRoleManager) DeleteClusterRole(name string) error {
	clusterRole, err := d.GetClusterRole(name)
	if err != nil {
		return err
	}
	return d.k8sClient.Delete(context.Background(), clusterRole)
}
