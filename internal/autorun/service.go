package autorun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/uid"
)

// PassIterationCap is the loop's step cap for a run. The two ceilings the
// design named — tokens and wall clock — are what bound a pass; the loop's
// iteration cap is a mechanism the loop has regardless, set here high enough
// that it is not a third ceiling in practice and recorded honestly as the stop
// reason if it ever fires.
const PassIterationCap = 1000

// DefaultSchedulerTick is how often the scheduler looks for due schedules.
const DefaultSchedulerTick = 30 * time.Second

// Loop is one constructed agent loop for one run: the agent (its tool
// registry, executor and system prompt already assembled for the run's scope)
// and the session it runs in. Close releases what the session holds.
type Loop struct {
	Agent   *agentloop.Agent
	Session *agentloop.Session
	Close   func()
}

// LoopFactory builds the loop for a run. The context it receives already
// carries the run's principal (svc:agent:core) and the run's session and task
// ids; the observer must be installed on the agent (agentloop.WithObserver),
// it is how coverage is derived; and the run's ResolvedComponents is the set
// the executor must enforce. The factory lives where the tool registry and the
// guarded accessor can be assembled (internal/api), not here: this package
// owns the run, not the plumbing a loop is built from.
type LoopFactory func(ctx context.Context, run *Run, observer agentloop.RunObserver) (*Loop, error)

// Config wires a Service.
type Config struct {
	Runs       Repository
	Sessions   sessionmodel.Repository
	Components store.ComponentRepository
	// Zones resolves zone selections; nil when RBAC is not configured, in
	// which case a selection naming zones is refused.
	Zones ZoneAssignments
	// Budget is the deployment default an operator trigger and a newly
	// installed schedule are budgeted from.
	Budget Budget
	Loop   LoopFactory
	// Findings is the report side's seam (FindingsPipeline). nil until wired.
	Findings FindingsPipeline
	Logger   *slog.Logger
	// SchedulerTick overrides DefaultSchedulerTick; zero keeps the default.
	SchedulerTick time.Duration
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// Service is the non-HTTP way to start an agent run. Trigger is the primitive
// an operator act reaches (through the HTTP handler in internal/api) and the
// scheduler reaches on its own; nothing else starts a run — cluster events are
// not a trigger, by design.
type Service struct {
	cfg       Config
	logger    *slog.Logger
	principal rbac.Principal

	mu      sync.Mutex
	baseCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	doneCh  chan struct{}
}

// New constructs a Service. It mints the run principal once, through the
// canonical constructor, so no literal is re-typed here.
func New(cfg Config) (*Service, error) {
	if cfg.Runs == nil || cfg.Sessions == nil || cfg.Components == nil || cfg.Loop == nil {
		return nil, fmt.Errorf("autorun: runs, sessions, components and loop factory are required")
	}
	if cfg.Budget.TokenCeiling <= 0 || cfg.Budget.WallClock <= 0 {
		return nil, fmt.Errorf("autorun: a positive token ceiling and wall clock are required")
	}
	principal, err := rbac.AgentCorePrincipal()
	if err != nil {
		return nil, err
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.SchedulerTick <= 0 {
		cfg.SchedulerTick = DefaultSchedulerTick
	}
	return &Service{
		cfg:       cfg,
		logger:    cfg.Logger.With("component", "autorun"),
		principal: principal,
	}, nil
}

// Principal is the identity every run executes as.
func (s *Service) Principal() rbac.Principal { return s.principal }

// Budget is the deployment default a trigger is budgeted from.
func (s *Service) Budget() Budget { return s.cfg.Budget }

// Start gives the service its base context (runs and the scheduler derive from
// it, so Stop ends them), closes every run left 'running' by a previous
// process, and starts the scheduler.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.baseCtx != nil {
		return nil
	}
	if n, err := s.cfg.Runs.FailInterrupted(ctx, s.cfg.Now()); err != nil {
		return fmt.Errorf("autorun: close interrupted runs: %w", err)
	} else if n > 0 {
		s.logger.Warn("closed runs left live by a previous process", "count", n)
	}
	s.baseCtx, s.cancel = context.WithCancel(ctx)
	s.doneCh = make(chan struct{})
	go s.scheduleLoop(s.baseCtx)
	s.logger.Info("auto run service started",
		"token_ceiling", s.cfg.Budget.TokenCeiling, "wall_clock", s.cfg.Budget.WallClock,
		"report_reserve_tokens", s.cfg.Budget.ReportReserveTokens, "scheduler_tick", s.cfg.SchedulerTick)
	return nil
}

