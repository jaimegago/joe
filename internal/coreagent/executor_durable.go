package coreagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/safety"
)

// DurableExecutor enforces the §D5 idempotency-key persist-before-issue
// protocol for joe's tool calls. Phase 1 Change 9.
//
// Phase G note: until Phase G this wrapper also hosted the §C
// captain-session gate + §B1 principal substitution. The gate has
// since moved to internal/captaingate (the shared wrapper used by BOTH
// the Core Agent and the user task loop — see
// docs/reference/joe-identity-design.md §0 bug #2 / §2.10 / §5 Invariant 6).
// Production composition is now `captaingate.Wrap(durable.Wrap(inner))`
// so the gate runs UPSTREAM of §D5 (a refused mutation is never
// persisted as an issued intent — nothing happened to record).
// DurableExecutor is now pure idempotency: look up the tool's
// NeedsDurability declaration, bypass if unset, else RecordToolIntent →
// inner.Execute → MarkToolCompleted.
//
// Sequence per mutating tool call (the named structural ordering, asserted
// by TestDurableExecutor_D5Ordering):
//
//  1. RecordToolIntent — persists the key as 'issued' BEFORE the tool runs.
//  2. inner.Execute — invokes the underlying executor on the caller's
//     goroutine. The no-goroutine-fan-out test asserts this.
//  3. MarkToolCompleted (or MarkToolFailed) — persists the terminal status
//     with the serialized result.
//
// Replay short-circuit: if RecordToolIntent returns a row whose status
// is already 'completed', the wrapper returns the cached result WITHOUT
// re-invoking the inner executor (the §D5 invariant). 'failed' status is
// also a terminal short-circuit: callers see the prior failure rather
// than a fresh attempt.
//
// Crash-resume permission: a key that's still 'issued' (the inner
// Execute crashed mid-call, never reached MarkToolCompleted) is allowed
// to re-run. RecordToolIntent is INSERT ... ON CONFLICT DO NOTHING and
// returns the existing row; the wrapper falls through to inner.Execute
// and lands the terminal status this time.
//
// Opt-in durability (D-0020 follow-up): the wrapper engages ONLY for tools
// whose classification declares NeedsDurability — i.e. non-idempotent
// creates/appends that a retry or crash-resume would duplicate. Every other
// tool (default OFF) bypasses the wrapper entirely: no key derived, no repo
// calls. This decision is INDEPENDENT of the Read/Mutate action class — "does
// this need crash-resume" is not "does this mutate the managed system". A
// Read create like register_component IS wrapped; a Mutate that is naturally
// idempotent is NOT.
//
// No-run fallback: if the request context carries no run ID, the
// wrapper passes through to the inner executor without persisting.
//
// This wrapper is wired into joe's executor. Since Phase 2 the CLI
// runs no loop of its own, so joe's is the only agentic loop.
type DurableExecutor struct {
	inner ToolExecutor
	repo  runmodel.Repository
}

// NewDurableExecutor wraps inner with §D5 idempotency-key persistence.
// inner is typically *tools.Executor (which satisfies ToolExecutor) or
// the *captaingate.Wrapper that hosts the §C gate. Production wiring in
// cmd/joe/server.go composes captaingate around DurableExecutor so a
// refused mutation never reaches persistence.
//
// Phase G: the constructor lost its sessRepo parameter when the §C gate
// moved out. Callers that used to pass a session-model repo now pass
// only (inner, repo); the gate is wired separately via
// captaingate.New(...).
func NewDurableExecutor(inner ToolExecutor, repo runmodel.Repository) *DurableExecutor {
	return &DurableExecutor{inner: inner, repo: repo}
}

