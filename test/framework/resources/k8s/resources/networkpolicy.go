package resources

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NetworkPolicyManager interface {
	CreateNetworkPolicy(networkPolicy runtime.Object) error
	DeleteNetworkPolicy(networkPolicy runtime.Object) error
}

type defaultNetworkPolicyManager struct {
	networkPolicyClient client.DelegatingClient
}

func NewNetworkPolicyManager(client client.DelegatingClient) NetworkPolicyManager {
	return &defaultNetworkPolicyManager{client}
}

func (d *defaultNetworkPolicyManager) CreateNetworkPolicy(networkPolicy runtime.Object) error {
	return d.networkPolicyClient.Create(context.Background(), networkPolicy)
}

func (d *defaultNetworkPolicyManager) DeleteNetworkPolicy(networkPolicy runtime.Object) error {
	return d.networkPolicyClient.Delete(context.Background(), networkPolicy)
}
