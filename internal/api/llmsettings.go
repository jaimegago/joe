package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmsettings"
)

// Stream G phase G5 — LLM settings HTTP API.
//
// Reads: GET /api/v1/llm/settings returns the current active model,
// per-window cost limits, and runaway ceiling. Available to any
// authenticated caller because none of those values is sensitive
// (they are policy knobs, not credentials).
//
// Writes: three admin-gated mutators that route directly through
// services.LLMSettings (the existing mutation service), which already
// persists the value AND writes the audit row in one transaction.
// SetActiveModel goes through the SAME mutation-service method
// internal/api/models.go::handleSetCurrent now uses, so there is
// exactly one persisted-and-audited path for an active-model change
// — settings and admin endpoints share it.
//
// Unset-vs-configured labelling. The storage-backed CostLimits /
// SessionLimits providers (internal/llmsettings/providers.go)
// reinterpret a stored ZERO as "unset, fall back to the hardcoded
// backstop"; a stored NEGATIVE is the explicit "disabled" sentinel the
// gate honours; a stored POSITIVE is the configured limit. The GET
// response carries the raw stored value AND a configured-vs-backstop
// label computed from that convention so the frontend can render
// "unlimited (backstop)" for an unset window without re-implementing
// the predicate. The label kinds are pinned in the constants below.

const (
	// LimitStateBackstop labels a raw stored zero — the migration seed
	// — which the storage-backed provider replaces with the hardcoded
	// backstop. The displayed limit is the backstop, not the stored
	// value. A consumer rendering "current limit" reads the backstop
	// (which the response also carries) rather than zero.
	LimitStateBackstop = "backstop_fallback"
	// LimitStateConfigured labels a raw stored value that is NOT zero
	// — operator-set, possibly negative. A negative value means the
	// operator explicitly cleared the window's limit (the gate's
	// zero-or-below disable convention trips on a negative read); a
	// positive value is the configured limit. Either way the stored
	// value is what the gate uses.
	LimitStateConfigured = "configured"
)

type llmSettingsHandler struct{ server *Server }

func (s *Server) registerLLMSettingsRoutes(mux *http.ServeMux, prefix string) {
	h := &llmSettingsHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/llm/settings", prefix), h.handleGet)
	mux.HandleFunc(fmt.Sprintf("POST %s/llm/settings/active-model", prefix), h.handleSetActiveModel)
	mux.HandleFunc(fmt.Sprintf("POST %s/llm/settings/cost-limit", prefix), h.handleSetCostLimit)
	mux.HandleFunc(fmt.Sprintf("POST %s/llm/settings/runaway-ceiling", prefix), h.handleSetRunawayCeiling)
	mux.HandleFunc(fmt.Sprintf("POST %s/llm/settings/context-budget", prefix), h.handleSetContextBudget)
}

type costLimitView struct {
	Window    string `json:"window"`
	StoredRaw int64  `json:"stored_raw"`
	State     string `json:"state"`
	// Effective is the value the cost-window gate actually enforces for
	// this window right now, sourced from the SAME CostLimitsProvider the
	// recorder gates with (services.CostLimitsProvider) so the displayed
	// number cannot drift from the enforced one. When State is
	// "configured" it equals StoredRaw; when State is "backstop_fallback"
	// it is the hardcoded backstop the provider substitutes for the unset
	// (stored-zero) window — NOT zero. A negative StoredRaw (the
	// explicit-disable sentinel) reports an Effective of 0 because the
	// gate enforces nothing on a non-positive limit (see effectiveEnforced).
	Effective int64 `json:"effective"`
}

type runawayCeilingView struct {
	StoredRaw int    `json:"stored_raw"`
	State     string `json:"state"`
	// Effective is the session token ceiling agentloop.Agent.Run actually
	// enforces, sourced from the SAME SessionLimitsProvider the loop reads
	// (services.SessionLimitsProvider). Same convention as costLimitView:
	// the substituted backstop for an unset (zero) ceiling, the stored
	// value for a configured positive one, and 0 for a negative
	// explicit-disable (the loop's "ceiling > 0" guard enforces nothing).
	Effective int `json:"effective"`
}

// contextBudgetView mirrors the cost-limit / runaway-ceiling views: the raw
// stored fraction, the unset-vs-configured label, and the EFFECTIVE fraction
// the agentic path actually budgets with — sourced from the SAME
// ContextBudgetProvider buildTaskRun reads, so the displayed number cannot
// drift from the enforced one. A stored zero (unset) reports the
// backstop-substituted default fraction as Effective.
type contextBudgetView struct {
	StoredRaw float64 `json:"stored_raw"`
	State     string  `json:"state"`
	Effective float64 `json:"effective"`
}

