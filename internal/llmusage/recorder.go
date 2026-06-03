// Package llmusage records one llm_usage row per Chat call.
//
// The package wraps the raw llm.LLMAdapter at a single wire site in
// cmd/joe/server.go so every downstream consumer — the swappable
// hot-swap wrapper, the knowledge embedder, the doc drafter, the review
// agent, the Core Agent — invokes the recorder transparently through the
// same interface. The recorder reads the caller principal, session id,
// and task id from context and writes one row to the llm_usage table
// (migration 017) after a successful inner Chat returns.
//
// # Failure posture: fail-open, deliberately
//
// Recording is OBSERVABILITY, not enforcement. A failure to write a usage
// row must NEVER alter the outcome of an LLM call: the recorder logs the
// write failure at warn level with enough context to reconstruct the
// missing row from operational logs, then returns the inner adapter's
// real response and a nil error. This is the OPPOSITE posture from the
// audit writer (internal/audit), which is fail-closed on mutating
// actions because the audit row is the durable record OF the mutation.
// Usage rows describe a call that already happened; refusing the call
// because the after-the-fact bookkeeping failed would punish the caller
// for an operator's database problem.
//
// Do NOT harden the recorder to fail-closed in a future change. If
// usage gaps become operationally intolerable, fix the underlying store
// instead — the §4 split (mutations fail-closed, observability
// fail-open) is a deliberate design property.
//
// # Context cancellation: WithoutCancel on the write
//
// One production path — the review agent (internal/review) — dispatches
// the LLM call in a goroutine AFTER the originating HTTP request has
// returned. By the time recording runs, the request context is already
// cancelled, but the principal value still lives in the context. The
// recorder writes the row on a context derived with context.WithoutCancel
// from the call context: cancellation is dropped, but every value
// (principal, session, task) is preserved. This requires Go 1.21+
// (context.WithoutCancel landed in stdlib then); the repo is on Go 1.25+
// so the primitive is available.
package llmusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
)

// ErrCostLimitExceeded is the sentinel the pre-call cost-window gate
// returns when ANY configured window's accumulated spend is at or above
// its threshold. The error returned from Chat wraps this sentinel with
// a descriptive message naming the primary tripped window and the
// observed and limit values; callers test via errors.Is to distinguish
// a gated refusal (no tokens consumed, no llm_usage row written) from
// an inner adapter failure or the session-runaway sentinel
// agentloop.ErrSessionTokenCeiling. The two enforcement primitives —
// session token ceiling (G3a) and cost-window gate (G3b) — are
// deliberately separable.
var ErrCostLimitExceeded = errors.New("llmusage: cost limit exceeded")

// Window names used in the gate's refusal audit row context blob and
// in the descriptive error message. Centralised so the classifier in
// internal/api/tasks.go and operator dashboards do not depend on
// stringly-coupled magic strings scattered across this package.
const (
	WindowHourly  = "hourly"
	WindowDaily   = "daily"
	WindowMonthly = "monthly"
)

// RecorderAdapter wraps an inner llm.LLMAdapter and records one llm_usage
// row per Chat call. Provider, model, currency, and the USD-to-configured
// FX rate are supplied at construction time because the concrete
// adapter clients (claude.Client, gemini.Client) do not expose their
// model or provider identity — the wiring site in main.go already
// holds the active ModelConfig, so it is the single source of truth.
//
// The recorder is safe for concurrent use: it holds no mutable state
// beyond the construction-time fields, the (concurrency-safe) inner
// adapter and repository, and the sync.Once used for the once-only
// mixed-currency detector.
type RecorderAdapter struct {
	inner     llm.LLMAdapter
	repo      Repository
	costs     *CostTable
	limits    CostLimits
	auditRepo audit.Repository
	provider  string
	model     string
	currency  string
	fxRate    float64
	logger    *slog.Logger

	// mixedOnce drives the once-only mixed-currency detector: the
	// foreign-currency count is a property of the deployment's history,
	// not of any individual call. The first Chat (and only the first
	// Chat) runs the detection; subsequent calls skip it. The detector
	// is asynchronous — it never blocks the call path.
	mixedOnce sync.Once
}

