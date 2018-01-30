// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package updater

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/acs/update_handler/os"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/httpclient"
	"github.com/aws/amazon-ecs-agent/agent/logger"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/utils/ttime"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/cihub/seelog"
)

var log = logger.ForModule("updater")

const desiredImageFile = "desired-image"

// update describes metadata around an update 2-phase request
type updater struct {
	stage     updateStage
	stageTime time.Time
	// downloadMessageID is the most recent message id seen for this update id
	downloadMessageID string
	// updateID is a unique identifier for this update used to determine if a
	// new update request, even with a different message id, is a duplicate or
	// not
	updateID   string
	fs         os.FileSystem
	acs        wsclient.ClientServer
	config     *config.Config
	httpclient *http.Client

	sync.Mutex
}

type updateStage int8

const (
	updateNone updateStage = iota
	updateDownloading
	updateDownloaded
)

const (
	updateDownloadTimeout = 15 * time.Minute
)

// AddAgentUpdateHandlers adds the needed update handlers to perform agent
// updates
func AddAgentUpdateHandlers(cs wsclient.ClientServer, cfg *config.Config, saver statemanager.Saver, taskEngine engine.TaskEngine) {
	singleUpdater := &updater{
		acs:        cs,
		config:     cfg,
		fs:         os.Default,
		httpclient: httpclient.New(updateDownloadTimeout, false),
	}
	cs.AddRequestHandler(singleUpdater.stageUpdateHandler())
	cs.AddRequestHandler(singleUpdater.performUpdateHandler(saver, taskEngine))
}

func (u *updater) stageUpdateHandler() func(req *ecsacs.StageUpdateMessage) {
	return func(req *ecsacs.StageUpdateMessage) {
		u.Lock()
		defer u.Unlock()

		if req == nil || req.MessageId == nil {
			log.Error("Nil request to stage update or missing MessageID")
			return
		}

		nack := func(reason string) {
			seelog.Errorf("Nacking StageUpdate; reason: %s", reason)
			u.acs.MakeRequest(&ecsacs.NackRequest{
				Cluster:           req.ClusterArn,
				ContainerInstance: req.ContainerInstanceArn,
				MessageId:         req.MessageId,
				Reason:            aws.String(reason),
			})
			u.reset()
		}

		if !u.config.UpdatesEnabled {
			nack("Updates are disabled")
			return
		}

		if err := validateUpdateInfo(req.UpdateInfo); err != nil {
			nack("Invalid update: " + err.Error())
			return
		}

		log.Debug("Staging update", "update", req)

		if u.stage != updateNone {
			if u.updateID != "" && u.updateID == *req.UpdateInfo.Signature {
				log.Debug("Update already in progress, acking duplicate message", "id", u.updateID)
				// Acking here is safe as any currently-downloading update will already be holding
				// the update lock.  A failed download will nack and clear state (while holding the
				// update lock) before this code is reached, meaning that the above conditional will
				// not evaluate true (no matching, in-progress update).
				u.acs.MakeRequest(&ecsacs.AckRequest{
					Cluster:           req.ClusterArn,
					ContainerInstance: req.ContainerInstanceArn,
					MessageId:         req.MessageId,
				})
				return
			} else {
				// Nack previous update
				reason := "New update arrived: " + *req.MessageId
				u.acs.MakeRequest(&ecsacs.NackRequest{
					Cluster:           req.ClusterArn,
					ContainerInstance: req.ContainerInstanceArn,
					MessageId:         &u.downloadMessageID,
					Reason:            &reason,
				})
			}
		}
		u.updateID = *req.UpdateInfo.Signature
		u.stage = updateDownloading
		u.stageTime = ttime.Now()
		u.downloadMessageID = *req.MessageId

		err := u.download(req.UpdateInfo)
		if err != nil {
			nack("Unable to download: " + err.Error())
			return
		}

		u.stage = updateDownloaded

		u.acs.MakeRequest(&ecsacs.AckRequest{
			Cluster:           req.ClusterArn,
			ContainerInstance: req.ContainerInstanceArn,
			MessageId:         req.MessageId,
		})
	}
}

func (u *updater) download(info *ecsacs.UpdateInfo) (err error) {
	if info == nil || info.Location == nil {
		return errors.New("No location given")
	}
	if info.Signature == nil {
		return errors.New("No signature given")
	}
	resp, err := u.httpclient.Get(*info.Location)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return err
	}

	outFileBasename := utils.RandHex() + ".ecs-update.tar"
	outFilePath := filepath.Join(u.config.UpdateDownloadDir, outFileBasename)
	outFile, err := u.fs.Create(outFilePath)
	if err != nil {
		return err
	}
	defer func() {
		outFile.Close()
		if err != nil {
			u.fs.Remove(outFilePath)
		}
	}()

	hashsum := sha256.New()
	bodyHashReader := io.TeeReader(resp.Body, hashsum)
	_, err = io.Copy(outFile, bodyHashReader)
	if err != nil {
		return err
	}
	shasum := hashsum.Sum(nil)
	shasumString := fmt.Sprintf("%x", shasum)

	if shasumString != strings.TrimSpace(*info.Signature) {
		return errors.New("Hashsum validation failed")
	}

	err = u.fs.WriteFile(filepath.Join(u.config.UpdateDownloadDir, desiredImageFile), []byte(outFileBasename+"\n"), 0644)
	return err
}

func (u *updater) performUpdateHandler(saver statemanager.Saver, taskEngine engine.TaskEngine) func(req *ecsacs.PerformUpdateMessage) {
	return func(req *ecsacs.PerformUpdateMessage) {
		u.Lock()
		defer u.Unlock()

		log.Debug("Got perform update request")

		if !u.config.UpdatesEnabled {
			reason := "Updates are disabled"
			seelog.Errorf("Nacking PerformUpdate; reason: %s", reason)
			u.acs.MakeRequest(&ecsacs.NackRequest{
				Cluster:           req.ClusterArn,
				ContainerInstance: req.ContainerInstanceArn,
				MessageId:         req.MessageId,
				Reason:            aws.String(reason),
			})
			return
		}

		if u.stage != updateDownloaded {
			log.Error("Nacking PerformUpdate; not downloaded")
			reason := "Cannot perform update; update not downloaded"
			u.acs.MakeRequest(&ecsacs.NackRequest{
				Cluster:           req.ClusterArn,
				ContainerInstance: req.ContainerInstanceArn,
				MessageId:         req.MessageId,
				Reason:            aws.String(reason),
			})
			return
		}
		u.acs.MakeRequest(&ecsacs.AckRequest{
			Cluster:           req.ClusterArn,
			ContainerInstance: req.ContainerInstanceArn,
			MessageId:         req.MessageId,
		})

		err := sighandlers.FinalSave(saver, taskEngine)
		if err != nil {
			log.Crit("Error saving before update exit", "err", err)
		} else {
			log.Debug("Saved state!")
		}
		u.fs.Exit(exitcodes.ExitUpdate)
	}
}

func (u *updater) reset() {
	u.updateID = ""
	u.downloadMessageID = ""
	u.stage = updateNone
	u.stageTime = time.Time{}
}