type llmSettingsResponse struct {
	ActiveModel    string             `json:"active_model"`
	CostLimits     []costLimitView    `json:"cost_limits"`
	RunawayCeiling runawayCeilingView `json:"runaway_ceiling"`
	ContextBudget  contextBudgetView  `json:"context_budget"`
}

// stateForLimit applies the documented backstop convention: a stored
// ZERO labels as backstop fallback; ANY other value (positive limit or
// negative explicit-disable) labels as operator-configured. This is
// the SAME predicate the storage-backed providers use to decide
// whether to substitute the backstop value, expressed here so the
// labelling cannot drift.
func stateForLimit(rawNonZero bool) string {
	if rawNonZero {
		return LimitStateConfigured
	}
	return LimitStateBackstop
}

// effectiveEnforced maps a provider-returned limit to the number the
// enforcement gate actually applies, for display. The enforcement
// convention is identical at both gate sites — the cost-window gate
// (internal/llmusage RecorderAdapter.gate skips any window whose limit
// is <= 0) and the runaway ceiling (agentloop.Agent.Run enforces only
// when ceiling > 0): a value of zero or below disables the check, so the
// window enforces NOTHING. We surface that as a zero effective limit —
// "no limit in force" — rather than echoing a raw negative sentinel that
// a client would misread as a negative threshold. A positive provider
// value (a configured limit OR a substituted backstop) is the number
// actually enforced and passes through unchanged. Sourcing the input
// from the live provider, not a re-derived constant, is what keeps the
// displayed number pinned to the enforced one.
func effectiveEnforced[T int | int64](providerValue T) T {
	if providerValue > 0 {
		return providerValue
	}
	return 0
}

func (h *llmSettingsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if h.server.services == nil || h.server.services.LLMSettings == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm settings service not available")
		return
	}
	repo := h.server.services.LLMSettings.Repo()
	if repo == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm settings repository not available")
		return
	}
	// The effective enforced value is read from the live enforcement
	// providers, not re-derived from the backstop constants here, so the
	// displayed number is the one the gate decides with. Both are wired
	// in cmd/joe/server.go; their absence means the instrumentation
	// stack is not fully up, so report unavailable rather than guess.
	costProvider := h.server.services.CostLimitsProvider
	sessionProvider := h.server.services.SessionLimitsProvider
	budgetProvider := h.server.services.ContextBudgetProvider
	if costProvider == nil || sessionProvider == nil || budgetProvider == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm limits providers not available")
		return
	}

	active, err := repo.ReadActiveModel(r.Context())
	if err != nil {
		writeInternalError(w, err, "llm settings read active model")
		return
	}
	limits, err := repo.ReadCostLimits(r.Context())
	if err != nil {
		writeInternalError(w, err, "llm settings read cost limits")
		return
	}
	ceiling, err := repo.ReadRunawayCeiling(r.Context())
	if err != nil {
		writeInternalError(w, err, "llm settings read runaway ceiling")
		return
	}
	budget, err := repo.ReadContextBudget(r.Context())
	if err != nil {
		writeInternalError(w, err, "llm settings read context budget")
		return
	}

	resp := llmSettingsResponse{
		ActiveModel: active,
		CostLimits: []costLimitView{
			{Window: llmsettings.WindowHourly, StoredRaw: limits.HourlyNano, State: stateForLimit(limits.HourlyNano != 0), Effective: effectiveEnforced(costProvider.HourlyLimitNano())},
			{Window: llmsettings.WindowDaily, StoredRaw: limits.DailyNano, State: stateForLimit(limits.DailyNano != 0), Effective: effectiveEnforced(costProvider.DailyLimitNano())},
			{Window: llmsettings.WindowMonthly, StoredRaw: limits.MonthlyNano, State: stateForLimit(limits.MonthlyNano != 0), Effective: effectiveEnforced(costProvider.MonthlyLimitNano())},
		},
		RunawayCeiling: runawayCeilingView{
			StoredRaw: ceiling,
			State:     stateForLimit(ceiling != 0),
			Effective: effectiveEnforced(sessionProvider.SessionTokenCeiling()),
		},
		ContextBudget: contextBudgetView{
			StoredRaw: budget,
			State:     stateForLimit(budget != 0),
			Effective: budgetProvider.BudgetFraction(),
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

type setActiveModelRequest struct {
	Name string `json:"name"`
}

// handleSetActiveModel admin-gates the same atomic-persist-and-audit
// path internal/api/models.go::handleSetCurrent uses. The two
// endpoints share the mutation service so there remains exactly one
// audited way to change the active model.
//
// Like handleSetCurrent, this endpoint validates the model name
// against the configured catalogue and constructs the adapter BEFORE
// the mutation runs — a missing API key for the new provider must
// not produce a phantom audit row of a swap that could not happen.
// On success the live SwappableAdapter is hot-swapped, so a settings
// write through the admin surface and a write through /models/current
// produce equivalent post-conditions.
func (h *llmSettingsHandler) handleSetActiveModel(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var req setActiveModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "set active model", "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "name is required")
		return
	}
	if h.server.services == nil || h.server.services.LLMSettings == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm settings service not available")
		return
	}
	cfg := h.server.services.Config
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "LLM config not available")
		return
	}
	mc, ok := cfg.LLM.Available[req.Name]
	if !ok {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, fmt.Sprintf("unknown model %q", req.Name))
		return
	}

	sw, ok := h.server.services.LLM.(*llm.SwappableAdapter)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "model switching not available")
		return
	}
	raw, err := newModelAdapter(r.Context(), mc)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, fmt.Sprintf("cannot switch to %q: %s", req.Name, err))
		return
	}
	// Wrap the raw client in the SAME recording / cost-gating chain the
	// boot path and /models/current install (services.BuildLLMChain).
	// The admin surface must produce an identical post-swap chain so a
	// model change here keeps usage recording and cost-gate enforcement.
	adapter := h.server.services.BuildLLMChain(raw, mc)
	// modelSwapMu makes persist+swap indivisible against the sibling switch
	// handler (models.go handleSetCurrent): without it two racing switches can
	// interleave the two writes and leave the live adapter disagreeing with the
	// persisted active model.
	h.server.modelSwapMu.Lock()
	defer h.server.modelSwapMu.Unlock()
	if err := h.server.services.LLMSettings.SetActiveModel(r.Context(), req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, errorCodeInternal, fmt.Sprintf("failed to persist active model: %s", err))
		return
	}
	sw.Swap(adapter, req.Name)
	writeJSON(w, http.StatusOK, map[string]any{"current": req.Name})
}