// Config bundles the construction arguments for NewRecorderAdapter.
// Every field is required except Logger (defaults to slog.Default()),
// CostTable (defaults to a fresh table backed by the built-in price
// map), Limits (defaults to StaticCostLimits — the safe hardcoded
// backstop), and Audit (nil is tolerated, in which case gate refusal
// audit rows are skipped).
type Config struct {
	Inner    llm.LLMAdapter
	Repo     Repository
	Costs    *CostTable
	Provider string
	Model    string
	Currency string
	// FXRate is the multiplier applied to the USD-quoted source-currency
	// price to land on the configured currency. When Currency == "USD"
	// this is implicitly 1.0; the recorder treats a zero rate as 1.0
	// for the USD == USD case.
	FXRate float64
	// Limits is the cost-window threshold provider consulted by the
	// pre-call gate. Nil or omitted installs StaticCostLimits — the
	// safe hardcoded backstop — so the gate cannot be silently
	// disabled by an accidentally-nil option argument. The storage-
	// backed implementation arriving in a later phase satisfies the
	// same interface and is dropped in here at the construction site.
	Limits CostLimits
	// Audit is the append-only audit sink the gate writes refusal,
	// gate-read-failure, and mixed-currency rows to. nil is tolerated
	// in dev / tests — gate enforcement still happens — matching the
	// captaingate package's nil-tolerant posture for refusal audit.
	Audit  audit.Repository
	Logger *slog.Logger
}

// NewRecorderAdapter constructs a RecorderAdapter. Inner and Repo are
// required; the rest of the Config carries the identity / currency
// metadata stamped on every recorded row plus the construction-time
// seams for the cost-window gate.
func NewRecorderAdapter(cfg Config) *RecorderAdapter {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	costs := cfg.Costs
	if costs == nil {
		costs = NewCostTable()
	}
	// The gate's threshold provider defaults to the hardcoded safe
	// backstop so a wiring site that forgets to pass Limits — or
	// explicitly passes nil — still gets the gate enforced. This is
	// the same posture agentloop.NewAgent uses for SessionLimits.
	limits := cfg.Limits
	if limits == nil {
		limits = NewStaticCostLimits()
	}
	return &RecorderAdapter{
		inner:     cfg.Inner,
		repo:      cfg.Repo,
		costs:     costs,
		limits:    limits,
		auditRepo: cfg.Audit,
		provider:  cfg.Provider,
		model:     cfg.Model,
		currency:  cfg.Currency,
		fxRate:    cfg.FXRate,
		logger:    logger,
	}
}

// Chat delegates to the inner adapter, then — on a successful inner
// return — records exactly one llm_usage row. A non-nil inner error
// short-circuits with no row recorded and is returned unchanged. A
// recording failure is logged and swallowed; the inner response is
// returned with nil error (fail-open, see the package doc).
//
// Before delegating, Chat runs the Stream G phase G3b cost-window gate:
// it aggregates accumulated spend over the hourly, daily, and monthly
// windows directly from the table (no cache, so a process restart can
// not bypass the gate by losing in-memory state) and refuses the call
// when any window is at or above its configured limit. Refusal returns
// the typed sentinel ErrCostLimitExceeded wrapped with a descriptive
// message naming every tripped window, BEFORE the inner adapter is
// invoked — no tokens are consumed and no usage row is recorded for
// the refused call. The gate also fires the once-only mixed-currency
// detector on the first call, off the per-call enforcement path.
func (r *RecorderAdapter) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	r.mixedOnce.Do(func() { r.detectMixedCurrency(ctx) })
	if err := r.gate(ctx); err != nil {
		return nil, err
	}
	resp, err := r.inner.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	r.record(ctx, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	return resp, nil
}

// ChatStream calls through to the inner adapter and records nothing.
// Streaming is currently stubbed in both production provider clients,
// so there is no token usage to record; this method exists to satisfy
// the adapter interface transparently. When streaming lands, the
// per-chunk token tracking belongs in the inner adapter and the
// recorder reads the final Usage block from a terminal stream event.
func (r *RecorderAdapter) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return r.inner.ChatStream(ctx, req)
}

