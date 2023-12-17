// Code generated by MockGen. DO NOT EDIT.
// Source: downloader.go
//
// Generated by this command:
//
//	mockgen -source=downloader.go -destination=mocks/downloader.go Downloader
//
// Package mock_vendors is a generated GoMock package.
package mock_vendors

import (
	context "context"
	reflect "reflect"

	serverservice "go.hollow.sh/serverservice/pkg/api/v1"
	gomock "go.uber.org/mock/gomock"
)

// MockDownloader is a mock of Downloader interface.
type MockDownloader struct {
	ctrl     *gomock.Controller
	recorder *MockDownloaderMockRecorder
}

// MockDownloaderMockRecorder is the mock recorder for MockDownloader.
type MockDownloaderMockRecorder struct {
	mock *MockDownloader
}

// NewMockDownloader creates a new mock instance.
func NewMockDownloader(ctrl *gomock.Controller) *MockDownloader {
	mock := &MockDownloader{ctrl: ctrl}
	mock.recorder = &MockDownloaderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockDownloader) EXPECT() *MockDownloaderMockRecorder {
	return m.recorder
}

// Download mocks base method.
func (m *MockDownloader) Download(ctx context.Context, downloadDir string, firmware *serverservice.ComponentFirmwareVersion) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Download", ctx, downloadDir, firmware)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Download indicates an expected call of Download.
func (mr *MockDownloaderMockRecorder) Download(ctx, downloadDir, firmware any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Download", reflect.TypeOf((*MockDownloader)(nil).Download), ctx, downloadDir, firmware)
}
