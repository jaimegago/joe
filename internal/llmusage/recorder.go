// Package llmusage records one llm_usage row per Chat call.
//
// The package wraps the raw llm.LLMAdapter at a single wire site in
// cmd/joe-core/main.go so every downstream consumer — the swappable
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
	"errors"
	"log/slog"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
)

// ErrCostLimitExceeded is reserved for a later phase: the pre-call
// cost-window gate inserted just before the inner adapter call in
// RecorderAdapter.Chat. No code path returns this sentinel yet —
// declaring it now lets the downstream classifier add a single
// errors.Is case when the gate lands, and lets callers distinguish a
// gated refusal (no tokens consumed, no llm_usage row written) from an
// inner adapter failure or the session-runaway sentinel
// agentloop.ErrSessionTokenCeiling. The two enforcement primitives are
// deliberately separable.
var ErrCostLimitExceeded = errors.New("llmusage: cost limit exceeded")

// RecorderAdapter wraps an inner llm.LLMAdapter and records one llm_usage
// row per Chat call. Provider, model, currency, and the USD-to-configured
// FX rate are supplied at construction time because the concrete
// adapter clients (claude.Client, gemini.Client) do not expose their
// model or provider identity — the wiring site in main.go already
// holds the active ModelConfig, so it is the single source of truth.
//
// The recorder is safe for concurrent use: it holds no mutable state
// beyond the construction-time fields and the (concurrency-safe) inner
// adapter and repository.
type RecorderAdapter struct {
	inner    llm.LLMAdapter
	repo     Repository
	costs    *CostTable
	provider string
	model    string
	currency string
	fxRate   float64
	logger   *slog.Logger
}

// Config bundles the construction arguments for NewRecorderAdapter.
// Every field is required except Logger (defaults to slog.Default()) and
// CostTable (defaults to a fresh table backed by the built-in price map).
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
	Logger *slog.Logger
}

// NewRecorderAdapter constructs a RecorderAdapter. Inner and Repo are
// required; the rest of the Config carries the identity / currency
// metadata stamped on every recorded row.
func NewRecorderAdapter(cfg Config) *RecorderAdapter {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	costs := cfg.Costs
	if costs == nil {
		costs = NewCostTable()
	}
	return &RecorderAdapter{
		inner:    cfg.Inner,
		repo:     cfg.Repo,
		costs:    costs,
		provider: cfg.Provider,
		model:    cfg.Model,
		currency: cfg.Currency,
		fxRate:   cfg.FXRate,
		logger:   logger,
	}
}

// Chat delegates to the inner adapter, then — on a successful inner
// return — records exactly one llm_usage row. A non-nil inner error
// short-circuits with no row recorded and is returned unchanged. A
// recording failure is logged and swallowed; the inner response is
// returned with nil error (fail-open, see the package doc).
func (r *RecorderAdapter) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
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