// Execute satisfies ToolExecutor.
//
// Pipeline order (Phase 1 Change 9, post-Phase-G, D-0020 follow-up):
//
//  1. look up the tool's NeedsDurability declaration (safety.ClassifyTool).
//  2. not declared → bypass entirely (no persistence). Default OFF.
//  3. declared: §D5 RecordToolIntent → inner.Execute → MarkToolCompleted.
//
// The §C gate that used to live here is now in
// internal/captaingate.Wrapper, composed OUTSIDE this wrapper by
// cmd/joe/server.go. The Phase G TestPhaseG_GateThenAccessorOrdering
// test asserts captaingate runs upstream of (this) DurableExecutor and
// of the accessor.
func (d *DurableExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	classification := safety.ClassifyTool(name)

	// Opt-in durability (D-0020 follow-up): only tools that DECLARE
	// NeedsDurability are wrapped. Everything else — the high-frequency read
	// path and idempotent mutations alike — bypasses with no key, no
	// persistence, no overhead. This no longer consumes the Read/Mutate action
	// class: durability tracks "needs crash-resume" (a per-tool property), not
	// "mutates the managed system". Asserted by
	// TestDurableExecutor_UndeclaredBypass and TestDurableExecutor_DrivenByProperty.
	if !classification.NeedsDurability {
		return d.inner.Execute(ctx, name, args)
	}

	// T2/T3: anchor on the current run. Without a run ID we have nowhere
	// to persist the key.
	runID := agentctx.RunID(ctx)
	if runID == "" {
		return d.inner.Execute(ctx, name, args)
	}

	// Derive the key. Caller-supplied (via X-Joe-Idempotency-Key header)
	// wins; otherwise compute deterministically from runID + name + args
	// so the same intent produces the same key across retries.
	key := agentctx.IdempotencyKey(ctx)
	argsHash := canonicalArgsHash(args)
	if key == "" {
		key = computeIdempotencyKey(runID, name, argsHash)
	}

	// 1. RecordToolIntent — persists 'issued' BEFORE invoking the tool.
	intent, err := d.repo.RecordToolIntent(ctx, key, runID, name, argsHash)
	if err != nil {
		return nil, fmt.Errorf("durable: persist intent: %w", err)
	}

	// Replay short-circuit on terminal status.
	switch intent.Status {
	case runmodel.IdempotencyKeyStatusCompleted:
		return decodeResult(intent.Result), nil
	case runmodel.IdempotencyKeyStatusFailed:
		msg := "unknown error"
		if intent.Result != nil {
			msg = *intent.Result
		}
		return nil, fmt.Errorf("durable: prior call for idempotency key failed: %s", msg)
	}

	// 2. Invoke underlying on the CALLER'S goroutine. No `go func()`
	// wrap here — the no-goroutine-fan-out test asserts this.
	result, execErr := d.inner.Execute(ctx, name, args)

	// 3. Persist terminal status.
	if execErr != nil {
		if markErr := d.repo.MarkToolFailed(ctx, key, execErr.Error()); markErr != nil {
			// Persist failure was itself failing — surface the original
			// execErr; the next resume will see 'issued' status and may
			// re-attempt.
			return nil, execErr
		}
		return nil, execErr
	}

	resultJSON, encErr := json.Marshal(result)
	if encErr != nil {
		// Failed to serialize the result — record a marker so resume
		// doesn't loop forever, but surface the original result to the
		// caller. The next observer sees a non-nil terminal status.
		_ = d.repo.MarkToolCompleted(ctx, key, fmt.Sprintf("<unserializable result: %v>", encErr))
		return result, nil
	}
	if err := d.repo.MarkToolCompleted(ctx, key, string(resultJSON)); err != nil {
		// Best-effort: surface the result anyway. The key stays 'issued';
		// a future resume will hit the inner executor again, which the
		// underlying tools must already be designed to handle (the
		// §D5 invariant covers exactly this case).
		if !errors.Is(err, runmodel.ErrAlreadyTerminal) {
			return result, nil
		}
	}
	return result, nil
}

// computeIdempotencyKey produces a deterministic key from runID, tool
// name, and the canonical args hash. Same input → same key, so a
// retry from the same context lands on the same intent row.
func computeIdempotencyKey(runID, toolName, argsHash string) string {
	h := sha256.Sum256([]byte(runID + "|" + toolName + "|" + argsHash))
	return hex.EncodeToString(h[:])
}

// canonicalArgsHash serializes args with sorted keys so map iteration
// order doesn't perturb the hash.
func canonicalArgsHash(args map[string]any) string {
	if len(args) == 0 {
		return "0"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([][2]any, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, [2]any{k, args[k]})
	}
	b, _ := json.Marshal(ordered)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// decodeResult unmarshals the persisted result JSON if present, falling
// back to nil. Tools return arbitrary shapes; the wrapper round-trips
// through json so the cached value at least matches the JSON-encoded
// shape the caller originally saw.
func decodeResult(stored *string) any {
	if stored == nil {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(*stored), &v); err != nil {
		return *stored
	}
	return v
}
