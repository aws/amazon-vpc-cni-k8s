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

package updater

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/acs/update_handler/os/mock"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/httpclient"
	"github.com/aws/amazon-ecs-agent/agent/httpclient/mock"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	mock_client "github.com/aws/amazon-ecs-agent/agent/wsclient/mock"
)

func ptr(i interface{}) interface{} {
	n := reflect.New(reflect.TypeOf(i))
	n.Elem().Set(reflect.ValueOf(i))
	return n.Interface()
}

func mocks(t *testing.T, cfg *config.Config) (*updater, *gomock.Controller, *config.Config, *mock_os.MockFileSystem, *mock_client.MockClientServer, *mock_http.MockRoundTripper) {
	if cfg == nil {
		cfg = &config.Config{
			UpdatesEnabled:    true,
			UpdateDownloadDir: filepath.Clean("/tmp/test/"),
		}
	}
	ctrl := gomock.NewController(t)

	mockfs := mock_os.NewMockFileSystem(ctrl)
	mockacs := mock_client.NewMockClientServer(ctrl)
	mockhttp := mock_http.NewMockRoundTripper(ctrl)
	httpClient := httpclient.New(updateDownloadTimeout, false)
	httpClient.Transport.(httpclient.OverridableTransport).SetTransport(mockhttp)

	u := &updater{
		acs:        mockacs,
		config:     cfg,
		fs:         mockfs,
		httpclient: httpClient,
	}

	return u, ctrl, cfg, mockfs, mockacs, mockhttp
}

func TestStageUpdateWithUpdatesDisabled(t *testing.T) {
	u, ctrl, _, _, mockacs, _ := mocks(t, &config.Config{
		UpdatesEnabled: false,
	})
	defer ctrl.Finish()

	mockacs.EXPECT().MakeRequest(&nackRequestMatcher{&ecsacs.NackRequest{
		Cluster:           ptr("cluster").(*string),
		ContainerInstance: ptr("containerInstance").(*string),
		MessageId:         ptr("mid").(*string),
		Reason:            ptr("Updates are disabled").(*string),
	}})

	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("6caeef375a080e3241781725b357890758d94b15d7ce63f6b2ff1cb5589f2007").(*string),
		},
	})

}

func TestPerformUpdateWithUpdatesDisabled(t *testing.T) {
	u, ctrl, cfg, _, mockacs, _ := mocks(t, &config.Config{
		UpdatesEnabled: false,
	})
	defer ctrl.Finish()

	mockacs.EXPECT().MakeRequest(&nackRequestMatcher{&ecsacs.NackRequest{
		Cluster:           ptr("cluster").(*string),
		ContainerInstance: ptr("containerInstance").(*string),
		MessageId:         ptr("mid").(*string),
		Reason:            ptr("Updates are disabled").(*string),
	}})

	u.performUpdateHandler(statemanager.NewNoopStateManager(), engine.NewTaskEngine(cfg, nil, nil, nil, nil, nil, nil))(&ecsacs.PerformUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("c54518806ff4d14b680c35784113e1e7478491fe").(*string),
		},
	})
}

