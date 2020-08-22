// Automatically generated by MockGen. DO NOT EDIT!
// Source: ec2metadatawrapper.go

package mock_ec2metadatawrapper

import (
	ec2metadata "github.com/aws/aws-sdk-go/aws/ec2metadata"
	gomock "github.com/golang/mock/gomock"
)

// Mock of HTTPClient interface
type MockHTTPClient struct {
	ctrl     *gomock.Controller
	recorder *_MockHTTPClientRecorder
}

// Recorder for MockHTTPClient (not exported)
type _MockHTTPClientRecorder struct {
	mock *MockHTTPClient
}

func NewMockHTTPClient(ctrl *gomock.Controller) *MockHTTPClient {
	mock := &MockHTTPClient{ctrl: ctrl}
	mock.recorder = &_MockHTTPClientRecorder{mock}
	return mock
}

func (_m *MockHTTPClient) EXPECT() *_MockHTTPClientRecorder {
	return _m.recorder
}

func (_m *MockHTTPClient) GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	ret := _m.ctrl.Call(_m, "GetInstanceIdentityDocument")
	ret0, _ := ret[0].(ec2metadata.EC2InstanceIdentityDocument)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockHTTPClientRecorder) GetInstanceIdentityDocument() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "GetInstanceIdentityDocument")
}

func (_m *MockHTTPClient) Region() (string, error) {
	ret := _m.ctrl.Call(_m, "Region")
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockHTTPClientRecorder) Region() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Region")
}

// Mock of EC2MetadataClient interface
type MockEC2MetadataClient struct {
	ctrl     *gomock.Controller
	recorder *_MockEC2MetadataClientRecorder
}

// Recorder for MockEC2MetadataClient (not exported)
type _MockEC2MetadataClientRecorder struct {
	mock *MockEC2MetadataClient
}

func NewMockEC2MetadataClient(ctrl *gomock.Controller) *MockEC2MetadataClient {
	mock := &MockEC2MetadataClient{ctrl: ctrl}
	mock.recorder = &_MockEC2MetadataClientRecorder{mock}
	return mock
}

func (_m *MockEC2MetadataClient) EXPECT() *_MockEC2MetadataClientRecorder {
	return _m.recorder
}

func (_m *MockEC2MetadataClient) GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	ret := _m.ctrl.Call(_m, "GetInstanceIdentityDocument")
	ret0, _ := ret[0].(ec2metadata.EC2InstanceIdentityDocument)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockEC2MetadataClientRecorder) GetInstanceIdentityDocument() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "GetInstanceIdentityDocument")
}

func (_m *MockEC2MetadataClient) Region() (string, error) {
	ret := _m.ctrl.Call(_m, "Region")
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockEC2MetadataClientRecorder) Region() *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Region")
}
