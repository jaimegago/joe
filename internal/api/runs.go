package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/runmodel"
)

// runsHandler exposes the §D run lifecycle HTTP control plane —
// Phase 1 Change 7. See docs/PHASE-0-SESSION-MODEL.md §D and
// docs/PHASE-1-DECOMPOSITION.md Change 7.
//
// The state machine (legal transitions only):
//
//	running          → awaiting_input  (open solicitation)
//	running          → awaiting_world  (record world handle)
//	awaiting_input   → running         (resolve solicitation)
//	awaiting_world   → running         (observe handle with terminal state)
//	{running, awaiting_input, awaiting_world} → completed | failed | cancelled
//
// Illegal transitions return 409. The terminal states (completed,
// failed, cancelled) are sinks — no transition out.
//
// §D3 single-threaded: only one 'running' run per session, enforced by
// the partial unique index in migration 010. The handler surfaces the
// constraint violation as a 409 Conflict.
//
// §D5 idempotency keys: every endpoint that backs a world-mutating
// effect (world_handle record + observe) requires an idempotency_key in
// the request body. Phase 1 Change 7 enforces presence here; Change 9's
// executor wrapper is what actually calls runmodel.RecordToolIntent
// against this key.
//
// §D7 override forms: exactly three terminal endpoints — terminate
// (→ cancelled), complete (→ completed), fail (→ failed). NO
// "rewind"/"pre-effect" / "treat-as-never-happened" path exists; the
// route-enumeration test in runs_test.go enforces this.
//
// §D8 SITREP shape: GET /runs/{id} returns Run + open solicitation +
// world handles + action ledger + latest synthesized understanding
// (most recent reasoning step's payload). NO reasoning trace — the
// runSITREP response type omits a Steps field by construction.
type runsHandler struct {
	repo runmodel.Repository
}

func (s *Server) registerRunRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.RunModel == nil {
		return
	}
	h := &runsHandler{repo: s.services.RunModel}

	// Session-scoped: start a run.
	mux.HandleFunc(fmt.Sprintf("POST %s/agent-sessions/{id}/runs", prefix), h.startRun)

	// Run-scoped: SITREP, steps, transitions.
	mux.HandleFunc(fmt.Sprintf("GET %s/runs/{id}", prefix), h.getSITREP)
	mux.HandleFunc(fmt.Sprintf("POST %s/runs/{id}/steps", prefix), h.appendStep)
	mux.HandleFunc(fmt.Sprintf("POST %s/runs/{id}/solicitations", prefix), h.openSolicitation)
	mux.HandleFunc(fmt.Sprintf("POST %s/runs/{id}/world_handles", prefix), h.recordWorldHandle)
	mux.HandleFunc(fmt.Sprintf("POST %s/runs/{id}/world_handles/{hid}/observe", prefix), h.observeWorldHandle)

	// §D7 three override forms — and ONLY these three. The route-
	// enumeration test in runs_test.go asserts the full route set on
	// /api/v1/runs/{id}/ has exactly the endpoints registered above.
	mux.HandleFunc(fmt.Sprintf("POST %s/runs/{id}/terminate", prefix), h.terminate)
	mux.HandleFunc(fmt.Sprintf("POST %s/runs/{id}/complete", prefix), h.complete)
	mux.HandleFunc(fmt.Sprintf("POST %s/runs/{id}/fail", prefix), h.fail)

	// Solicitation resolution sits under /solicitations, not /runs, by
	// the decomposition's route layout. Resolves a single solicitation
	// and transitions its run back to 'running'.
	mux.HandleFunc(fmt.Sprintf("POST %s/solicitations/{id}/resolve", prefix), h.resolveSolicitation)
}

// --- POST /agent-sessions/{id}/runs ---

type startRunRequest struct {
	ID string `json:"id,omitempty"` // optional; server generates if absent
}

func (h *runsHandler) startRun(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeBadRequest(w, nil, "start run", "missing session id")
		return
	}
	var req startRunRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBadRequest(w, err, "start run", "invalid request body")
			return
		}
	}
	run := runmodel.Run{
		ID:        req.ID,
		SessionID: sessionID,
		State:     runmodel.RunStateRunning,
	}
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	created, err := h.repo.CreateRun(r.Context(), run)
	if err != nil {
		// §D3: the partial unique index on (session_id) WHERE state =
		// 'running' will reject a second running run on the same
		// session with a UNIQUE constraint violation. The SQLite/Postgres
		// error strings differ, so we surface as a generic 409 Conflict.
		// The structural property is asserted in
		// internal/runmodel/schema_test.go.
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "conflict",
				"a run is already running for this session (§D3 single-threaded)")
			return
		}
		writeInternalError(w, err, "start run")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// isUniqueViolation returns true if err looks like a unique-constraint
