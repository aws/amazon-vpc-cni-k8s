// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package resources

import (
	"context"

	eventsv1 "k8s.io/api/events/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type EventManager interface {
	GetEventsWithOptions(opts *client.ListOptions) (eventsv1.EventList, error)
}

type defaultEventManager struct {
	k8sClient client.Client
}

func NewEventManager(k8sClient client.Client) EventManager {
	return &defaultEventManager{k8sClient: k8sClient}
}

func (d defaultEventManager) GetEventsWithOptions(opts *client.ListOptions) (eventsv1.EventList, error) {
	eventList := eventsv1.EventList{}
	err := d.k8sClient.List(context.Background(), &eventList, opts)
	return eventList, err
}
