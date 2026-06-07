// Package captaingate is the SHARED tool-executor wrapper that enforces
// the §C captain-session mutation gate (and the §B1 principal
// substitution) on every agentic tool call — joe's Core-Agent loop
// (onboarding/refresh) AND the user task loop (`agentloop.Agent.Run`
// behind /api/v1/tasks and /api/v1/tasks/stream).
//
// Why this lives here and not in coreagent (Phase G, see
// docs/joe-identity-design.md §0 bug #2, §2.10, §5 Invariant 6):
// before Phase G the gate lived in *coreagent.DurableExecutor, which
// wraps only the Core Agent's executor. The user task loop used a plain
// *tools.Executor with no gate — exactly the wiring the design called
// out as "the incident-mode design is unenforced on the path that
// matters." Moving the gate into a small standalone package and having
// both loops compose it gives one shared §C implementation that cannot
// drift between the two paths.
//
// The gate is and remains DENY-ONLY (design §2.10, settled invariant):
// in incident regime it can refuse a non-captain mutation, but it never
// grants authority a principal does not have. The accessor's RBAC check
// runs unchanged after this gate — gate-then-accessor. RBAC outcomes for
// a fixed principal/action/zone are identical whether or not the regime
// is incident; the gate only constrains WHICH SESSION may attempt a
// mutation, not WHAT AUTHORITY it has.
//
// The gate logic itself is the pure function sessiongate.Check. This
// package is the executor-shaped adapter around it: it classifies the
// tool's tier, calls Check, returns a typed GateRefusalError on refusal
// (no inner Execute), performs the §B1 principal substitution on allow,
// and writes ONE append-only audit row per refusal — the per-Phase-F
// requirement that "a loop-path gate refusal is observable in the audit
// trail." Allowed calls are silent here because the accessor will write
// its own infra_access row downstream; double-writing would inflate the
// log without adding information.
package captaingate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessiongate"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/tools"
)

// GateRefusalError is returned when the §C captain-session gate refuses
// a T2/T3 mutation. The LLM-facing layer surfaces this as the §A4
// finding/annotation path: synthesize a finding and post it to
// CaptainSessionID's timeline, rather than retry. Empty CaptainSessionID
// means pending_captain (§B2 null authority) — no captain to redirect
// to yet.
//
// Phase G note: this type used to live in internal/coreagent. It moved
// here when the gate was extracted into this shared package, because
// both the Core Agent and the user task loop now produce it.
type GateRefusalError struct {
	SessionID        string
	Tool             string
	CaptainSessionID string
}

func (e *GateRefusalError) Error() string {
	if e.CaptainSessionID == "" {
		return fmt.Sprintf("captaingate: §C gate refused tool %q from session %q — "+
			"incident has no captain attached yet (§B2 null authority)",
			e.Tool, e.SessionID)
	}
	return fmt.Sprintf("captaingate: §C gate refused tool %q from session %q — "+
		"redirect to captain session %q (§A4 finding path)",
		e.Tool, e.SessionID, e.CaptainSessionID)
}

// SingleExecutor is the minimal interface the wrapper delegates to per
// tool call. Both *tools.Executor and *coreagent.DurableExecutor (which
// also takes (ctx, name, args) -> (any, error)) satisfy it.
type SingleExecutor interface {
	Execute(ctx context.Context, name string, args map[string]any) (any, error)
}

// Wrapper applies the §C gate + §B1 substitution upstream of the inner
// executor's Execute. It also implements ExecuteBatch and
// ResultsToMessages so it can be dropped into agentloop.NewAgent in
// place of *tools.Executor — the loop only calls those two methods on
// its executor field, so providing both makes the wrapper a drop-in.
//
// The wrapper is intentionally simple: it does NOT call into rbac, does
// NOT compute zone or action, does NOT short-circuit RBAC. The
// accessor (internal/access) remains the authoritative RBAC point. This
// wrapper only answers the §C question: "from which session is this
// mutation arriving, and is that session the active incident's
// captain?" If the answer is no, the mutation never runs. If the answer
// is yes (or regime is normal), the mutation proceeds and the accessor
// makes its independent RBAC decision.
type Wrapper struct {
	inner    SingleExecutor
	sessRepo sessionmodel.Repository // nil ⇒ gate disabled (test-only carve-out)
	auditRep audit.Repository        // nil ⇒ refusal-audit disabled (test/dev)
}

// New constructs a Wrapper. sessRepo is the session-model repository
// that backs sessiongate.Check; auditRepo is the same audit.Repository
// the accessor writes through (so a gate refusal lands in the same
// append-only audit_log as every other event). Production wiring in
// cmd/joe/server.go always passes both; tests pass nil when they
// deliberately want to bypass the gate or the audit row.
func New(inner SingleExecutor, sessRepo sessionmodel.Repository, auditRepo audit.Repository) *Wrapper {
	return &Wrapper{inner: inner, sessRepo: sessRepo, auditRep: auditRepo}
}