// violation from either SQLite (modernc.org/sqlite) or Postgres (pgx).
// Phase 1 Change 7: we string-match because neither driver exposes a
// stable typed error today. The §D3 test verifies the resulting 409.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// SQLite: "UNIQUE constraint failed: ..." or "constraint failed"
	// Postgres: "duplicate key value violates unique constraint ..."
	for _, needle := range []string{
		"UNIQUE constraint failed",
		"unique constraint",
		"duplicate key",
	} {
		if containsFold(msg, needle) {
			return true
		}
	}
	return false
}

func containsFold(s, substr string) bool {
	// Tiny case-insensitive substring check to avoid pulling in strings
	// just for this and to make the §D3 test stable across both drivers.
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
outer:
	for i := 0; i+len(substr) <= len(s); i++ {
		for j := 0; j < len(substr); j++ {
			a := s[i+j]
			b := substr[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				continue outer
			}
		}
		return true
	}
	return false
}

// --- GET /runs/{id} — §D8 SITREP ---

// runSITREP is the §D8 rehydration payload. The fields are an
// intentional allowlist: Run state, the latest synthesized
// understanding (NOT the reasoning trace), the open solicitation, the
// run's world handles, and the action ledger. There is no Steps field
// here — that would be the reasoning trace, which §D8 explicitly
// excludes.
type runSITREP struct {
	Run                      runmodel.Run           `json:"run"`
	SynthesizedUnderstanding *string                `json:"synthesized_understanding"`
	OpenSolicitation         *runmodel.Solicitation `json:"open_solicitation"`
	WorldHandles             []runmodel.WorldHandle `json:"world_handles"`
	ActionLedger             []runmodel.LedgerEntry `json:"action_ledger"`
}

func (h *runsHandler) getSITREP(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if runID == "" {
		writeBadRequest(w, nil, "get run", "missing run id")
		return
	}
	run, err := h.repo.GetRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "get run")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "run not found", map[string]any{"id": runID})
		return
	}

	resp := runSITREP{Run: *run}

	// Latest synthesized understanding: the most recent reasoning step's
	// payload, if any. §D8 excludes the full reasoning trace.
	steps, err := h.repo.ListStepsForRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "list steps for sitrep")
		return
	}
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Kind == runmodel.StepKindReasoning {
			p := steps[i].Payload
			resp.SynthesizedUnderstanding = &p
			break
		}
	}

	// Open solicitation: the most recent solicitation_open step whose
	// matching solicitation hasn't been resolved. Phase 1: at most one
	// open at a time (the run is in awaiting_input or it isn't).
	// We list steps and probe the solicitation rows referenced by the
	// most recent open. To avoid coupling to the step payload schema,
	// we scan run_solicitations directly via the repo — but the repo
	// doesn't expose a "list open solicitations for run" method yet.
	// Instead, we pull the open solicitation by scanning steps for the
	// latest solicitation_open kind and matching its payload's id field.
	// For Phase 1 simplicity, if the run state is awaiting_input we
	// walk steps backward to find the latest solicitation_open and
	// fetch it. (The pattern grows ugly if Phase 2 needs richer
	// queries; tighten there.)
	if run.State == runmodel.RunStateAwaitingInput {
		for i := len(steps) - 1; i >= 0; i-- {
			if steps[i].Kind != runmodel.StepKindSolicitationOpen {
				continue
			}
			var p struct {
				SolicitationID string `json:"solicitation_id"`
			}
			if err := json.Unmarshal([]byte(steps[i].Payload), &p); err != nil || p.SolicitationID == "" {
				continue
			}
			sol, err := h.repo.GetSolicitation(r.Context(), p.SolicitationID)
			if err != nil {
				writeInternalError(w, err, "load open solicitation")
				return
			}
			if sol != nil && sol.ResolvedAt == nil {
				resp.OpenSolicitation = sol
			}
			break
		}
	}

	// World handles: full list for this run; the SITREP shows what the
	// run is currently watching.
	handles, err := h.repo.ListWorldHandlesForRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "list world handles for sitrep")
		return
	}
	resp.WorldHandles = handles

	// Action ledger.
	ledger, err := h.repo.ListLedgerForRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "list ledger for sitrep")
		return
	}
	resp.ActionLedger = ledger

	writeJSON(w, http.StatusOK, resp)
}

