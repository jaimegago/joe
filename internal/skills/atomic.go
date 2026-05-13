package skills

import "sync/atomic"

// AtomicRouter is a goroutine-safe holder for the current *Router. The
// filesystem watcher publishes a freshly-built router via Set; reasoning code
// calls Snapshot() exactly once per chain and uses the returned pointer
// throughout. The router and its registry are treated as immutable once
// published, so a snapshot taken at the start of a chain stays consistent
// even if a reload swaps the underlying pointer mid-chain.
type AtomicRouter struct {
	p atomic.Pointer[Router]
}

// NewAtomicRouter wraps the given router. The router may be nil, in which
// case Snapshot() returns nil and Router.Match handles that safely.
func NewAtomicRouter(initial *Router) *AtomicRouter {
	a := &AtomicRouter{}
	if initial != nil {
		a.p.Store(initial)
	}
	return a
}

// Snapshot returns the currently-published router. Safe to call on a nil
// receiver, returning nil. Callers should treat the returned pointer as
// immutable.
func (a *AtomicRouter) Snapshot() *Router {
	if a == nil {
		return nil
	}
	return a.p.Load()
}

// Set publishes a new router. Subsequent Snapshot() calls observe it; chains
// already in flight retain the pointer they captured at the start.
func (a *AtomicRouter) Set(r *Router) {
	if a == nil {
		return
	}
	a.p.Store(r)
}
