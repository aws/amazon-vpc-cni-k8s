// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package engine

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/engine/image"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/cihub/seelog"
	"golang.org/x/net/context"
)

const (
	imageNotFoundForDeletionError = "no such image"
)

// ImageManager is responsible for saving the Image states,
// adding and removing container references to ImageStates
type ImageManager interface {
	RecordContainerReference(container *api.Container) error
	RemoveContainerReferenceFromImageState(container *api.Container) error
	AddAllImageStates(imageStates []*image.ImageState)
	GetImageStateFromImageName(containerImageName string) *image.ImageState
	StartImageCleanupProcess(ctx context.Context)
	SetSaver(stateManager statemanager.Saver)
}

// dockerImageManager accounts all the images and their states in the instance.
// It also has the cleanup policy configuration.
type dockerImageManager struct {
	imageStates                      []*image.ImageState
	client                           DockerClient
	updateLock                       sync.RWMutex
	imageCleanupTicker               *time.Ticker
	state                            dockerstate.TaskEngineState
	saver                            statemanager.Saver
	imageStatesConsideredForDeletion map[string]*image.ImageState
	minimumAgeBeforeDeletion         time.Duration
	numImagesToDelete                int
	imageCleanupTimeInterval         time.Duration
}

// ImageStatesForDeletion is used for implementing the sort interface
type ImageStatesForDeletion []*image.ImageState

// NewImageManager returns a new ImageManager
func NewImageManager(cfg *config.Config, client DockerClient, state dockerstate.TaskEngineState) ImageManager {
	return &dockerImageManager{
		client: client,
		state:  state,
		minimumAgeBeforeDeletion: cfg.MinimumImageDeletionAge,
		numImagesToDelete:        cfg.NumImagesToDeletePerCycle,
		imageCleanupTimeInterval: cfg.ImageCleanupInterval,
	}
}

func (imageManager *dockerImageManager) SetSaver(stateManager statemanager.Saver) {
	imageManager.saver = stateManager
}

func (imageManager *dockerImageManager) AddAllImageStates(imageStates []*image.ImageState) {
	imageManager.updateLock.Lock()
	defer imageManager.updateLock.Unlock()
	for _, imageState := range imageStates {
		imageManager.addImageState(imageState)
	}
}

func (imageManager *dockerImageManager) GetImageStatesCount() int {
	imageManager.updateLock.RLock()
	defer imageManager.updateLock.RUnlock()
	return len(imageManager.imageStates)
}

// RecordContainerReference adds container reference to the corresponding imageState object
func (imageManager *dockerImageManager) RecordContainerReference(container *api.Container) error {
	// the image state has been updated, save the new state
	defer imageManager.saver.ForceSave()
	// On agent restart, container ID was retrieved from agent state file
	// TODO add setter and getter for modifying this
	if container.ImageID != "" {
		if !imageManager.addContainerReferenceToExistingImageState(container) {
			return fmt.Errorf("Failed to add container to existing image state")
		}
		return nil
	}

	if container.Image == "" {
		return fmt.Errorf("Invalid container reference: Empty image name")
	}

	// Inspect image for obtaining Container's Image ID
	imageInspected, err := imageManager.client.InspectImage(container.Image)
	if err != nil {
		seelog.Errorf("Error inspecting image %v: %v", container.Image, err)
		return err
	}

	container.ImageID = imageInspected.ID
	added := imageManager.addContainerReferenceToExistingImageState(container)
	if !added {
		imageManager.addContainerReferenceToNewImageState(container, imageInspected.Size)
	}
	return nil
}

func (imageManager *dockerImageManager) addContainerReferenceToExistingImageState(container *api.Container) bool {
	// this lock is used for reading the image states in the image manager
	imageManager.updateLock.RLock()
	defer imageManager.updateLock.RUnlock()
	imageManager.removeExistingImageNameOfDifferentID(container.Image, container.ImageID)
	imageState, ok := imageManager.getImageState(container.ImageID)
	if ok {
		imageState.UpdateImageState(container)
	}
	return ok
}

func (imageManager *dockerImageManager) addContainerReferenceToNewImageState(container *api.Container, imageSize int64) {
	// this lock is used while creating and adding new image state to image manager
	imageManager.updateLock.Lock()
	defer imageManager.updateLock.Unlock()
	imageManager.removeExistingImageNameOfDifferentID(container.Image, container.ImageID)
	// check to see if a different thread added image state for same image ID
	imageState, ok := imageManager.getImageState(container.ImageID)
	if ok {
		imageState.UpdateImageState(container)
	} else {
		sourceImage := &image.Image{
			ImageID: container.ImageID,
			Size:    imageSize,
		}
		sourceImageState := &image.ImageState{
			Image:      sourceImage,
			PulledAt:   time.Now(),
			LastUsedAt: time.Now(),
		}
		sourceImageState.UpdateImageState(container)
		imageManager.addImageState(sourceImageState)
	}
}