func TestFullUpdateFlow(t *testing.T) {
	// Test support for update via other regions' endpoints.
	regions := map[string]string{
		"us-east-1": "s3.amazonaws.com",
		"us-east-2": "s3-us-east-2.amazonaws.com",
		"us-west-1": "s3-us-west-1.amazonaws.com",
	}

	for region, host := range regions {
		t.Run(region, func(t *testing.T) {
			u, ctrl, cfg, mockfs, mockacs, mockhttp := mocks(t, nil)
			defer ctrl.Finish()

			var writtenFile bytes.Buffer
			gomock.InOrder(
				mockhttp.EXPECT().RoundTrip(mock_http.NewHTTPSimpleMatcher("GET", "https://"+host+"/amazon-ecs-agent/update.tar")).Return(mock_http.SuccessResponse("update-tar-data"), nil),
				mockfs.EXPECT().Create(gomock.Any()).Return(mock_os.NopReadWriteCloser(&writtenFile), nil),
				mockfs.EXPECT().WriteFile(filepath.Clean("/tmp/test/desired-image"), gomock.Any(), gomock.Any()).Return(nil),
				mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
					Cluster:           ptr("cluster").(*string),
					ContainerInstance: ptr("containerInstance").(*string),
					MessageId:         ptr("mid").(*string),
				})),
				mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
					Cluster:           ptr("cluster").(*string),
					ContainerInstance: ptr("containerInstance").(*string),
					MessageId:         ptr("mid2").(*string),
				})),
				mockfs.EXPECT().Exit(exitcodes.ExitUpdate),
			)

			u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
				ClusterArn:           ptr("cluster").(*string),
				ContainerInstanceArn: ptr("containerInstance").(*string),
				MessageId:            ptr("mid").(*string),
				UpdateInfo: &ecsacs.UpdateInfo{
					Location:  ptr("https://" + host + "/amazon-ecs-agent/update.tar").(*string),
					Signature: ptr("6caeef375a080e3241781725b357890758d94b15d7ce63f6b2ff1cb5589f2007").(*string),
				},
			})

			require.Equal(t, "update-tar-data", writtenFile.String(), "incorrect data written")

			u.performUpdateHandler(statemanager.NewNoopStateManager(), engine.NewTaskEngine(cfg, nil, nil, nil, nil, nil, nil))(&ecsacs.PerformUpdateMessage{
				ClusterArn:           ptr("cluster").(*string),
				ContainerInstanceArn: ptr("containerInstance").(*string),
				MessageId:            ptr("mid2").(*string),
				UpdateInfo: &ecsacs.UpdateInfo{
					Location:  ptr("https://" + host + "/amazon-ecs-agent/update.tar").(*string),
					Signature: ptr("c54518806ff4d14b680c35784113e1e7478491fe").(*string),
				},
			})
		})
	}
}

type nackRequestMatcher struct {
	*ecsacs.NackRequest
}

func (m *nackRequestMatcher) Matches(nack interface{}) bool {
	other := nack.(*ecsacs.NackRequest)
	if m.Cluster != nil && *m.Cluster != *other.Cluster {
		return false
	}
	if m.ContainerInstance != nil && *m.ContainerInstance != *other.ContainerInstance {
		return false
	}
	if m.MessageId != nil && *m.MessageId != *other.MessageId {
		return false
	}
	if m.Reason != nil && *m.Reason != *other.Reason {
		return false
	}
	return true
}

func TestMissingUpdateInfo(t *testing.T) {
	u, ctrl, _, _, mockacs, _ := mocks(t, nil)
	defer ctrl.Finish()

	mockacs.EXPECT().MakeRequest(&nackRequestMatcher{&ecsacs.NackRequest{
		Cluster:           ptr("cluster").(*string),
		ContainerInstance: ptr("containerInstance").(*string),
		MessageId:         ptr("mid").(*string),
	}})

	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid").(*string),
	})
}

func (m *nackRequestMatcher) String() string {
	return fmt.Sprintf("Nack request matcher %v", m.NackRequest)
}

func TestUndownloadedUpdate(t *testing.T) {
	u, ctrl, cfg, _, mockacs, _ := mocks(t, nil)
	defer ctrl.Finish()

	mockacs.EXPECT().MakeRequest(&nackRequestMatcher{&ecsacs.NackRequest{
		Cluster:           ptr("cluster").(*string),
		ContainerInstance: ptr("containerInstance").(*string),
		MessageId:         ptr("mid").(*string),
	}})

	u.performUpdateHandler(statemanager.NewNoopStateManager(), engine.NewTaskEngine(cfg, nil, nil, nil, nil, nil, nil))(&ecsacs.PerformUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid").(*string),
	})
}

