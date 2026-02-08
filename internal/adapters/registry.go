package adapters

import (
	"fmt"
	"sync"
)

// Registry manages adapter instances by source ID.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry creates a new adapter registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]Adapter),
	}
}

// Get returns the adapter for the given source ID.
func (r *Registry) Get(sourceID string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.adapters[sourceID]
	if !ok {
		return nil, fmt.Errorf("adapter not found for source %q", sourceID)
	}
	return a, nil
}

// Register adds an adapter instance for the given source ID.
func (r *Registry) Register(sourceID string, adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[sourceID] = adapter
}

// Unregister removes and disconnects an adapter by source ID.
func (r *Registry) Unregister(sourceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.adapters[sourceID]
	if !ok {
		return nil
	}

	delete(r.adapters, sourceID)
	return a.Disconnect()
}

// List returns all registered source IDs.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.adapters))
	for id := range r.adapters {
		ids = append(ids, id)
	}
	return ids
}
