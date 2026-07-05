package adapters

import (
	"errors"
	"fmt"
	"sync"
)

var ErrAdapterNotFound = errors.New("adapter not found")

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
		return nil, fmt.Errorf("%w: source %q", ErrAdapterNotFound, sourceID)
	}
	return a, nil
}

// Register adds an adapter instance for the given source ID and returns the
// previously registered adapter it displaced (nil if none, or if the same
// instance is re-registered). The CALLER owns the displaced adapter's shutdown:
// Register runs under the registry lock and a Disconnect can block on network
// teardown, so it is not done here — but a displaced connected adapter still
// holds live resources (redis/postgres/mysql pools, mongodb monitor
// goroutines), so the caller must Disconnect it best-effort or those leak on
// every re-registration.
func (r *Registry) Register(sourceID string, adapter Adapter) Adapter {
	r.mu.Lock()
	defer r.mu.Unlock()
	displaced := r.adapters[sourceID]
	r.adapters[sourceID] = adapter
	if displaced == adapter {
		return nil
	}
	return displaced
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