func TestDuplicateUpdateMessagesWithSuccess(t *testing.T) {
	u, ctrl, cfg, mockfs, mockacs, mockhttp := mocks(t, nil)
	defer ctrl.Finish()

	var writtenFile bytes.Buffer
	gomock.InOrder(
		mockhttp.EXPECT().RoundTrip(mock_http.NewHTTPSimpleMatcher("GET", "https://s3.amazonaws.com/amazon-ecs-agent/update.tar")).Return(mock_http.SuccessResponse("update-tar-data"), nil),
		mockfs.EXPECT().Create(gomock.Any()).Return(mock_os.NopReadWriteCloser(&writtenFile), nil),
		mockfs.EXPECT().WriteFile(filepath.Clean("/tmp/test/desired-image"), gomock.Any(), gomock.Any()).Return(nil),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("mid").(*string),
		})),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("mid2").(*string),
		})),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("mid3").(*string),
		})),
		mockfs.EXPECT().Exit(exitcodes.ExitUpdate),
	)

	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("6caeef375a080e3241781725b357890758d94b15d7ce63f6b2ff1cb5589f2007").(*string),
		},
	})

	// Multiple requests to stage something with the same signature should still
	// result in only one download / update procedure.
	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid2").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("6caeef375a080e3241781725b357890758d94b15d7ce63f6b2ff1cb5589f2007").(*string),
		},
	})

	require.Equal(t, "update-tar-data", writtenFile.String(), "incorrect data written")

	u.performUpdateHandler(statemanager.NewNoopStateManager(), engine.NewTaskEngine(cfg, nil, nil, nil, nil, nil, nil))(&ecsacs.PerformUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid3").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("c54518806ff4d14b680c35784113e1e7478491fe").(*string),
		},
	})
}

func TestDuplicateUpdateMessagesWithFailure(t *testing.T) {
	u, ctrl, cfg, mockfs, mockacs, mockhttp := mocks(t, nil)
	defer ctrl.Finish()

	var writtenFile bytes.Buffer
	gomock.InOrder(
		mockhttp.EXPECT().RoundTrip(mock_http.NewHTTPSimpleMatcher("GET", "https://s3.amazonaws.com/amazon-ecs-agent/update.tar")).Return(mock_http.SuccessResponse("update-tar-data"), nil),
		mockfs.EXPECT().Create(gomock.Any()).Return(nil, errors.New("test error")),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.NackRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("mid").(*string),
			Reason:            ptr("Unable to download: test error").(*string),
		})),
		mockhttp.EXPECT().RoundTrip(mock_http.NewHTTPSimpleMatcher("GET", "https://s3.amazonaws.com/amazon-ecs-agent/update.tar")).Return(mock_http.SuccessResponse("update-tar-data"), nil),
		mockfs.EXPECT().Create(gomock.Any()).Return(mock_os.NopReadWriteCloser(&writtenFile), nil),
		mockfs.EXPECT().WriteFile(filepath.Clean("/tmp/test/desired-image"), gomock.Any(), gomock.Any()).Return(nil),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("mid2").(*string),
		})),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("mid3").(*string),
		})),
		mockfs.EXPECT().Exit(exitcodes.ExitUpdate),
	)

	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("6caeef375a080e3241781725b357890758d94b15d7ce63f6b2ff1cb5589f2007").(*string),
		},
	})

	// Multiple requests to stage something with the same signature where the previous update failed
	// should cause another update attempt to be started
	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid2").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("6caeef375a080e3241781725b357890758d94b15d7ce63f6b2ff1cb5589f2007").(*string),
		},
	})

	require.Equal(t, "update-tar-data", writtenFile.String(), "incorrect data written")

	u.performUpdateHandler(statemanager.NewNoopStateManager(), engine.NewTaskEngine(cfg, nil, nil, nil, nil, nil, nil))(&ecsacs.PerformUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid3").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("c54518806ff4d14b680c35784113e1e7478491fe").(*string),
		},
	})
}

