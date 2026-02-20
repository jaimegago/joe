package notify

import "testing"

func TestNewService(t *testing.T) {
	svc := NewService()
	if svc == nil {
		t.Error("NewService() returned nil")
	}
}