// --- POST /runs/{id}/steps — generic step record ---

type appendStepRequest struct {
	StepNumber int64           `json:"step_number"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func (h *runsHandler) appendStep(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if runID == "" {
		writeBadRequest(w, nil, "append step", "missing run id")
		return
	}
	run, err := h.repo.GetRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "get run for step")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "run not found", nil)
		return
	}
	if isTerminal(run.State) {
		writeError(w, http.StatusConflict, "conflict",
			fmt.Sprintf("cannot append steps to terminal run (state=%s)", run.State))
		return
	}
	var req appendStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "append step", "invalid request body")
		return
	}
	if !isValidStepKind(req.Kind) {
		writeBadRequest(w, nil, "append step", "kind must be one of: reasoning, tool_call_intent, tool_call_result, solicitation_open, solicitation_resolved, world_handle_recorded, world_handle_observed")
		return
	}
	step := runmodel.Step{
		ID:         uuid.NewString(),
		RunID:      runID,
		StepNumber: req.StepNumber,
		Kind:       runmodel.StepKind(req.Kind),
		Payload:    string(req.Payload),
	}
	created, err := h.repo.AppendStep(r.Context(), step)
	if err != nil {
		writeInternalError(w, err, "append step")
		return
	}
	// Update last_step_id pointer on the run.
	_ = h.repo.SetLastStepID(r.Context(), runID, created.ID)
	writeJSON(w, http.StatusCreated, created)
}

// --- POST /runs/{id}/solicitations ---
//
// Validates §D awaiting_input taxonomy payload requirements:
//   - decision      → payload.options must be a non-empty array (bounded choice).
//   - provide_data  → payload.liveness must be 'attached_human_now' or 'out_of_band_human_work'.
//   - confirm_close → payload.action_ledger must be a (possibly empty) array snapshot.
//
// Transitions run state: running → awaiting_input.

type openSolicitationRequest struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

func (h *runsHandler) openSolicitation(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if runID == "" {
		writeBadRequest(w, nil, "open solicitation", "missing run id")
		return
	}
	run, err := h.repo.GetRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "get run for solicitation")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "run not found", nil)
		return
	}
	if run.State != runmodel.RunStateRunning {
		writeError(w, http.StatusConflict, "conflict",
			fmt.Sprintf("cannot open solicitation while run is %s (need 'running')", run.State))
		return
	}

	var req openSolicitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "open solicitation", "invalid request body")
		return
	}
	kind := runmodel.SolicitationKind(req.Kind)
	livenessFlag, err := validateSolicitationPayload(kind, req.Payload)
	if err != nil {
		writeBadRequest(w, err, "open solicitation", err.Error())
		return
	}

	sol := runmodel.Solicitation{
		ID:           uuid.NewString(),
		RunID:        runID,
		Kind:         kind,
		Payload:      string(req.Payload),
		LivenessFlag: livenessFlag,
	}
	created, err := h.repo.OpenSolicitation(r.Context(), sol)
	if err != nil {
		writeInternalError(w, err, "open solicitation")
		return
	}
	// Transition run: running → awaiting_input.
	if err := h.repo.UpdateRunState(r.Context(), runID, runmodel.RunStateAwaitingInput, nil); err != nil {
		writeInternalError(w, err, "transition run to awaiting_input")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// validateSolicitationPayload enforces the §D taxonomy. Returns the
// extracted liveness flag for 'provide_data' (used to persist on the
// solicitation row); errors otherwise.
func validateSolicitationPayload(kind runmodel.SolicitationKind, raw json.RawMessage) (*runmodel.LivenessFlag, error) {
	switch kind {
	case runmodel.SolicitationKindDecision:
		var p struct {
			Options []json.RawMessage `json:"options"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decision payload must include 'options' array: %w", err)
		}
		if len(p.Options) == 0 {
			return nil, fmt.Errorf("decision payload requires non-empty 'options' array (bounded choice)")
		}
		return nil, nil

	case runmodel.SolicitationKindProvideData:
		var p struct {
			Liveness string `json:"liveness"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("provide_data payload must include 'liveness' flag: %w", err)
		}
		switch p.Liveness {
		case string(runmodel.LivenessFlagAttachedHumanNow):
			f := runmodel.LivenessFlagAttachedHumanNow
			return &f, nil
		case string(runmodel.LivenessFlagOutOfBandHumanWork):
			f := runmodel.LivenessFlagOutOfBandHumanWork
			return &f, nil
		default:
			return nil, fmt.Errorf("provide_data 'liveness' must be 'attached_human_now' or 'out_of_band_human_work'")
		}

	case runmodel.SolicitationKindConfirmClose:
		var p struct {
			ActionLedger *json.RawMessage `json:"action_ledger"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("confirm_close payload must include 'action_ledger' snapshot: %w", err)
		}
		if p.ActionLedger == nil {
			return nil, fmt.Errorf("confirm_close payload requires 'action_ledger' snapshot field")
		}
		return nil, nil

	default:
		return nil, fmt.Errorf("kind must be one of: decision, provide_data, confirm_close")
	}
}

