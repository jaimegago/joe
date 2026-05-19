// Package agentctx carries the session/run/idempotency-key request
// context that joe-core's HTTP middleware (api.SessionMiddleware) reads
// from request headers and the durable executor wrapper
// (coreagent.DurableExecutor) consumes when persisting tool-call intent.
//
// Phase 1 Change 9 — see docs/PHASE-1-DECOMPOSITION.md.
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
