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
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessiongate"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// GateRefusalError is returned by DurableExecutor when the §C
// captain-session gate refuses a T2/T3 mutation. The LLM-facing layer
// surfaces this as the §A4 finding/annotation path: synthesize a
// finding and post it to CaptainSessionID's timeline, rather than
// retry. Empty CaptainSessionID means pending_captain (B2 null
// authority) — no captain to redirect to yet.
type GateRefusalError struct {
	SessionID        string
	Tool             string
	CaptainSessionID string
}

func (e *GateRefusalError) Error() string {
	if e.CaptainSessionID == "" {
		return fmt.Sprintf("durable: §C gate refused tool %q from session %q — "+
			"incident has no captain attached yet (§B2 null authority)",
			e.Tool, e.SessionID)
	}
	return fmt.Sprintf("durable: §C gate refused tool %q from session %q — "+
		"redirect to captain session %q (§A4 finding path)",
		e.Tool, e.SessionID, e.CaptainSessionID)
}

// DurableExecutor enforces the §D5 idempotency-key persist-before-issue
// protocol for joe-core's tool calls. Phase 1 Change 9.
//
// Sequence per T2/T3 tool call (the named structural ordering, asserted
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
// T1 bypass: read-only tools (TierObserve) skip the wrapper entirely.
// No key derived, no repo calls. Reads/discovery are §A1/§C1-free.
//
// No-run fallback: if the request context carries no run ID, the
// wrapper passes through to the inner executor without persisting.
// Change 10 will tighten this when the captain-session gate is wired.
//
// This wrapper is wired into joe-core's executor. Since Phase 2 the CLI
// runs no loop of its own, so joe-core's is the only agentic loop.
type DurableExecutor struct {
	inner    ToolExecutor
	repo     runmodel.Repository
	sessRepo sessionmodel.Repository // for the §C captain-session gate (Change 10)
}

// NewDurableExecutor wraps inner with §D5 idempotency-key persistence
// and the §C captain-session gate. inner is typically *tools.Executor
// (which satisfies ToolExecutor). sessRepo may be nil only in tests
// that deliberately skip the gate; production wiring in cmd/joe-core/
// main.go always passes the session-model repository so the gate cannot
// be disabled by a missing dependency.
func NewDurableExecutor(inner ToolExecutor, repo runmodel.Repository, sessRepo sessionmodel.Repository) *DurableExecutor {
	return &DurableExecutor{inner: inner, repo: repo, sessRepo: sessRepo}
}

// Execute satisfies ToolExecutor so the wrapper is a drop-in
// replacement for *tools.Executor.
//
// Pipeline order (Phase 1 Change 10 — §C2 / Invariant 5):
//
//  1. classify tool tier (safety.ClassifyTool).
//  2. T1 → bypass entirely (no gate, no persistence).
//  3. T2/T3:
//     a. §C gate (sessiongate.Check) — UPSTREAM of everything else.
//     Refusal returns *GateRefusalError; no IsAllowed call, no
//     RecordToolIntent, no inner.Execute.
//     b. §B1 principal substitution — in incident regime + Allow,
//     replace request-time principal with current captain's
//     principal so downstream PrincipalFromContext readers see
//     the captain's authority.
//     c. §D5 RecordToolIntent → inner.Execute → MarkToolCompleted
//     (same as Change 9).
//
// The C2 ordering test asserts that on the refusal path, no IsAllowed
// call ever fires. Spy harnesses in executor_gate_test.go verify the
// ordering structurally.
func (d *DurableExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	classification := safety.ClassifyTool(name)

	// T1 reads/discovery bypass the wrapper entirely — no key, no
	// persistence, no overhead. Asserted by TestDurableExecutor_T1Bypass.
	if classification.Tier == safety.TierObserve {
		return d.inner.Execute(ctx, name, args)
	}

	// T2/T3: anchor on the current run. Without a run ID we have nowhere
	// to persist the key. The gate still runs (it doesn't need a run);
	// the §D5 fallback for runID == "" sits below it.
	runID := agentctx.RunID(ctx)

	// --- §C captain-session gate (Change 10) ---
	//
	// The gate sits upstream of EVERYTHING else, including the §D5
	// persistence layer. On refusal we never call RecordToolIntent and
	// never invoke the inner executor — there is nothing to record
	// because nothing happened. The refusal carries the captain session
	// ID so the LLM-facing layer can synthesize a §A4 finding rather
	// than retry.
	//
	// Skipped only when sessRepo is nil (test-only carve-out; production
	// always wires it). The §C5 non-configurable floor test enumerates
	// config permutations and asserts the gate's refusal property holds
	// in all of them.
	if d.sessRepo != nil {
		sessionID := agentctx.SessionID(ctx)
		callerPrincipal := string(rbac.PrincipalFromContext(ctx))
		// sessionID may be empty for legacy callers; the gate handles
		// that via its "not the active incident session" branch in
		// incident regime, or via "always allow" in normal regime.
		decision, err := sessiongate.Check(ctx, d.sessRepo, sessionID, callerPrincipal, classification.Tier)
		if err != nil {
			return nil, fmt.Errorf("durable: §C gate: %w", err)
		}
		if !decision.Allow {
			return nil, &GateRefusalError{
				SessionID:        sessionID,
				Tool:             name,
				CaptainSessionID: decision.CaptainSessionID,
			}
		}

		// --- §B1 principal substitution ---
		//
		// On Allow in incident regime, replace request-time principal
		// with current captain's principal so downstream IsAllowed (or
		// any other PrincipalFromContext reader) sees the captain's
		// authority. In normal regime no substitution happens.
		regime, err := d.sessRepo.GetRegime(ctx)
		if err != nil {
			return nil, fmt.Errorf("durable: load regime for principal substitution: %w", err)
		}
		if regime != nil && regime.Mode == sessionmodel.RegimeModeIncident && sessionID != "" {
			captainPrincipal, ok, err := d.sessRepo.CurrentCaptainPrincipal(ctx, sessionID)
			if err != nil {
				return nil, fmt.Errorf("durable: load captain principal: %w", err)
			}
			if ok && captainPrincipal != "" {
				ctx = rbac.WithPrincipal(ctx, rbac.Principal(captainPrincipal))
			}
		}
	}

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