// Embed calls through to the inner adapter and records nothing. Embed
// is stubbed in both production provider clients and currently returns
// a not-implemented error; the recorder must NOT crash on that error
// and must return it transparently. This is the early exercise of
// fail-open: when there is no usage to record, the call's outcome is
// unaffected. When embeddings are implemented later, embedding token
// recording lands in this method.
func (r *RecorderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return r.inner.Embed(ctx, text)
}

// record builds and writes the per-call usage row. All input is taken
// from the call context (principal, session, task) and from the
// recorder's construction-time fields (provider, model, currency, fx
// rate). Errors are logged and swallowed.
func (r *RecorderAdapter) record(ctx context.Context, inputTokens, outputTokens int) {
	row := Row{
		Principal:    normalizePrincipal(rbac.PrincipalFromContext(ctx)),
		Model:        r.model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Currency:     r.currency,
		SessionID:    agentctx.SessionID(ctx),
		TaskID:       agentctx.TaskID(ctx),
	}

	if price, ok := r.costs.Lookup(r.provider, r.model); ok {
		if cost, valid := EstimateCostNano(price, inputTokens, outputTokens, r.fxRate); valid {
			row.EstimatedCostNano = cost
		} else {
			// Cost computation failed (FX rate invalid, source
			// currency not USD). Record the row at zero cost — the
			// gap surfaces in the table rather than the call vanishing
			// from usage entirely — and warn so the operator sees it.
			r.logger.Warn("llmusage: cost computation failed; recording zero cost",
				"provider", r.provider,
				"model", r.model,
				"currency", r.currency,
				"fx_rate", r.fxRate,
			)
		}
	} else {
		// Unknown provider/model: record the row with real token
		// counts and zero cost, then warn. Never blocks the call;
		// never silently omits the row. This makes the gap visible
		// in usage queries instead of hiding it.
		r.logger.Warn("llmusage: unpriced provider/model — recording zero cost",
			"provider", r.provider,
			"model", r.model,
		)
	}

	// Detach cancellation but preserve every context value (principal,
	// session, task, request-id) so the review-agent goroutine path can
	// still write its row after the originating HTTP request returned.
	writeCtx := context.WithoutCancel(ctx)
	if err := r.repo.Insert(writeCtx, row); err != nil {
		r.logger.Warn("llmusage: recording failed (fail-open, call returned successfully)",
			"error", err,
			"provider", r.provider,
			"model", r.model,
			"principal", row.Principal,
			"session_id", row.SessionID,
			"task_id", row.TaskID,
			"input_tokens", row.InputTokens,
			"output_tokens", row.OutputTokens,
			"currency", row.Currency,
		)
	}
}

// normalizePrincipal maps the principal-from-context into the audit
// convention: an empty principal AND the rbac.Unknown sentinel both map
// to the empty string, which the repository persists as SQL NULL. Every
// other value (user:..., svc:...) round-trips verbatim.
func normalizePrincipal(p rbac.Principal) string {
	s := string(p)
	if s == "" || p == rbac.Unknown {
		return ""
	}
	return s
}

// Compile-time check that RecorderAdapter implements llm.LLMAdapter.
// If the adapter interface grows, the build fails here — the failure is
// at the wire site, not at runtime.
var _ llm.LLMAdapter = (*RecorderAdapter)(nil)

// windowState bundles the per-window data the gate computes for one
// pre-call evaluation. Kept package-private; only the gate uses it.
type windowState struct {
	name     string
	limit    int64
	observed int64
	over     bool
	// readErr is set when the aggregation query for THIS window failed.
	// A non-nil readErr means the gate cannot decide on this window;
	// per the fail-open posture the gate proceeds (allows the call) and
	// emits one gate-read-failure warning + audit row.
	readErr error
}