// Stop cancels every live run and the scheduler and waits, bounded by ctx, for
// them to record their end. A run cancelled here is recorded as such.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.doneCh
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	finished := make(chan struct{})
	go func() {
		s.wg.Wait()
		<-done
		close(finished)
	}()
	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("autorun: timeout waiting for runs to stop")
	}
}

// Trigger is the operator primitive: start one run over sel as activator,
// budgeted from the deployment default. It resolves the scope, records the
// run (which is what refuses a second live run on the scope), creates the
// run's session as svc:agent:core, and starts the pass in the background. It
// returns the recorded run, or the refusal.
func (s *Service) Trigger(ctx context.Context, activator string, sel Selection) (*Run, error) {
	return s.TriggerWithBudget(ctx, activator, sel, s.cfg.Budget)
}

// TriggerWithBudget is Trigger with an explicit budget — a schedule's, copied
// from the defaults when it was installed.
func (s *Service) TriggerWithBudget(ctx context.Context, activator string, sel Selection, budget Budget) (*Run, error) {
	s.mu.Lock()
	base := s.baseCtx
	s.mu.Unlock()
	if base == nil {
		return nil, ErrNotStarted
	}
	if activator == "" {
		return nil, fmt.Errorf("autorun: activator required")
	}
	if budget.TokenCeiling <= 0 || budget.WallClock <= 0 {
		return nil, fmt.Errorf("autorun: a positive token ceiling and wall clock are required")
	}
	resolved, err := ResolveScope(ctx, s.cfg.Components, s.cfg.Zones, sel)
	if err != nil {
		return nil, err
	}
	now := s.cfg.Now()
	run := Run{
		ID:                  uid.New(),
		Activator:           activator,
		Principal:           string(s.principal),
		Selection:           sel.Normalize(),
		ScopeKey:            sel.Key(),
		ResolvedComponents:  resolved,
		TokenCeiling:        budget.TokenCeiling,
		WallClockSeconds:    int(budget.WallClock / time.Second),
		ReportReserveTokens: budget.ReportReserveTokens,
		State:               StateRunning,
		Covered:             []string{},
		NotCovered:          append([]string{}, resolved...),
		StartedAt:           now,
	}
	created, err := s.cfg.Runs.CreateRun(ctx, run)
	if err != nil {
		return nil, err
	}
	run = *created

	// The session is the run's output, created by svc:agent:core — the creator
	// is what tells a run's session from a person's. Team-public and durable
	// are properties every agent_sessions row has (team-wide read, migration
	// 025). The title is set at creation, not auto-titled from a first message
	// a run never sends.
	title := fmt.Sprintf("Auto Joe pass over %d component(s)", len(resolved))
	sessionID := uid.New()
	if _, err := s.cfg.Sessions.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               sessionID,
		Type:             sessionmodel.SessionTypeDefault,
		CreatorPrincipal: string(s.principal),
		Title:            &title,
		CreatedAt:        now,
	}); err != nil {
		s.finishFailed(ctx, &run, "create session: "+err.Error())
		return nil, fmt.Errorf("autorun: create session: %w", err)
	}
	run.SessionID = sessionID
	if err := s.cfg.Runs.SetRunSession(ctx, run.ID, run.SessionID); err != nil {
		s.finishFailed(ctx, &run, "link session: "+err.Error())
		return nil, fmt.Errorf("autorun: link session: %w", err)
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.execute(base, run, budget)
	}()
	s.logger.Info("auto run started", "run_id", run.ID, "activator", activator,
		"components", len(resolved), "session_id", run.SessionID)
	snapshot := run
	return &snapshot, nil
}

