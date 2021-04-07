package metrics

import (
	"context"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodWatcher interface {
	GetCNIPods(ctx context.Context) ([]string, error)
}

type defaultPodWatcher struct {
	k8sClient client.Client
	log       logger.Logger
}

// NewDefaultPodWatcher creates a new podWatcher
func NewDefaultPodWatcher(k8sClient client.Client, log logger.Logger) *defaultPodWatcher{
	return &defaultPodWatcher{
		k8sClient: k8sClient,
		log: log,
	}
}

//Returns aws-node pod info. Below function assumes CNI pods follow aws-node* naming format
//and so the function has to be updated if the CNI pod name format changes.
func (d *defaultPodWatcher) GetCNIPods(ctx context.Context) ([]string, error){
	var CNIPods []string
	var podList corev1.PodList
	listOptions := client.ListOptions{
		Namespace:     metav1.NamespaceSystem,
	}

	err := d.k8sClient.List(ctx, &podList, &listOptions)
	if err != nil {
		return CNIPods, err
	}

	for _,pod := range podList.Items {
		if strings.HasPrefix(pod.Name, "aws-node") {
			CNIPods = append(CNIPods, pod.Name)
		}
	}

	d.log.Debugf("Total aws-node pod count:- ", len(CNIPods))
	return CNIPods, nil
}
