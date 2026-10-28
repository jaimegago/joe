package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/autorun"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/tools"
)

// autoRunHandler is the Auto Joe run surface (autojoe-ro-run-build): the HTTP
// side of the operator trigger and the schedules, and the loop factory that
// assembles a run's agent loop from the same parts the task loop is built from
// — with two differences that are the whole point.
//
// The accessor is the run path's OWN: built on the promote-aware engine the
// Core Agent refresh uses (rbac.NewPolicyEngineWithPromote, constructed at the
// composition root and passed to InstallAutoRun), never on the transport
// engine the HTTP handlers enforce with. The transport engine carries the
// read-posture resolver, under which the launch-default team_flat posture
// admits every authenticated human to read every component; the autonomous
// agent:core read surface is a separate axis governed solely by grants plus
// auto_promote_reads (read-posture-latch-02, D-0043). A run must see exactly
// what the refresher sees, so it reads through an accessor built exactly as
// the refresher's is.
//
// The principal is not the caller's. The run service stamps svc:agent:core on
// the run context before the factory runs, so every tool's adapter/graph
// access reaches this accessor with that principal — the activator who pressed
// the trigger is on the run record and nowhere on the context.
type autoRunHandler struct {
	server   *Server
	service  *autorun.Service
	accessor *access.Accessor
	inproc   *inProcessCoreClient
}

// InstallAutoRun wires the Auto Joe run service onto the server and returns
// it. runEngine is the run path's policy engine — the promote-aware,
// posture-less engine the refresher uses, built at the composition root
// (cmd/joe/server.go), nil when RBAC is disabled (permit-all, matching the
// transport accessor's nil-engine semantics). It must be called before
// RegisterRoutes for the routes to exist. It returns nil, and installs
// nothing, when the session store is not wired: a run's output is a session,
// and there is nowhere to write one.
func (s *Server) InstallAutoRun(runEngine *rbac.PolicyEngine, budget autorun.Budget) *autorun.Service {
	if s.services.SessionModel == nil || s.services.Store == nil {
		slog.Warn("auto run: not installed — session store not wired")
		return nil
	}
	var auditRepo audit.Repository
	if s.services.Audit != nil {
		auditRepo = s.services.Audit
	}
	runAccessor := access.New(s.services.Adapters, s.services.Graph, runEngine, auditRepo)
	h := &autoRunHandler{
		server:   s,
		accessor: runAccessor,
		inproc:   newInProcessCoreClient(runAccessor, s.services),
	}
	var zones autorun.ZoneAssignments
	if s.services.RBAC != nil {
		zones = s.services.RBAC
	}
	svc, err := autorun.New(autorun.Config{
		Runs:       autorun.NewRepository(s.services.Store.DB(), s.services.Store.Driver()),
		Sessions:   s.services.SessionModel,
		Components: s.services.Store.Components,
		Zones:      zones,
		Budget:     budget,
		Loop:       h.buildLoop,
	})
	if err != nil {
		slog.Error("auto run: not installed", "error", err)
		return nil
	}
	h.service = svc
	s.autorun = h
	return svc
}