func TestNewerUpdateMessages(t *testing.T) {
	u, ctrl, cfg, mockfs, mockacs, mockhttp := mocks(t, nil)
	defer ctrl.Finish()

	var writtenFile bytes.Buffer
	gomock.InOrder(
		mockhttp.EXPECT().RoundTrip(mock_http.NewHTTPSimpleMatcher("GET", "https://s3.amazonaws.com/amazon-ecs-agent/update.tar")).Return(mock_http.SuccessResponse("update-tar-data"), nil),
		mockfs.EXPECT().Create(gomock.Any()).Return(mock_os.NopReadWriteCloser(&writtenFile), nil),
		mockfs.EXPECT().WriteFile(filepath.Clean("/tmp/test/desired-image"), gomock.Any(), gomock.Any()).Return(nil),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("StageMID").(*string),
		})),
		mockacs.EXPECT().MakeRequest(&nackRequestMatcher{&ecsacs.NackRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("StageMID").(*string),
			Reason:            ptr("New update arrived: StageMIDNew").(*string),
		}}),
		mockhttp.EXPECT().RoundTrip(mock_http.NewHTTPSimpleMatcher("GET", "https://s3.amazonaws.com/amazon-ecs-agent/new.tar")).Return(mock_http.SuccessResponse("newer-update-tar-data"), nil),
		mockfs.EXPECT().Create(gomock.Any()).Return(mock_os.NopReadWriteCloser(&writtenFile), nil),
		mockfs.EXPECT().WriteFile(filepath.Clean("/tmp/test/desired-image"), gomock.Any(), gomock.Any()).Return(nil),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("StageMIDNew").(*string),
		})),
		mockacs.EXPECT().MakeRequest(gomock.Eq(&ecsacs.AckRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("mid2").(*string),
		})),
		mockfs.EXPECT().Exit(exitcodes.ExitUpdate),
	)

	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("StageMID").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("6caeef375a080e3241781725b357890758d94b15d7ce63f6b2ff1cb5589f2007").(*string),
		},
	})

	require.Equal(t, "update-tar-data", writtenFile.String(), "incorrect data written")
	writtenFile.Reset()

	// Never perform, make sure a new hash results in a new stage
	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("StageMIDNew").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/new.tar").(*string),
			Signature: ptr("9c6ea7bd7d49f95b6d516517e453b965897109bf8a1d6ff3a6e57287049eb2de").(*string),
		},
	})

	require.Equal(t, "newer-update-tar-data", writtenFile.String(), "incorrect data written")

	u.performUpdateHandler(statemanager.NewNoopStateManager(), engine.NewTaskEngine(cfg, nil, nil, nil, nil, nil, nil))(&ecsacs.PerformUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("mid2").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("c54518806ff4d14b680c35784113e1e7478491fe").(*string),
		},
	})
}

func TestValidationError(t *testing.T) {
	u, ctrl, _, mockfs, mockacs, mockhttp := mocks(t, nil)
	defer ctrl.Finish()

	var writtenFile bytes.Buffer
	gomock.InOrder(
		mockhttp.EXPECT().RoundTrip(mock_http.NewHTTPSimpleMatcher("GET", "https://s3.amazonaws.com/amazon-ecs-agent/update.tar")).Return(mock_http.SuccessResponse("update-tar-data"), nil),
		mockfs.EXPECT().Create(gomock.Any()).Return(mock_os.NopReadWriteCloser(&writtenFile), nil),
		mockfs.EXPECT().Remove(gomock.Any()),
		mockacs.EXPECT().MakeRequest(&nackRequestMatcher{&ecsacs.NackRequest{
			Cluster:           ptr("cluster").(*string),
			ContainerInstance: ptr("containerInstance").(*string),
			MessageId:         ptr("StageMID").(*string),
		}}),
	)

	u.stageUpdateHandler()(&ecsacs.StageUpdateMessage{
		ClusterArn:           ptr("cluster").(*string),
		ContainerInstanceArn: ptr("containerInstance").(*string),
		MessageId:            ptr("StageMID").(*string),
		UpdateInfo: &ecsacs.UpdateInfo{
			Location:  ptr("https://s3.amazonaws.com/amazon-ecs-agent/update.tar").(*string),
			Signature: ptr("Invalid signature").(*string),
		},
	})

	assert.Equal(t, "update-tar-data", writtenFile.String(), "incorrect data written")
}