// gate runs the pre-call cost-window check. It evaluates all three
// windows on EVERY call, reading live from the table (no cache). Any
// aggregation error fails OPEN — the call proceeds, with a loud
// warning and one gate-read-failure audit row — because a read failure
// that blocked every call would be a denial of service against the
// whole system including the operator's own path to intervene. See the
// fail-open comment block below.
//
// When at least one window is over its limit, the gate returns the
// typed ErrCostLimitExceeded sentinel wrapped with a message naming the
// primary tripped window plus its observed and limit values; one audit
// row of kind KindLLMLimitTriggered with action
// ActionLLMCostLimitRefused is written, whose structured context names
// every window that was over its limit (not only the first) along with
// the observed sums, the limits, the currency, and the session and
// task identifiers.
func (r *RecorderAdapter) gate(ctx context.Context) error {
	now := time.Now().UTC()
	hStart, hEnd := HourWindow(now)
	dStart, dEnd := DayWindow(now)
	mStart, mEnd := MonthWindow(now)
	windows := []struct {
		name  string
		start time.Time
		end   time.Time
		limit int64
	}{
		{WindowHourly, hStart, hEnd, r.limits.HourlyLimitNano()},
		{WindowDaily, dStart, dEnd, r.limits.DailyLimitNano()},
		{WindowMonthly, mStart, mEnd, r.limits.MonthlyLimitNano()},
	}

	states := make([]windowState, 0, len(windows))
	var anyReadErr error
	for _, w := range windows {
		// A non-positive limit disables this window's check (matches
		// the disable convention used by SessionLimits and documented
		// on the CostLimits interface). Skip without a read.
		if w.limit <= 0 {
			continue
		}
		sum, err := r.repo.SumCostNano(ctx, w.start, w.end, r.currency)
		if err != nil {
			anyReadErr = err
			states = append(states, windowState{name: w.name, limit: w.limit, readErr: err})
			continue
		}
		st := windowState{name: w.name, limit: w.limit, observed: sum, over: sum >= w.limit}
		states = append(states, st)
	}

	// FAIL-OPEN, LOUD on aggregation read error. This is the inverse
	// posture from everything else in the recorder. The reasoning is
	// the same asymmetric-risk logic that drove Rule 4: a read failure
	// that blocked every LLM call would be a denial of service against
	// the whole system — including the operator's own path to call
	// joe and intervene — whereas a read failure that lets calls
	// through degrades the cap temporarily and visibly. The failure
	// MUST be loud: emit a warning and write one gate-read-failure
	// audit row so a persistent gate outage shows up in the operational
	// log and audit table instead of disappearing silently. Do NOT
	// harden this to fail-closed in a future change; a database blip
	// would turn into a system-wide refusal.
	if anyReadErr != nil {
		r.logger.Warn("llmusage: cost-window gate read failed; allowing call (fail-open, loud)",
			"error", anyReadErr,
			"currency", r.currency,
		)
		r.writeGateReadFailureAudit(ctx, anyReadErr)
		// Do not return the read error; the gate's decision is to
		// allow the call to proceed.
		return nil
	}

	var tripped []windowState
	for _, st := range states {
		if st.over {
			tripped = append(tripped, st)
		}
	}
	if len(tripped) == 0 {
		return nil
	}

	primary := tripped[0]
	r.writeCostLimitRefusedAudit(ctx, tripped)
	return fmt.Errorf("cost-window gate refused: %s window observed %d >= limit %d (currency %s): %w",
		primary.name, primary.observed, primary.limit, r.currency, ErrCostLimitExceeded)
}

// writeCostLimitRefusedAudit records one KindLLMLimitTriggered row of
// action ActionLLMCostLimitRefused for a gate refusal. Best-effort: a
// nil repository (no audit wired in tests / dev) is tolerated and skips
// the write silently — the refusal has already been decided, so the
// audit row is forensic, not gating. On a real repository failure we
// route through audit.FailurePosture (fail-open-but-loud) so the loud
// failure log lands in operational logs, but we still return; the gate is
// already returning the cost-limit sentinel and the caller should see
// THAT error, not an internal audit error.
//
// The context blob carries every tripped window's name, observed sum,
// and limit — not only the primary one — so an operator can see whether
// one window or several tripped, plus the currency, session id, and
// task id.
func (r *RecorderAdapter) writeCostLimitRefusedAudit(ctx context.Context, tripped []windowState) {
	if r.auditRepo == nil {
		return
	}
	overList := make([]map[string]any, 0, len(tripped))
	for _, st := range tripped {
		overList = append(overList, map[string]any{
			"window":       st.name,
			"observed":     st.observed,
			"limit":        st.limit,
			"currency":     r.currency,
			"observed_per": float64(st.observed) / float64(st.limit),
		})
	}
	blob, _ := json.Marshal(map[string]any{
		"session_id":      agentctx.SessionID(ctx),
		"task_id":         agentctx.TaskID(ctx),
		"currency":        r.currency,
		"primary_window":  tripped[0].name,
		"tripped_windows": overList,
	})
	err := r.auditRepo.Insert(ctx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    audit.ActionLLMCostLimitRefused,
		Decision:  audit.DecisionDeny,
		Reason:    "cost_window_limit_exceeded",
		Kind:      audit.KindLLMLimitTriggered,
		Context:   string(blob),
	})
	_ = audit.FailurePosture(ctx, audit.ActionLLMCostLimitRefused, err, "llmusage:cost_gate_refused", audit.FailOpen)
}

