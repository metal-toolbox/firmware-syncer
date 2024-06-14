// Code generated by MockGen. DO NOT EDIT.
// Source: serverservice.go
//
// Generated by this command:
//
//	mockgen -source=serverservice.go -destination=mocks/serverservice.go ServerService
//
// Package mock_inventory is a generated GoMock package.
package mock_inventory

import (
	context "context"
	reflect "reflect"

	fleetdbapi "github.com/metal-toolbox/fleetdb/pkg/api/v1"
	gomock "go.uber.org/mock/gomock"
)

// MockServerService is a mock of ServerService interface.
type MockServerService struct {
	ctrl     *gomock.Controller
	recorder *MockServerServiceMockRecorder
}

// MockServerServiceMockRecorder is the mock recorder for MockServerService.
type MockServerServiceMockRecorder struct {
	mock *MockServerService
}

// NewMockServerService creates a new mock instance.
func NewMockServerService(ctrl *gomock.Controller) *MockServerService {
	mock := &MockServerService{ctrl: ctrl}
	mock.recorder = &MockServerServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockServerService) EXPECT() *MockServerServiceMockRecorder {
	return m.recorder
}

// Publish mocks base method.
func (m *MockServerService) Publish(ctx context.Context, newFirmware *fleetdbapi.ComponentFirmwareVersion) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Publish", ctx, newFirmware)
	ret0, _ := ret[0].(error)
	return ret0
}

// Publish indicates an expected call of Publish.
func (mr *MockServerServiceMockRecorder) Publish(ctx, newFirmware any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Publish", reflect.TypeOf((*MockServerService)(nil).Publish), ctx, newFirmware)
}