// --- POST /solicitations/{id}/resolve — transitions run back to running ---

type resolveSolicitationRequest struct {
	ResolutionPayload json.RawMessage `json:"resolution_payload"`
}

func (h *runsHandler) resolveSolicitation(w http.ResponseWriter, r *http.Request) {
	solicitationID := r.PathValue("id")
	if solicitationID == "" {
		writeBadRequest(w, nil, "resolve solicitation", "missing solicitation id")
		return
	}
	sol, err := h.repo.GetSolicitation(r.Context(), solicitationID)
	if err != nil {
		writeInternalError(w, err, "get solicitation")
		return
	}
	if sol == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "solicitation not found", nil)
		return
	}
	if sol.ResolvedAt != nil {
		writeError(w, http.StatusConflict, "conflict", "solicitation already resolved")
		return
	}

	var req resolveSolicitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "resolve solicitation", "invalid request body")
		return
	}
	now := time.Now().UTC()
	if err := h.repo.ResolveSolicitation(r.Context(), solicitationID, string(req.ResolutionPayload), now); err != nil {
		writeInternalError(w, err, "resolve solicitation")
		return
	}
	// Transition run: awaiting_input → running.
	run, err := h.repo.GetRun(r.Context(), sol.RunID)
	if err != nil {
		writeInternalError(w, err, "get run after resolve")
		return
	}
	if run != nil && run.State == runmodel.RunStateAwaitingInput {
		if err := h.repo.UpdateRunState(r.Context(), sol.RunID, runmodel.RunStateRunning, nil); err != nil {
			writeInternalError(w, err, "transition run to running")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"solicitation_id": solicitationID,
		"resolved_at":     now.Format(time.RFC3339),
	})
}

// --- POST /runs/{id}/world_handles — §D5 idempotency-key required ---

type recordWorldHandleRequest struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Locator        string          `json:"locator"`
	QueryMeta      json.RawMessage `json:"query_meta"`
}

func (h *runsHandler) recordWorldHandle(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	if runID == "" {
		writeBadRequest(w, nil, "record world handle", "missing run id")
		return
	}
	run, err := h.repo.GetRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "get run for world handle")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "run not found", nil)
		return
	}
	if run.State != runmodel.RunStateRunning {
		writeError(w, http.StatusConflict, "conflict",
			fmt.Sprintf("cannot record world handle while run is %s (need 'running')", run.State))
		return
	}

	var req recordWorldHandleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "record world handle", "invalid request body")
		return
	}
	if req.IdempotencyKey == "" {
		// §D5 contract: world-mutating endpoints REJECT requests without
		// an idempotency key. The executor wrapper in Change 9 actually
		// records intent; here we just enforce the field's presence.
		writeBadRequest(w, nil, "record world handle",
			"idempotency_key is required for world-mutating effects (§D5 invariant)")
		return
	}
	if req.Locator == "" {
		writeBadRequest(w, nil, "record world handle", "locator is required")
		return
	}

	handle := runmodel.WorldHandle{
		ID:        uuid.NewString(),
		RunID:     runID,
		Locator:   req.Locator,
		QueryMeta: string(req.QueryMeta),
	}
	created, err := h.repo.RecordWorldHandle(r.Context(), handle)
	if err != nil {
		writeInternalError(w, err, "record world handle")
		return
	}
	if err := h.repo.UpdateRunState(r.Context(), runID, runmodel.RunStateAwaitingWorld, nil); err != nil {
		writeInternalError(w, err, "transition run to awaiting_world")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// --- POST /runs/{id}/world_handles/{hid}/observe ---
//
// §D6 never-re-issue: the response carries the observed state and the
// resulting run state. It deliberately exposes NO field named retry,
// reissue, re_issue, or similar — a resume reads the persisted handle
// and re-queries the world; it never tells the caller to re-fire the
// underlying effect.

type observeWorldHandleRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	ObservedState  string `json:"observed_state"`
	Terminal       bool   `json:"terminal"`
}

