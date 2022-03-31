package resources

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NetworkPolicyManager interface {
	CreateNetworkPolicy(networkPolicy client.Object) error
	DeleteNetworkPolicy(networkPolicy client.Object) error
}

type defaultNetworkPolicyManager struct {
	networkPolicyClient client.Client
}

func NewNetworkPolicyManager(client client.Client) NetworkPolicyManager {
	return &defaultNetworkPolicyManager{client}
}

func (d *defaultNetworkPolicyManager) CreateNetworkPolicy(networkPolicy client.Object) error {
	return d.networkPolicyClient.Create(context.Background(), networkPolicy)
}

func (d *defaultNetworkPolicyManager) DeleteNetworkPolicy(networkPolicy client.Object) error {
	return d.networkPolicyClient.Delete(context.Background(), networkPolicy)
}
