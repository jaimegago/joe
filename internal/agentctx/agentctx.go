// Package agentctx carries the session/run/idempotency-key request
// context that joe's HTTP middleware (api.SessionMiddleware) reads
// from request headers and the durable executor wrapper
// (coreagent.DurableExecutor) consumes when persisting tool-call intent.
//
// Phase 1 Change 9 — see the Phase 1 decomposition plan.
//
// Lives in its own package to avoid an import cycle between
// internal/api (which writes the values) and internal/coreagent (which
// reads them).
package agentctx

import "context"

// Header names used by api.SessionMiddleware to populate context.
// Documented here so callers can set them without depending on the api
// package.
const (
	HeaderSessionID      = "X-Joe-Session-ID"
	HeaderRunID          = "X-Joe-Run-ID"
	HeaderIdempotencyKey = "X-Joe-Idempotency-Key"
)

type sessionIDKey struct{}
type runIDKey struct{}
type idempotencyKey struct{}
type taskIDKey struct{}

// WithSessionID returns a new context carrying the given session ID.
// Pairs with SessionID.
func WithSessionID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey{}, id)
}

// SessionID extracts the session ID from ctx, or "" if absent.
func SessionID(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithRunID returns a new context carrying the given run ID.
func WithRunID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, runIDKey{}, id)
}

// RunID extracts the run ID from ctx, or "" if absent.
func RunID(ctx context.Context) string {
	if v, ok := ctx.Value(runIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithIdempotencyKey returns a new context carrying the caller-supplied
// idempotency key. Used by coreagent.DurableExecutor when persisting
// tool-call intent — if absent, the wrapper computes a key from
// hash(runID, toolName, argsHash).
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKey{}, key)
}

// IdempotencyKey extracts the caller-supplied idempotency key from ctx,
// or "" if absent.
func IdempotencyKey(ctx context.Context) string {
	if v, ok := ctx.Value(idempotencyKey{}).(string); ok {
		return v
	}
	return ""
}

// WithTaskID returns a new context carrying the given task ID. Stream G
// phase G2: the task id is stamped into context by the agentic /tasks
// handler before agentloop.Agent.Run so the llmusage recorder can read it
// when persisting the per-call usage row. The non-agentic chat handler
// does not run inside a task and never sets this value (TaskID returns
// "" there, which the recorder maps to a NULL task_id column).
func WithTaskID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, taskIDKey{}, id)
}

// TaskID extracts the task ID from ctx, or "" if absent.
func TaskID(ctx context.Context) string {
	if v, ok := ctx.Value(taskIDKey{}).(string); ok {
		return v
	}
	return ""
}
