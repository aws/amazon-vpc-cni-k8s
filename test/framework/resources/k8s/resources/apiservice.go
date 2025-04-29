package resources

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
)

type APIServiceManager interface {
	CleanupUnavailable() error
}
type defaultAPIServiceManager struct {
	client apiregistrationclient.Interface
}

func NewAPIServiceManager(client apiregistrationclient.Interface) APIServiceManager {
	return &defaultAPIServiceManager{
		client: client,
	}
}
func (m *defaultAPIServiceManager) CleanupUnavailable() error {
	apiServices, err := m.client.ApiregistrationV1().APIServices().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list API services: %v", err)
	}
	for _, apiService := range apiServices.Items {
		for _, condition := range apiService.Status.Conditions {
			if condition.Type == "Available" && condition.Status == "False" {
				if isCriticalAPIService(apiService.Name) {
					continue
				}
				if err := m.client.ApiregistrationV1().APIServices().Delete(
					context.TODO(), apiService.Name, metav1.DeleteOptions{}); err != nil {
					return fmt.Errorf("failed to delete API service %s: %v", apiService.Name, err)
				}
			}
		}
	}
	return nil
}
func isCriticalAPIService(name string) bool {
	criticalServices := map[string]bool{
		"v1":                              true,
		"v1.apps":                         true,
		"v1.admissionregistration.k8s.io": true,
	}
	return criticalServices[name]
}
