package metrics

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

type PodWatcher interface {
	GetCNIPods(ctx context.Context) ([]string, error)
}

type defaultPodWatcher struct {
	k8sClient client.Client
	log       logger.Logger
}

// NewDefaultPodWatcher creates a new podWatcher
func NewDefaultPodWatcher(k8sClient client.Client, log logger.Logger) *defaultPodWatcher {
	return &defaultPodWatcher{
		k8sClient: k8sClient,
		log:       log,
	}
}

//Returns aws-node pod info. Below function assumes CNI pods follow aws-node* naming format
//and so the function has to be updated if the CNI pod name format changes.
func (d *defaultPodWatcher) GetCNIPods(ctx context.Context) ([]string, error) {
	var CNIPods []string
	var podList corev1.PodList
	labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app": "aws-node",
		},
	})

	if err != nil {
		panic(err.Error())
	}

	listOptions := client.ListOptions{
		Namespace:     metav1.NamespaceSystem,
		LabelSelector: labelSelector,
	}

	err = d.k8sClient.List(ctx, &podList, &listOptions)
	if err != nil {
		return CNIPods, err
	}

	for _, pod := range podList.Items {
		CNIPods = append(CNIPods, pod.Name)
	}

	d.log.Infof("Total aws-node pod count:- ", len(CNIPods))
	return CNIPods, nil
}