type setCostLimitRequest struct {
	Window string `json:"window"`
	// Value is the new stored value in nano-units of the configured
	// currency. The mutation service forwards the value unchanged to
	// the repository; the storage provider's backstop convention then
	// interprets zero as unset and negative as explicit-disable.
	Value int64 `json:"value"`
}

func (h *llmSettingsHandler) handleSetCostLimit(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var req setCostLimitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "set cost limit", "invalid request body")
		return
	}
	if req.Window == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "window is required")
		return
	}
	if h.server.services == nil || h.server.services.LLMSettings == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm settings service not available")
		return
	}
	if err := h.server.services.LLMSettings.SetCostLimit(r.Context(), req.Window, req.Value); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, fmt.Sprintf("failed to set cost limit: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"window": req.Window, "value": req.Value})
}

type setRunawayCeilingRequest struct {
	Value int `json:"value"`
}

func (h *llmSettingsHandler) handleSetRunawayCeiling(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var req setRunawayCeilingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "set runaway ceiling", "invalid request body")
		return
	}
	if h.server.services == nil || h.server.services.LLMSettings == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm settings service not available")
		return
	}
	if err := h.server.services.LLMSettings.SetRunawayCeiling(r.Context(), req.Value); err != nil {
		writeError(w, http.StatusInternalServerError, errorCodeInternal, fmt.Sprintf("failed to set runaway ceiling: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": req.Value})
}

type setContextBudgetRequest struct {
	// Fraction is the new context-budget fraction. Validated here to be in
	// the half-open range (0, 1]: a fraction of zero or below, or above 1.0,
	// is rejected with 400 before it reaches the mutation service. (A stored
	// zero remains a valid "unset" sentinel, but it is reached only via the
	// migration seed, never written through this endpoint — an operator
	// clearing the budget is not an expressible request.)
	Fraction float64 `json:"fraction"`
}

// handleSetContextBudget admin-gates the atomic persist-and-audit path for
// the context-budget fraction. Validation (0 < fraction <= 1.0) happens here
// so an out-of-range value is a 400 before the mutation runs.
func (h *llmSettingsHandler) handleSetContextBudget(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var req setContextBudgetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "set context budget", "invalid request body")
		return
	}
	if req.Fraction <= 0 || req.Fraction > 1.0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "fraction must be greater than 0 and at most 1.0")
		return
	}
	if h.server.services == nil || h.server.services.LLMSettings == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm settings service not available")
		return
	}
	if err := h.server.services.LLMSettings.SetContextBudget(r.Context(), req.Fraction); err != nil {
		writeError(w, http.StatusInternalServerError, errorCodeInternal, fmt.Sprintf("failed to set context budget: %s", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fraction": req.Fraction})
}