// Execute is the §C-gated + §B1-substituted tool-call entry point.
//
// Pipeline order (Phase G — the same §C2 / Invariant 5 ordering Change
// 10 enforced for the Core Agent path, now applied to ALL agentic
// paths via this shared wrapper):
//
//  1. classify tool action (safety.ClassifyTool).
//  2. Read → bypass the gate entirely (reads/discovery are §A1/§C1-free).
//  3. Mutate:
//     a. §C gate (sessiongate.Check) — UPSTREAM of the inner executor's
//     safety check AND of the accessor's RBAC check. Refusal returns
//     *GateRefusalError; no inner.Execute call, no accessor call.
//     b. On refusal: write ONE captain_transition audit row with
//     action=captain_gate_refused, decision=deny. Fail-closed per
//     Phase F's posture for mutating actions (audit fail ⇒ surface
//     the audit error; the mutation still does not run because the
//     gate already refused it).
//     c. §B1 principal substitution — in incident regime + Allow,
//     replace the request-time principal in ctx with the current
//     captain's principal so downstream PrincipalFromContext readers
//     (the accessor, anything that reads ctx) see the captain's
//     authority.
//     d. inner.Execute on the same goroutine.
//
// Test coverage of this ordering lives in captaingate_test.go (the
// migrated equivalents of the Change-10 executor_gate_test.go tests).
func (w *Wrapper) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if w.sessRepo == nil {
		// Test-only carve-out: a wrapper built with a nil session repo
		// is the equivalent of "gate disabled" and behaves like a plain
		// inner executor. Production wiring never hits this branch.
		return w.inner.Execute(ctx, name, args)
	}

	classification := safety.ClassifyTool(name)

	// 1. Reads/discovery skip the gate.
	if classification.Class == safety.ActionRead {
		return w.inner.Execute(ctx, name, args)
	}

	sessionID := agentctx.SessionID(ctx)
	callerPrincipal := string(rbac.PrincipalFromContext(ctx))

	// 2. §C gate.
	decision, err := sessiongate.Check(ctx, w.sessRepo, sessionID, callerPrincipal, classification.Class)
	if err != nil {
		return nil, fmt.Errorf("captaingate: §C gate: %w", err)
	}
	if !decision.Allow {
		refusal := &GateRefusalError{
			SessionID:        sessionID,
			Tool:             name,
			CaptainSessionID: decision.CaptainSessionID,
		}
		// 2a. Write the durable refusal row. Phase F failure posture:
		// captain_gate_refused is not in the read-class, so a missing
		// audit row fails closed — we surface the audit error rather
		// than the refusal. Either way the inner.Execute is not
		// called, so the §C invariant (no non-captain mutation) holds.
		if err := w.writeRefusalAudit(ctx, callerPrincipal, sessionID, name, decision.CaptainSessionID); err != nil {
			return nil, fmt.Errorf("captaingate: audit write failed for refused mutation: %w", err)
		}
		return nil, refusal
	}

	// 3. §B1 principal substitution.
	regime, err := w.sessRepo.GetRegime(ctx)
	if err != nil {
		return nil, fmt.Errorf("captaingate: load regime for principal substitution: %w", err)
	}
	if regime != nil && regime.Mode == sessionmodel.RegimeModeIncident && sessionID != "" {
		captainPrincipal, ok, err := w.sessRepo.CurrentCaptainPrincipal(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("captaingate: load captain principal: %w", err)
		}
		if ok && captainPrincipal != "" {
			ctx = rbac.WithPrincipal(ctx, rbac.Principal(captainPrincipal))
		}
	}

	return w.inner.Execute(ctx, name, args)
}

// ExecuteBatch routes each call through w.Execute (the gated path).
// Mirrors *tools.Executor.ExecuteBatch's contract: returns a result for
// every call (with per-call errors captured in ToolCallResult.Error)
// and an overall ErrAllToolsFailed only when every call errored. The
// agentloop calls this method, so threading the gate through it is what
// makes the loop path enforce §C.
func (w *Wrapper) ExecuteBatch(ctx context.Context, calls []tools.ToolCallRequest) ([]tools.ToolCallResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	results := make([]tools.ToolCallResult, len(calls))
	errorCount := 0
	for i, call := range calls {
		result, err := w.Execute(ctx, call.Name, call.Args)
		results[i] = tools.ToolCallResult{
			ID:     call.ID,
			Name:   call.Name,
			Result: result,
			Error:  err,
		}
		if err != nil {
			errorCount++
		}
	}
	if errorCount == len(calls) {
		return results, fmt.Errorf("%w: %d tool(s) failed", tools.ErrAllToolsFailed, errorCount)
	}
	return results, nil
}

// ResultsToMessages mirrors *tools.Executor.ResultsToMessages — pure
// formatting, no executor state involved. Implemented here so the
// wrapper is a drop-in replacement for *tools.Executor in
// agentloop.NewAgent.
func (w *Wrapper) ResultsToMessages(results []tools.ToolCallResult) []llm.Message {
	messages := make([]llm.Message, len(results))
	for i, r := range results {
		messages[i] = tools.ResultToMessage(r)
	}
	return messages
}

// writeRefusalAudit records one captain_transition row of action
// captain_gate_refused for a §C-refused mutation. Phase F kept the
// audit_log.kind CHECK constraint to three values; rather than expand
// the schema for one new event class, gate refusals re-use the
// captain_transition kind — the gate IS a captain-mechanism event, so
// the existing kind is the natural home. Operators querying for
// captain-mechanism events get gate refusals alongside attaches and
// transfers without a schema change.
func (w *Wrapper) writeRefusalAudit(ctx context.Context, principal, sessionID, tool, captainSessionID string) error {
	if w.auditRep == nil {
		return nil
	}
	ctxBlob := map[string]string{
		"tool":               tool,
		"session_id":         sessionID,
		"captain_session_id": captainSessionID,
	}
	blob, _ := json.Marshal(ctxBlob)
	err := w.auditRep.Insert(ctx, audit.Event{
		Principal: principal,
		Action:    audit.ActionCaptainGateRefused,
		Decision:  audit.DecisionDeny,
		Reason:    audit.ReasonCaptainGateRefused,
		Kind:      audit.KindCaptainTransition,
		Context:   string(blob),
	})
	return audit.FailurePosture(ctx, audit.ActionCaptainGateRefused, err, "captaingate:refusal", audit.FailClosed)
}
