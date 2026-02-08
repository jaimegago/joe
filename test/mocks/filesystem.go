// Package mocks provides mock implementations for testing
package mocks

import (
	"fmt"
	"sync"
)

// MockFileSystem provides a simple in-memory file system for testing
type MockFileSystem struct {
	files map[string][]byte
	mu    sync.RWMutex

	// Track operations for assertions
	ReadCount  int
	WriteCount int
}

// NewMockFileSystem creates a new mock filesystem
func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		files: make(map[string][]byte),
	}
}

// ReadFile reads a file from the mock filesystem
func (m *MockFileSystem) ReadFile(path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.ReadCount++

	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}

	return data, nil
}

// WriteFile writes a file to the mock filesystem
func (m *MockFileSystem) WriteFile(path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.WriteCount++
	m.files[path] = data
	return nil
}

// Delete removes a file from the mock filesystem
func (m *MockFileSystem) Delete(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.files[path]; !ok {
		return fmt.Errorf("file not found: %s", path)
	}

	delete(m.files, path)
	return nil
}

// Exists checks if a file exists in the mock filesystem
func (m *MockFileSystem) Exists(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.files[path]
	return ok
}

// ListFiles returns all file paths in the mock filesystem
func (m *MockFileSystem) ListFiles() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	paths := make([]string, 0, len(m.files))
	for path := range m.files {
		paths = append(paths, path)
	}
	return paths
}

// Clear removes all files from the mock filesystem
func (m *MockFileSystem) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.files = make(map[string][]byte)
	m.ReadCount = 0
	m.WriteCount = 0
}