// buildLoop is the autorun.LoopFactory: the run's agent loop, assembled like
// buildTaskRun's but for a pass with no request behind it. ctx already
// carries svc:agent:core, the run's session id and task id (the service
// stamps them), and the observer is the service's coverage derivation.
func (h *autoRunHandler) buildLoop(ctx context.Context, run *autorun.Run, observer agentloop.RunObserver) (*autorun.Loop, error) {
	if h.server.services.LLM == nil {
		return nil, fmt.Errorf("LLM not available")
	}
	// A run reads and queries. Read-only is a property of the identity
	// (svc:agent:core holds no write grant and the accessor denies Mutate for
	// it like for anyone), not of this policy; the policy still disables the
	// T2/T3 tool classes so a run's tool surface is the observe surface, the
	// same way a /tasks caller may ask for safety_tier "observe".
	policy := safety.DefaultPolicy()
	policy.Record = safety.RecordPolicy{}
	policy.Act = safety.ActPolicy{}

	registry := tools.NewCoreRegistry(h.inproc, policy, h.server.services.WebSearch)
	// The resolved scope is enforced on the executor: a tool call naming a
	// component outside it is refused as a scope violation. This is how a run
	// never widens its own aim — the set is fixed on the record at start and
	// the executor holds that same set.
	execOpts := []tools.ExecutorOption{
		tools.WithPolicy(policy),
		tools.WithWriteFloor(h.server.services.WriteFloor),
		tools.WithAllowedComponents(run.ResolvedComponents),
		tools.WithScopeZoneNames("the run's resolved scope"),
	}
	executor := tools.NewExecutor(registry, h.server.services.Metrics, execOpts...)
	// The same shared §C captain gate the task loop and the Core Agent carry,
	// so an incident regime holds here too. SessionModel is non-nil by
	// InstallAutoRun's precondition.
	var loopExec agentloop.BatchExecutor = captaingate.New(executor, h.server.services.SessionModel,
		h.server.services.Audit, captaingate.WithFloor(h.server.services.WriteFloor))

	// STABLE: the task system prompt plus the standing pass instructions, both
	// compile-time constants. VOLATILE: posture, the run's scope, the graph
	// summary — per run, per process. No query-derived skills: a run has no
	// query.
	systemPrompt := llm.StaticSystem(prompts.TaskSystem).Append(prompts.AutoRunSystem, true)
	systemPrompt = systemPrompt.Append(prompts.PostureSection(h.server.services.WriteFloor.Reason()), false)
	systemPrompt = systemPrompt.Append(prompts.AutoRunScopeSection(run.ResolvedComponents), false)
	if h.accessor.GraphAvailable() {
		// The principal here is the run's, read from the context the service
		// stamped — never a constant and never the activator.
		if summary, err := h.accessor.GraphSummary(ctx, rbac.PrincipalFromContext(ctx)); err == nil {
			systemPrompt = systemPrompt.Append(fmt.Sprintf(
				"Current infrastructure context:\nInfrastructure graph: %d nodes, %d edges. Node types: %v",
				summary.NodeCount, summary.EdgeCount, summary.NodesByType,
			), false)
		}
	}

	caps, _, _, _ := h.server.resolveActiveModel()
	agent := agentloop.NewAgent(
		h.server.services.LLM,
		loopExec,
		registry,
		systemPrompt,
		agentloop.WithObserver(observer),
		// The run's token ceiling is the loop's session ceiling: the loop's
		// own check stops the pass at the ceiling, and the service funds the
		// report from the reserve after it returns.
		agentloop.WithSessionLimits(runCeiling(run.TokenCeiling)),
		agentloop.WithAuditRepo(h.server.services.Audit),
		agentloop.WithMaxOutputTokens(caps.MaxOutputTokens),
		agentloop.WithToolErrorClassifier(classifyWriteFailure),
	)
	agent.SetMaxIterations(autorun.PassIterationCap)

	metrics := observability.EnsureMetrics(h.server.services.Metrics)
	session := agentloop.NewSession(metrics)
	fraction := agentloop.DefaultContextBudgetFraction
	if h.server.services.ContextBudgetProvider != nil {
		fraction = h.server.services.ContextBudgetProvider.BudgetFraction()
	}
	overhead := agentloop.EstimateOverheadTokens(systemPrompt.String(), registry.ToDefinitions())
	budget := agentloop.ComputeInputTokenBudget(caps.ContextWindowTokens, caps.MaxOutputTokens, overhead, fraction)
	if budget < 1 {
		budget = 1
	}
	session.TokenBudget = budget
	session.MaxMessages = agentloop.DefaultMaxMessages

	return &autorun.Loop{Agent: agent, Session: session, Close: session.Close}, nil
}

// runCeiling is the per-run agentloop.SessionLimits: the run's token ceiling,
// fixed for the run. The storage-backed provider the task loop reads is a
// deployment-wide runaway backstop; a run's ceiling is its own budget.
type runCeiling int

