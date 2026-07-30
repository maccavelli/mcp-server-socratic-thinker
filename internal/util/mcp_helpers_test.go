package util

import (
	"testing"
)

func TestMockSessionProvider(t *testing.T) {
	mock := &MockSessionProvider{Srv: nil}
	if mock.MCPServer() != nil {
		t.Error("expected nil")
	}
	if mock.Session() != nil {
		t.Error("expected nil")
	}
}