// execute is the pass: the loop under svc:agent:core, bounded by the two
// ceilings; then, when a ceiling stopped it, the report call funded from the
// reserve; then the record and the session's final turn.
func (s *Service) execute(base context.Context, run Run, budget Budget) {
	ctx := rbac.WithPrincipal(base, s.principal)
	ctx = agentctx.WithSessionID(ctx, run.SessionID)
	ctx = agentctx.WithTaskID(ctx, run.ID)

	cov := newCoverage(run.ResolvedComponents)
	loop, err := s.cfg.Loop(ctx, &run, cov)
	if err != nil {
		s.finishFailed(ctx, &run, "build loop: "+err.Error())
		return
	}
	if loop.Close != nil {
		defer loop.Close()
	}

	passCtx, cancelPass := context.WithTimeout(ctx, budget.WallClock)
	answer, runErr := loop.Agent.Run(passCtx, loop.Session, prompts.AutoRunPass)
	cancelPass()
	run.TokensUsed = loop.Session.TotalTokens

	var reportErr error
	switch {
	case runErr == nil:
		run.StopReason = loop.Session.StopReason()
	case errors.Is(runErr, agentloop.ErrSessionTokenCeiling):
		run.StopReason = agentloop.StopReasonTokenCeiling
		answer, reportErr = s.report(ctx, loop, budget)
	case passCtx.Err() == context.DeadlineExceeded:
		run.StopReason = agentloop.StopReasonWallClock
		answer, reportErr = s.report(ctx, loop, budget)
	case errors.Is(runErr, context.Canceled) || base.Err() != nil:
		run.StopReason = agentloop.StopReasonCancelled
		run.Error = "service stopped while the run was live"
	case errors.Is(runErr, agentloop.ErrMaxIterations):
		// The cap fired and the loop's own synthesis produced nothing; the
		// reserve funds one more attempt, the same call the ceilings get.
		run.StopReason = agentloop.StopReasonMaxIterations
		answer, reportErr = s.report(ctx, loop, budget)
	default:
		run.StopReason = StopReasonError
		run.Error = runErr.Error()
	}
	run.ReportTokens = loop.Session.TotalTokens - run.TokensUsed
	run.Covered, run.NotCovered = cov.Partition()
	ended := s.cfg.Now()
	run.EndedAt = &ended
	switch run.StopReason {
	case StopReasonError, agentloop.StopReasonCancelled:
		run.State = StateFailed
	default:
		run.State = StateCompleted
	}
	if reportErr != nil && run.Error == "" {
		run.Error = "report: " + reportErr.Error()
	}

	// The record first: delivery reads coverage and the stop reason from it,
	// and the session's turn is rendered from it. Written under a context that
	// survives the base context's cancellation — a run cancelled by Stop still
	// owes its record, and that is the moment the base context is gone.
	wctx := context.WithoutCancel(ctx)
	if err := s.cfg.Runs.FinishRun(wctx, run); err != nil {
		s.logger.Error("auto run: record write failed", "run_id", run.ID, "error", err)
	}

	findings := ""
	if s.cfg.Findings != nil && run.State == StateCompleted {
		section, err := s.cfg.Findings.Findings(wctx, &run, answer)
		if err != nil {
			s.logger.Error("auto run: findings pipeline failed", "run_id", run.ID, "error", err)
			findings = unverifiedSection(answer) + "\n\n(The findings pipeline failed: " + err.Error() + ".)"
		} else {
			findings = section
		}
	} else {
		findings = unverifiedSection(answer)
	}
	s.writeFinalTurn(wctx, &run, FinalTurn(findings, &run))
	s.logger.Info("auto run finished", "run_id", run.ID, "state", run.State, "stop_reason", run.StopReason,
		"covered", len(run.Covered), "not_covered", len(run.NotCovered),
		"tokens_used", run.TokensUsed, "report_tokens", run.ReportTokens)
}

// report is the reserve-funded report call: one tool-less synthesis under a
// FRESH deadline (the pass's context may already be past its wall clock) with
// its output bounded by the reserve. It is what makes a ceiling-stopped run
// report rather than discard.
func (s *Service) report(ctx context.Context, loop *Loop, budget Budget) (string, error) {
	reserve := budget.ReportReserve
	if reserve <= 0 {
		reserve = 2 * time.Minute
	}
	reportCtx, cancel := context.WithTimeout(ctx, reserve)
	defer cancel()
	return loop.Agent.SynthesizeReport(reportCtx, loop.Session, prompts.AutoRunBudgetSynthesis, budget.ReportReserveTokens)
}

func (s *Service) finishFailed(ctx context.Context, run *Run, reason string) {
	ended := s.cfg.Now()
	run.EndedAt = &ended
	run.State = StateFailed
	run.StopReason = StopReasonError
	run.Error = reason
	if err := s.cfg.Runs.FinishRun(context.WithoutCancel(ctx), *run); err != nil {
		s.logger.Error("auto run: record write failed", "run_id", run.ID, "error", err)
	}
	if run.SessionID != "" {
		s.writeFinalTurn(ctx, run, FinalTurn(unverifiedSection(""), run))
	}
	s.logger.Error("auto run failed", "run_id", run.ID, "error", reason)
}