func (c runCeiling) SessionTokenCeiling() int { return int(c) }

// --- HTTP ---

func (s *Server) registerAutoRunRoutes(mux *http.ServeMux, prefix string) {
	h := s.autorun
	if h == nil {
		return
	}
	mux.HandleFunc(fmt.Sprintf("POST %s/auto/runs", prefix), h.handleTrigger)
	mux.HandleFunc(fmt.Sprintf("GET %s/auto/runs", prefix), h.handleListRuns)
	mux.HandleFunc(fmt.Sprintf("GET %s/auto/runs/{id}", prefix), h.handleGetRun)
	mux.HandleFunc(fmt.Sprintf("POST %s/auto/schedules", prefix), h.handleInstallSchedule)
	mux.HandleFunc(fmt.Sprintf("GET %s/auto/schedules", prefix), h.handleListSchedules)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/auto/schedules/{id}", prefix), h.handleRemoveSchedule)
}

type autoRunRequest struct {
	Scope autorun.Selection `json:"scope"`
}

type autoScheduleRequest struct {
	Scope           autorun.Selection `json:"scope"`
	IntervalMinutes int               `json:"interval_minutes"`
}

// handleTrigger is the operator trigger: POST /auto/runs starts one run over
// the scope, budgeted from the deployment defaults, as svc:agent:core, with
// the caller recorded as activator. Admin-gated: a run spends the deployment's
// LLM budget on the operator's say-so, which is an operator act in the same
// sense installing a schedule is. A live run on the scope is a 409 carrying
// the refusal, so it is visible to the activator and never queued.
func (h *autoRunHandler) handleTrigger(w http.ResponseWriter, r *http.Request) {
	principal, gated := h.server.requireAdmin(w, r)
	if gated {
		return
	}
	var req autoRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	run, err := h.service.Trigger(r.Context(), string(principal), req.Scope)
	if err != nil {
		h.writeTriggerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (h *autoRunHandler) writeTriggerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, autorun.ErrRunLive):
		writeError(w, http.StatusConflict, errorCodeConflict, "a run on this scope is live; the trigger is refused, not queued")
	case errors.Is(err, autorun.ErrEmptyScope), errors.Is(err, autorun.ErrUnknownComponent), errors.Is(err, autorun.ErrZonesUnavailable):
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, err.Error())
	case errors.Is(err, autorun.ErrNotStarted):
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "auto run service not started")
	default:
		writeInternalError(w, err, "auto run trigger")
	}
}

func (h *autoRunHandler) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.service.ListRuns(r.Context(), 50)
	if err != nil {
		writeInternalError(w, err, "auto run list")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "count": len(runs)})
}

func (h *autoRunHandler) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.service.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, autorun.ErrNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "run not found")
			return
		}
		writeInternalError(w, err, "auto run get")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// handleInstallSchedule installs a standing trigger over a scope. Admin-gated
// for the same reason the trigger is; the schedule records who installed it.
func (h *autoRunHandler) handleInstallSchedule(w http.ResponseWriter, r *http.Request) {
	principal, gated := h.server.requireAdmin(w, r)
	if gated {
		return
	}
	var req autoScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	if req.IntervalMinutes < 1 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "interval_minutes must be >= 1")
		return
	}
	sch, err := h.service.InstallSchedule(r.Context(), string(principal), req.Scope,
		time.Duration(req.IntervalMinutes)*time.Minute)
	if err != nil {
		h.writeTriggerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sch)
}

func (h *autoRunHandler) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.service.ListSchedules(r.Context())
	if err != nil {
		writeInternalError(w, err, "auto run schedules list")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": schedules, "count": len(schedules)})
}

func (h *autoRunHandler) handleRemoveSchedule(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if err := h.service.RemoveSchedule(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, autorun.ErrNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "schedule not found")
			return
		}
		writeInternalError(w, err, "auto run schedule remove")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
