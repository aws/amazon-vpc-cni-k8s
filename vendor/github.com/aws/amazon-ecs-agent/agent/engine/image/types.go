// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package image

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/cihub/seelog"
)

type Image struct {
	ImageID string
	Names   []string
	Size    int64
}

func (image *Image) String() string {
	return fmt.Sprintf("ImageID: %s; Names: %s", image.ImageID, strings.Join(image.Names, ", "))
}

// ImageState represents a docker image
// and its state information such as containers associated with it
type ImageState struct {
	Image      *Image
	Containers []*api.Container `json:"-"`
	PulledAt   time.Time
	LastUsedAt time.Time
	updateLock sync.RWMutex
}

func (imageState *ImageState) UpdateContainerReference(container *api.Container) {
	imageState.updateLock.Lock()
	defer imageState.updateLock.Unlock()
	seelog.Infof("Updating container reference %v in Image State - %v", container.Name, imageState.Image.ImageID)
	imageState.Containers = append(imageState.Containers, container)
}

func (imageState *ImageState) AddImageName(imageName string) {
	imageState.updateLock.Lock()
	defer imageState.updateLock.Unlock()
	if !imageState.HasImageName(imageName) {
		seelog.Infof("Adding image name- %v to Image state- %v", imageName, imageState.Image.ImageID)
		imageState.Image.Names = append(imageState.Image.Names, imageName)
	}
}

func (imageState *ImageState) GetImageNamesCount() int {
	imageState.updateLock.RLock()
	defer imageState.updateLock.RUnlock()
	return len(imageState.Image.Names)
}

func (imageState *ImageState) HasNoAssociatedContainers() bool {
	return len(imageState.Containers) == 0
}

func (imageState *ImageState) UpdateImageState(container *api.Container) {
	imageState.AddImageName(container.Image)
	imageState.UpdateContainerReference(container)
}

func (imageState *ImageState) RemoveImageName(containerImageName string) {
	imageState.updateLock.Lock()
	defer imageState.updateLock.Unlock()
	for i, imageName := range imageState.Image.Names {
		if imageName == containerImageName {
			imageState.Image.Names = append(imageState.Image.Names[:i], imageState.Image.Names[i+1:]...)
		}
	}
}

func (imageState *ImageState) HasImageName(containerImageName string) bool {
	for _, imageName := range imageState.Image.Names {
		if imageName == containerImageName {
			return true
		}
	}
	return false
}

func (imageState *ImageState) RemoveContainerReference(container *api.Container) error {
	// Get the image state write lock for updating container reference
	imageState.updateLock.Lock()
	defer imageState.updateLock.Unlock()
	for i := range imageState.Containers {
		if imageState.Containers[i].Name == container.Name {
			// Container reference found; hence remove it
			seelog.Infof("Removing Container Reference: %v from Image State- %v", container.Name, imageState.Image.ImageID)
			imageState.Containers = append(imageState.Containers[:i], imageState.Containers[i+1:]...)
			// Update the last used time for the image
			imageState.LastUsedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("Container reference is not found in the image state container: %s", container.String())
}

func (imageState *ImageState) MarshalJSON() ([]byte, error) {
	imageState.updateLock.Lock()
	defer imageState.updateLock.Unlock()

	return json.Marshal(&struct {
		Image      *Image
		PulledAt   time.Time
		LastUsedAt time.Time
	}{
		Image:      imageState.Image,
		PulledAt:   imageState.PulledAt,
		LastUsedAt: imageState.LastUsedAt,
	})
}

func (imageState *ImageState) String() string {
	image := ""
	if imageState.Image != nil {
		image = imageState.Image.String()
	}
	return fmt.Sprintf("Image: [%s] referenced by %d containers; PulledAt: %s; LastUsedAt: %s",
		image, len(imageState.Containers), imageState.PulledAt.String(), imageState.LastUsedAt.String())
}
