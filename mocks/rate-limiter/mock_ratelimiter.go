// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/rudderlabs/rudder-server/rate-limiter (interfaces: RateLimiter)

// Package mocks_ratelimiter is a generated GoMock package.
package mocks_ratelimiter

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockRateLimiter is a mock of RateLimiter interface.
type MockRateLimiter struct {
	ctrl     *gomock.Controller
	recorder *MockRateLimiterMockRecorder
}

// MockRateLimiterMockRecorder is the mock recorder for MockRateLimiter.
type MockRateLimiterMockRecorder struct {
	mock *MockRateLimiter
}

// NewMockRateLimiter creates a new mock instance.
func NewMockRateLimiter(ctrl *gomock.Controller) *MockRateLimiter {
	mock := &MockRateLimiter{ctrl: ctrl}
	mock.recorder = &MockRateLimiterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRateLimiter) EXPECT() *MockRateLimiterMockRecorder {
	return m.recorder
}

// LimitReached mocks base method.
func (m *MockRateLimiter) LimitReached(arg0 string) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LimitReached", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// LimitReached indicates an expected call of LimitReached.
func (mr *MockRateLimiterMockRecorder) LimitReached(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LimitReached", reflect.TypeOf((*MockRateLimiter)(nil).LimitReached), arg0)
}
