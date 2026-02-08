package mocks

import (
	"context"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// MockK8sAdapter implements k8s.KubernetesAdapter for testing.
type MockK8sAdapter struct {
	connected bool

	// Pre-configured responses
	ListResult []unstructured.Unstructured
	GetResult  *unstructured.Unstructured
	LogsResult string

	// Error injection
	ListErr error
	GetErr  error
	LogsErr error
}

func NewMockK8sAdapter() *MockK8sAdapter {
	return &MockK8sAdapter{connected: true}
}

func (m *MockK8sAdapter) Connect(source store.Source) error { m.connected = true; return nil }
func (m *MockK8sAdapter) Disconnect() error                 { m.connected = false; return nil }
func (m *MockK8sAdapter) Status() adapters.Status {
	return adapters.Status{Connected: m.connected}
}

func (m *MockK8sAdapter) ListResources(ctx context.Context, resource, namespace string) ([]unstructured.Unstructured, error) {
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	return m.ListResult, nil
}

func (m *MockK8sAdapter) GetResource(ctx context.Context, resource, namespace, name string) (*unstructured.Unstructured, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	return m.GetResult, nil
}

func (m *MockK8sAdapter) GetPodLogs(ctx context.Context, namespace, pod, container string, tailLines int) (string, error) {
	if m.LogsErr != nil {
		return "", m.LogsErr
	}
	return m.LogsResult, nil
}