// RemoveContainerReferenceFromImageState removes container reference from the corresponding imageState object
func (imageManager *dockerImageManager) RemoveContainerReferenceFromImageState(container *api.Container) error {
	// the image state has been updated, save the new state
	defer imageManager.saver.ForceSave()
	// this lock is for reading image states and finding the one that the container belongs to
	imageManager.updateLock.RLock()
	defer imageManager.updateLock.RUnlock()
	if container.ImageID == "" {
		return fmt.Errorf("Invalid container reference: Empty image id")
	}

	// Find image state that this container is part of, and remove the reference
	imageState, ok := imageManager.getImageState(container.ImageID)
	if !ok {
		return fmt.Errorf("Cannot find image state for the container to be removed")
	}
	// Found matching ImageState
	return imageState.RemoveContainerReference(container)
}

func (imageManager *dockerImageManager) addImageState(imageState *image.ImageState) {
	imageManager.imageStates = append(imageManager.imageStates, imageState)
}

// getAllImageStates returns the list of imageStates in the instance
func (imageManager *dockerImageManager) getAllImageStates() []*image.ImageState {
	return imageManager.imageStates
}

// getImageState returns the ImageState object that the container is referenced at
func (imageManager *dockerImageManager) getImageState(containerImageID string) (*image.ImageState, bool) {
	for _, imageState := range imageManager.getAllImageStates() {
		if imageState.Image.ImageID == containerImageID {
			return imageState, true
		}
	}
	return nil, false
}

// removeImageState removes the imageState from the list of imageState objects in ImageManager
func (imageManager *dockerImageManager) removeImageState(imageStateToBeRemoved *image.ImageState) {
	for i, imageState := range imageManager.imageStates {
		if imageState.Image.ImageID == imageStateToBeRemoved.Image.ImageID {
			// Image State found; hence remove it
			seelog.Infof("Removing Image State: [%s] from Image Manager", imageState.String())
			imageManager.imageStates = append(imageManager.imageStates[:i], imageManager.imageStates[i+1:]...)
			return
		}
	}
}

func (imageManager *dockerImageManager) getCandidateImagesForDeletion() []*image.ImageState {
	if len(imageManager.imageStatesConsideredForDeletion) < 1 {
		seelog.Debugf("Image Manager: Empty state!")
		// no image states present in image manager
		return nil
	}
	var imagesForDeletion []*image.ImageState
	for _, imageState := range imageManager.imageStatesConsideredForDeletion {
		if imageManager.isImageOldEnough(imageState) && imageState.HasNoAssociatedContainers() {
			seelog.Infof("Candidate image for deletion: [%s]", imageState.String())
			imagesForDeletion = append(imagesForDeletion, imageState)
		}
	}
	return imagesForDeletion
}

func (imageManager *dockerImageManager) isImageOldEnough(imageState *image.ImageState) bool {
	ageOfImage := time.Now().Sub(imageState.PulledAt)
	return ageOfImage > imageManager.minimumAgeBeforeDeletion
}

// Implementing sort interface based on last used times of the images
func (imageStates ImageStatesForDeletion) Len() int {
	return len(imageStates)
}

func (imageStates ImageStatesForDeletion) Less(i, j int) bool {
	return imageStates[i].LastUsedAt.Before(imageStates[j].LastUsedAt)
}

func (imageStates ImageStatesForDeletion) Swap(i, j int) {
	imageStates[i], imageStates[j] = imageStates[j], imageStates[i]
}

func (imageManager *dockerImageManager) getLeastRecentlyUsedImage(imagesForDeletion []*image.ImageState) *image.ImageState {
	var candidateImages ImageStatesForDeletion
	for _, imageState := range imagesForDeletion {
		candidateImages = append(candidateImages, imageState)
	}
	// sort images in the order of last used times
	sort.Sort(candidateImages)
	// return only the top LRU image for deletion
	return candidateImages[0]
}

func (imageManager *dockerImageManager) removeExistingImageNameOfDifferentID(containerImageName string, inspectedImageID string) {
	for _, imageState := range imageManager.getAllImageStates() {
		// image with same name pulled in the instance. Untag the already existing image name
		if imageState.Image.ImageID != inspectedImageID {
			imageState.RemoveImageName(containerImageName)
		}
	}
}

func (imageManager *dockerImageManager) StartImageCleanupProcess(ctx context.Context) {
	// passing the cleanup interval as argument which would help during testing
	imageManager.performPeriodicImageCleanup(ctx, imageManager.imageCleanupTimeInterval)
}