// writeGateReadFailureAudit records one KindLLMLimitTriggered row when
// the gate's aggregation query itself errored. Decision is "allow"
// because the gate's policy on read failure is fail-open: the call is
// proceeding despite the missing aggregation. The row exists so a
// persistent gate outage is visible in the audit table; without it a
// silently-degraded gate would never surface. nil auditRepo skips the
// write — the warn log is the other half of the loud-failure signal
// and lands in operational logs regardless.
func (r *RecorderAdapter) writeGateReadFailureAudit(ctx context.Context, readErr error) {
	if r.auditRepo == nil {
		return
	}
	blob, _ := json.Marshal(map[string]any{
		"session_id": agentctx.SessionID(ctx),
		"task_id":    agentctx.TaskID(ctx),
		"currency":   r.currency,
		"error":      readErr.Error(),
	})
	err := r.auditRepo.Insert(ctx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    audit.ActionLLMCostLimitRefused,
		Decision:  audit.DecisionAllow,
		Reason:    "cost_window_gate_read_failed",
		Kind:      audit.KindLLMLimitTriggered,
		Context:   string(blob),
	})
	_ = audit.FailurePosture(ctx, audit.ActionLLMCostLimitRefused, err, "llmusage:cost_gate_read_failed", audit.FailOpen)
}

// detectMixedCurrency runs the once-only foreign-currency check at
// first use. The mixed-currency condition — the presence of usage rows
// whose currency differs from the currently configured currency — is a
// property of the deployment's history (the fingerprint of an operator
// changing the configured currency between deployments), NOT of any
// individual call, and per Rule 4 it deliberately does NOT block any
// call. Detection is decoupled from the per-call enforcement path: the
// gate sums ONLY same-currency rows for the comparison decision and
// never issues a foreign-currency count.
//
// On a positive detection the recorder emits a warning and writes one
// audit row tagged with a mixed_currency reason and decision = allow,
// naming the configured currency. Any error from the underlying count
// query is logged at debug — the detector is purely forensic and a
// missing detection must not stop the gate from running.
func (r *RecorderAdapter) detectMixedCurrency(ctx context.Context) {
	if r.currency == "" {
		return
	}
	// Use a detached context: the first Chat may be cancelled (review-
	// agent path) but the once-only detector should still publish its
	// signal. WithoutCancel preserves principal/session/task for the
	// audit row.
	detectCtx := context.WithoutCancel(ctx)
	n, err := r.repo.CountForeignCurrency(detectCtx, r.currency)
	if err != nil {
		r.logger.Debug("llmusage: mixed-currency detection query failed; skipping forensic signal",
			"error", err,
			"currency", r.currency,
		)
		return
	}
	if n == 0 {
		return
	}
	r.logger.Warn("llmusage: mixed-currency rows detected in llm_usage history; gate sums only configured-currency rows",
		"configured_currency", r.currency,
		"foreign_row_count", n,
	)
	if r.auditRepo == nil {
		return
	}
	blob, _ := json.Marshal(map[string]any{
		"configured_currency": r.currency,
		"foreign_row_count":   n,
	})
	insertErr := r.auditRepo.Insert(detectCtx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(detectCtx)),
		Action:    audit.ActionLLMCostLimitRefused,
		Decision:  audit.DecisionAllow,
		Reason:    "mixed_currency_history_detected",
		Kind:      audit.KindLLMLimitTriggered,
		Context:   string(blob),
	})
	_ = audit.FailurePosture(detectCtx, audit.ActionLLMCostLimitRefused, insertErr, "llmusage:mixed_currency", audit.FailOpen)
}
