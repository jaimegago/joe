package mocks

import (
	"context"

	"github.com/jaimegago/joe/internal/adapters"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/store"
)

// MockGitAdapter implements git.GitAdapter for testing.
type MockGitAdapter struct {
	connected bool

	// Pre-configured responses
	ReadFileResult  string
	ListFilesResult []gitadapter.FileInfo
	LogResult       []gitadapter.CommitInfo
	DiffResult      string

	// Error injection
	ReadFileErr  error
	ListFilesErr error
	LogErr       error
	DiffErr      error
}

func NewMockGitAdapter() *MockGitAdapter {
	return &MockGitAdapter{connected: true}
}

func (m *MockGitAdapter) Connect(source store.Source) error { m.connected = true; return nil }
func (m *MockGitAdapter) Disconnect() error                 { m.connected = false; return nil }
func (m *MockGitAdapter) Status() adapters.Status {
	return adapters.Status{Connected: m.connected}
}

func (m *MockGitAdapter) ReadFile(_ context.Context, _ string) (string, error) {
	if m.ReadFileErr != nil {
		return "", m.ReadFileErr
	}
	return m.ReadFileResult, nil
}

func (m *MockGitAdapter) ListFiles(_ context.Context, _ string) ([]gitadapter.FileInfo, error) {
	if m.ListFilesErr != nil {
		return nil, m.ListFilesErr
	}
	return m.ListFilesResult, nil
}

func (m *MockGitAdapter) Log(_ context.Context, _ int) ([]gitadapter.CommitInfo, error) {
	if m.LogErr != nil {
		return nil, m.LogErr
	}
	return m.LogResult, nil
}

func (m *MockGitAdapter) Diff(_ context.Context, _, _ string) (string, error) {
	if m.DiffErr != nil {
		return "", m.DiffErr
	}
	return m.DiffResult, nil
}