func (imageManager *dockerImageManager) performPeriodicImageCleanup(ctx context.Context, imageCleanupInterval time.Duration) {
	imageManager.imageCleanupTicker = time.NewTicker(imageCleanupInterval)
	for {
		select {
		case <-imageManager.imageCleanupTicker.C:
			go imageManager.removeUnusedImages()
		case <-ctx.Done():
			imageManager.imageCleanupTicker.Stop()
			return
		}
	}
}

func (imageManager *dockerImageManager) removeUnusedImages() {
	seelog.Debug("Attempting to obtain ImagePullDeleteLock for removing images")
	ImagePullDeleteLock.Lock()
	seelog.Debug("Obtained ImagePullDeleteLock for removing images")
	defer seelog.Debug("Released ImagePullDeleteLock after removing images")
	defer ImagePullDeleteLock.Unlock()

	imageManager.updateLock.Lock()
	defer imageManager.updateLock.Unlock()

	imageManager.imageStatesConsideredForDeletion = make(map[string]*image.ImageState)
	seelog.Info("Begin building map of eligible unused images for deletion")
	for _, imageState := range imageManager.getAllImageStates() {
		imageManager.imageStatesConsideredForDeletion[imageState.Image.ImageID] = imageState
	}
	for i := 0; i < imageManager.numImagesToDelete; i++ {
		err := imageManager.removeLeastRecentlyUsedImage()
		if err != nil {
			seelog.Infof("End of eligible images for deletion: %v; Still have %d image states being managed", err, len(imageManager.getAllImageStates()))
			break
		}
	}
}

func (imageManager *dockerImageManager) removeLeastRecentlyUsedImage() error {
	leastRecentlyUsedImage := imageManager.getUnusedImageForDeletion()
	if leastRecentlyUsedImage == nil {
		return fmt.Errorf("No more eligible images for deletion")
	}
	imageManager.removeImage(leastRecentlyUsedImage)
	return nil
}

func (imageManager *dockerImageManager) getUnusedImageForDeletion() *image.ImageState {
	candidateImageStatesForDeletion := imageManager.getCandidateImagesForDeletion()
	if len(candidateImageStatesForDeletion) < 1 {
		seelog.Infof("No eligible images for deletion for this cleanup cycle")
		return nil
	}
	seelog.Infof("Found %d eligible images for deletion", len(candidateImageStatesForDeletion))
	return imageManager.getLeastRecentlyUsedImage(candidateImageStatesForDeletion)
}

func (imageManager *dockerImageManager) removeImage(leastRecentlyUsedImage *image.ImageState) {
	// Handling deleting while traversing a slice
	imageNames := make([]string, len(leastRecentlyUsedImage.Image.Names))
	copy(imageNames, leastRecentlyUsedImage.Image.Names)
	if len(imageNames) == 0 {
		// potentially untagged image of format <none>:<none>; remove by ID
		imageManager.deleteImage(leastRecentlyUsedImage.Image.ImageID, leastRecentlyUsedImage)
	} else {
		// Image has multiple tags/repos. Untag each name and delete the final reference to image
		for _, imageName := range imageNames {
			imageManager.deleteImage(imageName, leastRecentlyUsedImage)
		}
	}
}

func (imageManager *dockerImageManager) deleteImage(imageID string, imageState *image.ImageState) {
	if imageID == "" {
		seelog.Errorf("Image ID to be deleted is null")
		return
	}
	seelog.Infof("Removing Image: %s", imageID)
	err := imageManager.client.RemoveImage(imageID, removeImageTimeout)
	if err != nil {
		if err.Error() == imageNotFoundForDeletionError {
			seelog.Errorf("Image already removed from the instance: %v", err)
		} else {
			seelog.Errorf("Error removing Image %v - %v", imageID, err)
			delete(imageManager.imageStatesConsideredForDeletion, imageState.Image.ImageID)
			return
		}
	}
	seelog.Infof("Image removed: %v", imageID)
	imageState.RemoveImageName(imageID)
	if len(imageState.Image.Names) == 0 {
		seelog.Infof("Cleaning up all tracking information for image %s as it has zero references", imageID)
		delete(imageManager.imageStatesConsideredForDeletion, imageState.Image.ImageID)
		imageManager.removeImageState(imageState)
		imageManager.state.RemoveImageState(imageState)
		imageManager.saver.Save()
	}
}

func (imageManager *dockerImageManager) GetImageStateFromImageName(containerImageName string) *image.ImageState {
	imageManager.updateLock.Lock()
	defer imageManager.updateLock.Unlock()
	for _, imageState := range imageManager.getAllImageStates() {
		for _, imageName := range imageState.Image.Names {
			if imageName == containerImageName {
				return imageState
			}
		}
	}
	return nil
}
