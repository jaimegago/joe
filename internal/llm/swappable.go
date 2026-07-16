package llm

import (
	"context"
	"sync"
)

// SwappableAdapter wraps an LLMAdapter behind an RWMutex so the active model
// can be hot-swapped at runtime without restarting joe. After the Phase 2
// runtime collapse it is the single LLM contact point: the server-side agentic
// loop and the Web UI chat handler read through it, and the /model HTTP API
// swaps the inner adapter via Swap.
//
// Swap deliberately does NOT close the superseded adapter. The same underlying
// adapter instance may be shared with other consumers (e.g. background services
// that must stay on a stable model and must not follow chat-model swaps), so
// closing it on swap could break them. The bounded resource leak of a superseded
// provider client is acceptable given how rarely an operator switches models
// interactively.
//
// Chat snapshots the inner adapter under a read lock and then
// release it before issuing the (potentially long-running) call. A concurrent
// Swap therefore never blocks for the duration of an in-flight request: the
// in-flight call completes against the previous adapter while new calls use the
// new one. This is safe precisely because superseded adapters are not closed.
type SwappableAdapter struct {
	mu      sync.RWMutex
	inner   LLMAdapter
	current string // key of the active model (into config LLM.Available)
}

// NewSwappableAdapter wraps inner as the initially-active adapter. current is
// the model key reported by Current until the first Swap.
func NewSwappableAdapter(inner LLMAdapter, current string) *SwappableAdapter {
	return &SwappableAdapter{inner: inner, current: current}
}

// get returns the current inner adapter under a read lock.
func (s *SwappableAdapter) get() LLMAdapter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner
}

// Chat delegates to the active inner adapter.
func (s *SwappableAdapter) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return s.get().Chat(ctx, req)
}

// Swap replaces the active adapter and the reported current-model key. The
// previous adapter is intentionally not closed (see the type doc).
func (s *SwappableAdapter) Swap(inner LLMAdapter, current string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner = inner
	s.current = current
}

// Current returns the key of the active model.
func (s *SwappableAdapter) Current() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}