// writeFinalTurn persists the run's one assistant message. A run has no human
// turn, so nothing is written in the user role: the opening instruction is
// joe's, not a person's, and a transcript that showed it as one would
// misreport who spoke.
func (s *Service) writeFinalTurn(ctx context.Context, run *Run, content string) {
	if run.SessionID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if _, err := s.cfg.Sessions.AddChatMessage(ctx, sessionmodel.ChatMessage{
		ID:         uid.New(),
		SessionID:  run.SessionID,
		Role:       "assistant",
		Content:    content,
		StopReason: run.StopReason,
		CreatedAt:  s.cfg.Now(),
	}); err != nil {
		s.logger.Error("auto run: final turn write failed", "run_id", run.ID, "session_id", run.SessionID, "error", err)
	}
}

// GetRun reads one run record.
func (s *Service) GetRun(ctx context.Context, id string) (*Run, error) {
	return s.cfg.Runs.GetRun(ctx, id)
}

// ListRuns reads the most recent run records.
func (s *Service) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	return s.cfg.Runs.ListRuns(ctx, limit)
}

// InstallSchedule installs a standing trigger over sel, firing every interval,
// budgeted from the deployment defaults as they stand now. The selection is
// resolved once here so an unresolvable schedule is refused at install rather
// than at every tick; each run resolves it again at its own start.
func (s *Service) InstallSchedule(ctx context.Context, installedBy string, sel Selection, interval time.Duration) (*Schedule, error) {
	if installedBy == "" {
		return nil, fmt.Errorf("autorun: installer required")
	}
	if interval < time.Minute {
		return nil, fmt.Errorf("autorun: schedule interval must be at least one minute")
	}
	if _, err := ResolveScope(ctx, s.cfg.Components, s.cfg.Zones, sel); err != nil {
		return nil, err
	}
	now := s.cfg.Now()
	return s.cfg.Runs.CreateSchedule(ctx, Schedule{
		ID:                  uid.New(),
		Selection:           sel.Normalize(),
		ScopeKey:            sel.Key(),
		IntervalSeconds:     int(interval / time.Second),
		TokenCeiling:        s.cfg.Budget.TokenCeiling,
		WallClockSeconds:    int(s.cfg.Budget.WallClock / time.Second),
		ReportReserveTokens: s.cfg.Budget.ReportReserveTokens,
		InstalledBy:         installedBy,
		InstalledAt:         now,
		NextRunAt:           now.Add(interval),
	})
}

// RemoveSchedule uninstalls a standing trigger. A run it already started is
// not affected.
func (s *Service) RemoveSchedule(ctx context.Context, id string) error {
	return s.cfg.Runs.DeleteSchedule(ctx, id)
}

// ListSchedules reads every installed schedule.
func (s *Service) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return s.cfg.Runs.ListSchedules(ctx)
}

func (s *Service) scheduleLoop(ctx context.Context) {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.cfg.SchedulerTick)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Tick(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// Tick fires every due schedule once. Each firing is a Trigger with the
// schedule as activator and the schedule's recorded budget; a refusal (a run
// on the scope still live) is recorded on the schedule as its last outcome,
// which is where a schedule, as activator, sees it. Exported so a test can
// drive the scheduler without waiting on the ticker.
func (s *Service) Tick(ctx context.Context) {
	now := s.cfg.Now()
	due, err := s.cfg.Runs.ListDueSchedules(ctx, now)
	if err != nil {
		s.logger.Error("auto run: list due schedules failed", "error", err)
		return
	}
	for _, sch := range due {
		activator := "schedule:" + sch.ID
		budget := sch.Budget()
		budget.ReportReserve = s.cfg.Budget.ReportReserve
		outcome := ""
		run, err := s.TriggerWithBudget(ctx, activator, sch.Selection, budget)
		switch {
		case err == nil:
			outcome = "started " + run.ID
		case errors.Is(err, ErrRunLive):
			outcome = "refused: a run on this scope was still live"
		default:
			outcome = "error: " + err.Error()
		}
		next := now.Add(time.Duration(sch.IntervalSeconds) * time.Second)
		if err := s.cfg.Runs.MarkScheduleFired(ctx, sch.ID, now, next, outcome); err != nil {
			s.logger.Error("auto run: mark schedule fired failed", "schedule_id", sch.ID, "error", err)
		}
		s.logger.Info("auto run schedule fired", "schedule_id", sch.ID, "outcome", outcome)
	}
}