// observeWorldHandleResponse is the §D6 / §D7 shape. The fields are an
// intentional allowlist; the test in runs_test.go asserts that no field
// named retry, reissue, re_issue, etc. is present.
type observeWorldHandleResponse struct {
	HandleID      string `json:"handle_id"`
	RunID         string `json:"run_id"`
	ObservedState string `json:"observed_state"`
	Terminal      bool   `json:"terminal"`
	RunState      string `json:"run_state"`
}

func (h *runsHandler) observeWorldHandle(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	handleID := r.PathValue("hid")
	if runID == "" || handleID == "" {
		writeBadRequest(w, nil, "observe world handle", "missing run id or handle id")
		return
	}
	var req observeWorldHandleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "observe world handle", "invalid request body")
		return
	}
	if req.IdempotencyKey == "" {
		writeBadRequest(w, nil, "observe world handle",
			"idempotency_key is required (§D5 invariant)")
		return
	}
	handle, err := h.repo.GetWorldHandle(r.Context(), handleID)
	if err != nil {
		writeInternalError(w, err, "get world handle")
		return
	}
	if handle == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "world handle not found", nil)
		return
	}
	if handle.RunID != runID {
		writeError(w, http.StatusConflict, "conflict", "world handle does not belong to this run")
		return
	}

	now := time.Now().UTC()
	if err := h.repo.ObserveWorldHandle(r.Context(), handleID, req.ObservedState, now); err != nil {
		writeInternalError(w, err, "observe world handle")
		return
	}

	resp := observeWorldHandleResponse{
		HandleID:      handleID,
		RunID:         runID,
		ObservedState: req.ObservedState,
		Terminal:      req.Terminal,
	}

	// If terminal, return run to 'running'. The handle stays recorded
	// (§D6: never re-issue; we keep the audit trail).
	run, _ := h.repo.GetRun(r.Context(), runID)
	if run != nil {
		if req.Terminal && run.State == runmodel.RunStateAwaitingWorld {
			if err := h.repo.UpdateRunState(r.Context(), runID, runmodel.RunStateRunning, nil); err != nil {
				writeInternalError(w, err, "transition run to running after terminal observe")
				return
			}
			resp.RunState = string(runmodel.RunStateRunning)
		} else {
			resp.RunState = string(run.State)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- §D7 three terminal forms ---

func (h *runsHandler) terminate(w http.ResponseWriter, r *http.Request) {
	h.terminalTransition(w, r, runmodel.RunStateCancelled)
}

func (h *runsHandler) complete(w http.ResponseWriter, r *http.Request) {
	h.terminalTransition(w, r, runmodel.RunStateCompleted)
}

func (h *runsHandler) fail(w http.ResponseWriter, r *http.Request) {
	h.terminalTransition(w, r, runmodel.RunStateFailed)
}

func (h *runsHandler) terminalTransition(w http.ResponseWriter, r *http.Request, target runmodel.RunState) {
	runID := r.PathValue("id")
	if runID == "" {
		writeBadRequest(w, nil, "terminal transition", "missing run id")
		return
	}
	run, err := h.repo.GetRun(r.Context(), runID)
	if err != nil {
		writeInternalError(w, err, "get run for terminal")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "run not found", nil)
		return
	}
	if isTerminal(run.State) {
		writeError(w, http.StatusConflict, "conflict",
			fmt.Sprintf("run already terminal (state=%s)", run.State))
		return
	}
	now := time.Now().UTC()
	if err := h.repo.UpdateRunState(r.Context(), runID, target, &now); err != nil {
		writeInternalError(w, err, "terminal transition")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run_id":   runID,
		"state":    string(target),
		"ended_at": now.Format(time.RFC3339),
	})
}

// --- helpers ---

func isTerminal(s runmodel.RunState) bool {
	switch s {
	case runmodel.RunStateCompleted, runmodel.RunStateFailed, runmodel.RunStateCancelled:
		return true
	}
	return false
}

func isValidStepKind(s string) bool {
	switch runmodel.StepKind(s) {
	case runmodel.StepKindReasoning,
		runmodel.StepKindToolCallIntent,
		runmodel.StepKindToolCallResult,
		runmodel.StepKindSolicitationOpen,
		runmodel.StepKindSolicitationResolved,
		runmodel.StepKindWorldHandleRecorded,
		runmodel.StepKindWorldHandleObserved:
		return true
	}
	return false
}
