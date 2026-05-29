// Package access is the single guarded seam through which all
// infrastructure-adapter and graph-store access must flow.
//
// Identity & Authentication design (docs/joe-identity-design.md §2.5),
// Phase A: enforcement moves off the HTTP transport into this accessor.
// Every dispatch method evaluates rbac.IsAllowed BEFORE resolving and
// calling the underlying adapter or graph store. On a denied decision the
// accessor returns ErrPermissionDenied and performs no infrastructure
// call; on a permitted decision it delegates and returns the result.
//
// The action for each operation is declared at the call site of the
// generic guard / permit helper, immediately adjacent to the adapter
// method it gates — NOT inferred from an HTTP verb. This keeps the action
// declaration next to the method's own semantics (design §2.8).
//
// Invariant (asserted by internal/access/guard_test.go and
// internal/api/access_guard_test.go): no package other than this one
// resolves an adapter via Registry.Get or calls a graph-store method, so
// there is exactly one path to reach an adapter or the graph store. The
// in-process Core Agent refresh path (internal/coreagent) is the single
// documented exception, deferred to Phase E (loopback removal); see the
// allowlist in api/access_guard_test.go.
package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// GraphSourceID is the reserved source identifier used when evaluating
// RBAC for graph-store operations. The graph store is not keyed by a real
// infrastructure source, so a stable reserved key is used; like any source
// with no zone assignment it resolves to the "unassigned" zone, which is
// exactly how the previous HTTP transport gated graph paths (their parsed
// path segments were likewise non-source strings resolving to unassigned).
const GraphSourceID = "graph"

// ErrPermissionDenied is returned when the policy engine denies a decision.
// It wraps no infrastructure error because, by contract, no infrastructure
// call is attempted on denial.
var ErrPermissionDenied = errors.New("access denied by RBAC policy")

// ErrSourceNotFound is returned when no adapter is registered for the
// requested source. It wraps store.ErrSourceNotFound so existing HTTP error
// mapping (errors.Is(err, store.ErrSourceNotFound) → 404) is preserved.
var ErrSourceNotFound = fmt.Errorf("%w", store.ErrSourceNotFound)

// ErrWrongAdapterType is returned when the registered adapter for a source
// does not implement the requested typed interface (e.g. asking for a
// Kubernetes operation on a Git source).
var ErrWrongAdapterType = errors.New("source is not the expected adapter type")

// ErrGraphUnavailable is returned when a graph operation is attempted but no
// graph store is wired (services.Graph == nil).
var ErrGraphUnavailable = errors.New("graph store not available")

// Accessor is the guarded seam in front of the adapter registry and the
// graph store. Construct it with New; it is safe for concurrent use (the
// registry and graph store it wraps are themselves concurrency-safe).
type Accessor struct {
	registry *adapters.Registry
	graph    graph.GraphStore
	// engine is the RBAC policy engine. A nil engine means RBAC is
	// disabled (auth not configured) and every decision is permitted —
	// mirroring rbac.EnforcementMiddleware(nil) on the HTTP transport, so
	// the accessor's decision is identical to the middleware's.
	engine *rbac.PolicyEngine
}

// New builds an Accessor over the given registry, graph store, and policy
// engine. engine may be nil (RBAC disabled).
func New(registry *adapters.Registry, graphStore graph.GraphStore, engine *rbac.PolicyEngine) *Accessor {
	return &Accessor{registry: registry, graph: graphStore, engine: engine}
}

// permit is the single enforcement chokepoint. It evaluates the policy
// engine for (principal, sourceID, action) and returns ErrPermissionDenied
// on a denied decision. A nil engine permits everything (RBAC disabled).
//
// permit performs NO infrastructure access: callers must invoke it before
// resolving or calling any adapter or graph method, so that a denial never
// touches infrastructure.
func (a *Accessor) permit(ctx context.Context, principal rbac.Principal, sourceID string, action rbac.Action) error {
	if a.engine == nil {
		return nil
	}
	if !a.engine.IsAllowed(ctx, principal, sourceID, action) {
		return fmt.Errorf("%w: principal=%q source=%q action=%q", ErrPermissionDenied, principal, sourceID, action)
	}
	return nil
}

// guard enforces (principal, sourceID, action), then resolves the adapter
// for sourceID and type-asserts it to T. It is the generic resolve path for
// every typed adapter dispatch method. On a denied decision it returns
// ErrPermissionDenied and never calls Registry.Get, so no infrastructure
// adapter is touched.
//
// guard is a package-level function (Go methods cannot be type-parameterised)
// but it is the ONLY code that calls a.registry.Get — the static guard test
// relies on that. typeName is a human-readable adapter kind used only for the
// ErrWrongAdapterType message.
func guard[T any](
	a *Accessor,
	ctx context.Context,
	principal rbac.Principal,
	sourceID string,
	action rbac.Action,
	typeName string,
) (T, error) {
	var zero T
	if err := a.permit(ctx, principal, sourceID, action); err != nil {
		return zero, err
	}
	adapter, err := a.registry.Get(sourceID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return zero, fmt.Errorf("%w: %s", ErrSourceNotFound, sourceID)
		}
		return zero, err
	}
	typed, ok := any(adapter).(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrWrongAdapterType, typeName)
	}
	return typed, nil
}
