package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jaimegago/joe/internal/llmusage"
)

// Stream G phase G5 — LLM usage view endpoints.
//
// Reads, available to any authenticated caller:
//   - GET /api/v1/llm/usage/sessions/{id}              — session breakdown
//   - GET /api/v1/llm/usage/aggregate                  — today/week/month rollup
//   - GET /api/v1/llm/usage/per-model                  — per-model over window
//
// Admin-gated:
//   - GET /api/v1/llm/usage/per-principal              — per-principal over window
//
// All endpoints go through new read-only repository methods on
// llmusage.Repository (SessionUsage / AggregateUsage / PerModelUsage /
// PerPrincipalUsage), separate from the pre-call enforcement gate's
// SumCostNano. The display surface and the enforcement surface MUST
// NOT share a query: enforcement is one indexed COALESCE-SUM on the
// hot path; display is GROUP BYs over a possibly-wide window. Sharing
// would couple every future shape change in the display to the
// enforcement read.
//
// The window query parameter on the per-model and per-principal
// endpoints accepts `hour`, `day`, or `month`, mirroring the cost-gate
// window vocabulary. An empty or unknown window defaults to `day`. A
// fuller "arbitrary range" surface would carry start/end times; that is
// out of scope for G5.

type usageBreakdownDTO struct {
	Calls             int64  `json:"calls"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	EstimatedCostNano int64  `json:"estimated_cost_nano"`
	Currency          string `json:"currency"`
	Model             string `json:"model,omitempty"`
	Principal         string `json:"principal,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
}

func toBreakdownDTOs(in []llmusage.UsageBreakdown) []usageBreakdownDTO {
	out := make([]usageBreakdownDTO, 0, len(in))
	for _, b := range in {
		out = append(out, usageBreakdownDTO{
			Calls:             b.Calls,
			InputTokens:       b.InputTokens,
			OutputTokens:      b.OutputTokens,
			EstimatedCostNano: b.EstimatedCostNano,
			Currency:          b.Currency,
			Model:             b.Model,
			Principal:         b.Principal,
			SessionID:         b.SessionID,
		})
	}
	return out
}

type llmUsageHandler struct{ server *Server }

func (s *Server) registerLLMUsageRoutes(mux *http.ServeMux, prefix string) {
	h := &llmUsageHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/llm/usage/sessions/{id}", prefix), h.handleSession)
	mux.HandleFunc(fmt.Sprintf("GET %s/llm/usage/aggregate", prefix), h.handleAggregate)
	mux.HandleFunc(fmt.Sprintf("GET %s/llm/usage/per-model", prefix), h.handlePerModel)
	mux.HandleFunc(fmt.Sprintf("GET %s/llm/usage/per-principal", prefix), h.handlePerPrincipal)
}

func (h *llmUsageHandler) repo() llmusage.Repository {
	if h.server.services == nil {
		return nil
	}
	return h.server.services.LLMUsage
}

func (h *llmUsageHandler) handleSession(w http.ResponseWriter, r *http.Request) {
	repo := h.repo()
	if repo == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm usage repository not available")
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "session id is required")
		return
	}
	rows, err := repo.SessionUsage(r.Context(), sessionID)
	if err != nil {
		writeInternalError(w, err, "llm usage session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"rows":       toBreakdownDTOs(rows),
	})
}

// handleAggregate emits three window rollups — today, this week, this
// month — in one response. Each window is a half-open [start, end)
// pair computed in UTC. The week window starts on Monday UTC (ISO week
// convention) and ends the following Monday; the day and month windows
// reuse llmusage.DayWindow / MonthWindow so the bounds agree with the
// gate's vocabulary.
func (h *llmUsageHandler) handleAggregate(w http.ResponseWriter, r *http.Request) {
	repo := h.repo()
	if repo == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm usage repository not available")
		return
	}
	now := time.Now().UTC()

	dayStart, dayEnd := llmusage.DayWindow(now)
	weekStart, weekEnd := weekWindow(now)
	monthStart, monthEnd := llmusage.MonthWindow(now)

	today, err := repo.AggregateUsage(r.Context(), dayStart, dayEnd)
	if err != nil {
		writeInternalError(w, err, "llm usage today")
		return
	}
	week, err := repo.AggregateUsage(r.Context(), weekStart, weekEnd)
	if err != nil {
		writeInternalError(w, err, "llm usage week")
		return
	}
	month, err := repo.AggregateUsage(r.Context(), monthStart, monthEnd)
	if err != nil {
		writeInternalError(w, err, "llm usage month")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"today": toBreakdownDTOs(today),
		"week":  toBreakdownDTOs(week),
		"month": toBreakdownDTOs(month),
	})
}

func (h *llmUsageHandler) handlePerModel(w http.ResponseWriter, r *http.Request) {
	repo := h.repo()
	if repo == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm usage repository not available")
		return
	}
	lower, upper, label := resolveWindow(r.URL.Query().Get("window"))
	rows, err := repo.PerModelUsage(r.Context(), lower, upper)
	if err != nil {
		writeInternalError(w, err, "llm usage per-model")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window": label,
		"rows":   toBreakdownDTOs(rows),
	})
}

func (h *llmUsageHandler) handlePerPrincipal(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	repo := h.repo()
	if repo == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "llm usage repository not available")
		return
	}
	lower, upper, label := resolveWindow(r.URL.Query().Get("window"))
	rows, err := repo.PerPrincipalUsage(r.Context(), lower, upper)
	if err != nil {
		writeInternalError(w, err, "llm usage per-principal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window": label,
		"rows":   toBreakdownDTOs(rows),
	})
}

// resolveWindow maps the `window` query parameter to a half-open
// [start, end) range. Unknown values fall back to `day` so a missing
// parameter is interpreted as "today" rather than an error — the
// per-model/per-principal endpoints are reads, and a default is
// kinder than a 400 for an exploratory caller.
func resolveWindow(q string) (lower, upper time.Time, label string) {
	now := time.Now().UTC()
	switch q {
	case "hour":
		l, u := llmusage.HourWindow(now)
		return l, u, "hour"
	case "month":
		l, u := llmusage.MonthWindow(now)
		return l, u, "month"
	default:
		l, u := llmusage.DayWindow(now)
		return l, u, "day"
	}
}

// weekWindow returns the half-open UTC range for the ISO week
// containing now (Monday-start). Centralised here rather than in the
// llmusage package because the cost-window gate has no concept of
// "week" — it would be dead code there. If a later phase needs a
// week boundary on the enforcement path, the constant moves; this
// handler does not have a load-bearing reason to define it elsewhere.
func weekWindow(now time.Time) (start, end time.Time) {
	u := now.UTC()
	// time.Weekday: Sunday=0..Saturday=6. ISO week starts on Monday,
	// so shift by (weekday+6) % 7 days back to land on Monday.
	wd := int(u.Weekday())
	delta := (wd + 6) % 7
	start = time.Date(u.Year(), u.Month(), u.Day()-delta, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 0, 7)
	return start, end
}
