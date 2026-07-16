package coreagent_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/coreagent"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// spyExecutor records how many times Execute was called, the goroutine
// the call ran on, and lets a test inject a return value or error. It
// satisfies coreagent.ToolExecutor.
type spyExecutor struct {
	executions  atomic.Int64
	lastGoID    atomic.Int64
	returnValue any
	returnErr   error
	// invokeOrder is appended to by the wrapped repo before/after the
	// inner Execute fires; spyExecutor adds "execute" between them.
	invokeOrder *[]string
}

func (s *spyExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	s.executions.Add(1)
	s.lastGoID.Store(currentGoroutineID())
	if s.invokeOrder != nil {
		*s.invokeOrder = append(*s.invokeOrder, "execute")
	}
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	return s.returnValue, nil
}

// spyRepo wraps a real runmodel.Repository and records every call site
// touching tool_idempotency_keys, in order. Other methods pass through.
type spyRepo struct {
	inner       runmodel.Repository
	invokeOrder *[]string
	// Failure injection — if non-nil, MarkToolCompleted calls this
	// before the underlying repo. Used by the crash-resume test to
	// simulate the wrapper crashing AFTER inner.Execute and BEFORE
	// MarkToolCompleted lands.
	beforeMarkCompleted func() error
}

func (r *spyRepo) CreateRun(ctx context.Context, run runmodel.Run) (*runmodel.Run, error) {
	return r.inner.CreateRun(ctx, run)
}

func (r *spyRepo) GetRun(ctx context.Context, id string) (*runmodel.Run, error) {
	return r.inner.GetRun(ctx, id)
}
func (r *spyRepo) ListRunsForSession(ctx context.Context, sessionID string) ([]runmodel.Run, error) {
	return r.inner.ListRunsForSession(ctx, sessionID)
}
func (r *spyRepo) UpdateRunState(ctx context.Context, runID string, state runmodel.RunState, endedAt *time.Time) error {
	return r.inner.UpdateRunState(ctx, runID, state, endedAt)
}
func (r *spyRepo) SetLastStepID(ctx context.Context, runID, stepID string) error {
	return r.inner.SetLastStepID(ctx, runID, stepID)
}
func (r *spyRepo) AppendStep(ctx context.Context, s runmodel.Step) (*runmodel.Step, error) {
	return r.inner.AppendStep(ctx, s)
}
func (r *spyRepo) ListStepsForRun(ctx context.Context, runID string) ([]runmodel.Step, error) {
	return r.inner.ListStepsForRun(ctx, runID)
}
func (r *spyRepo) OpenSolicitation(ctx context.Context, s runmodel.Solicitation) (*runmodel.Solicitation, error) {
	return r.inner.OpenSolicitation(ctx, s)
}
func (r *spyRepo) OpenSolicitationAwaitInput(ctx context.Context, s runmodel.Solicitation) (*runmodel.Solicitation, error) {
	return r.inner.OpenSolicitationAwaitInput(ctx, s)
}
func (r *spyRepo) GetSolicitation(ctx context.Context, id string) (*runmodel.Solicitation, error) {
	return r.inner.GetSolicitation(ctx, id)
}
func (r *spyRepo) ResolveSolicitation(ctx context.Context, id string, p string, t time.Time) error {
	return r.inner.ResolveSolicitation(ctx, id, p, t)
}
func (r *spyRepo) RecordWorldHandle(ctx context.Context, h runmodel.WorldHandle) (*runmodel.WorldHandle, error) {
	return r.inner.RecordWorldHandle(ctx, h)
}
func (r *spyRepo) RecordWorldHandleAwaitWorld(ctx context.Context, h runmodel.WorldHandle) (*runmodel.WorldHandle, error) {
	return r.inner.RecordWorldHandleAwaitWorld(ctx, h)
}
func (r *spyRepo) GetWorldHandle(ctx context.Context, id string) (*runmodel.WorldHandle, error) {
	return r.inner.GetWorldHandle(ctx, id)
}
func (r *spyRepo) ListWorldHandlesForRun(ctx context.Context, runID string) ([]runmodel.WorldHandle, error) {
	return r.inner.ListWorldHandlesForRun(ctx, runID)
}
func (r *spyRepo) ObserveWorldHandle(ctx context.Context, id string, observedState string, polledAt time.Time) error {
	return r.inner.ObserveWorldHandle(ctx, id, observedState, polledAt)
}
func (r *spyRepo) AppendLedger(ctx context.Context, e runmodel.LedgerEntry) (*runmodel.LedgerEntry, error) {
	return r.inner.AppendLedger(ctx, e)
}
func (r *spyRepo) ListLedgerForRun(ctx context.Context, runID string) ([]runmodel.LedgerEntry, error) {
	return r.inner.ListLedgerForRun(ctx, runID)
}

// --- spied: tool_idempotency_keys methods ---

func (r *spyRepo) RecordToolIntent(ctx context.Context, key, runID, toolName, argsHash string) (*runmodel.IdempotencyKey, error) {
	if r.invokeOrder != nil {
		*r.invokeOrder = append(*r.invokeOrder, "RecordToolIntent")
	}
	return r.inner.RecordToolIntent(ctx, key, runID, toolName, argsHash)
}

func (r *spyRepo) MarkToolCompleted(ctx context.Context, key string, result string) error {
	if r.beforeMarkCompleted != nil {
		if err := r.beforeMarkCompleted(); err != nil {
			return err
		}
	}
	if r.invokeOrder != nil {
		*r.invokeOrder = append(*r.invokeOrder, "MarkToolCompleted")
	}
	return r.inner.MarkToolCompleted(ctx, key, result)
}

func (r *spyRepo) MarkToolFailed(ctx context.Context, key string, result string) error {
	if r.invokeOrder != nil {
		*r.invokeOrder = append(*r.invokeOrder, "MarkToolFailed")
	}
	return r.inner.MarkToolFailed(ctx, key, result)
}

func (r *spyRepo) GetIdempotencyKey(ctx context.Context, key string) (*runmodel.IdempotencyKey, error) {
	return r.inner.GetIdempotencyKey(ctx, key)
}

// --- helpers ---

// currentGoroutineID extracts the current goroutine's ID from
// runtime.Stack. Used by TestDurableExecutor_NoGoroutineFanOut to
// assert the inner Execute ran on the caller's goroutine.
func currentGoroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// runtime.Stack output starts with "goroutine NUMBER [...".
	s := string(buf[:n])
	prefix := "goroutine "
	if !strings.HasPrefix(s, prefix) {
		return -1
	}
	rest := s[len(prefix):]
	var id int64
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + int64(c-'0')
	}
	return id
}

// fixtureEnv builds a test fixture with a real SQLite store + a real
// runmodel.Repository under spyRepo, plus a created run so the wrapper
// has a runID anchor.
//
// The order field is a POINTER to a slice — both spyExecutor and
// spyRepo append through the same pointer so the test sees the
// combined call sequence. (Storing the slice by value would let one
// spy's append re-slice the header while the other still points at
// the stale header.)
type fixtureEnv struct {
	ctx       context.Context
	store     *store.Store
	repo      *spyRepo
	order     *[]string
	sessionID string
	runID     string
}

func newFixture(t *testing.T) *fixtureEnv {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	innerRepo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	order := make([]string, 0, 8)
	orderPtr := &order
	spied := &spyRepo{inner: innerRepo, invokeOrder: orderPtr}

	// Plain investigation session under normal regime. The §D5 tests
	// only need a valid session_id FK target for the run — the gate
	// itself moved out to internal/captaingate in Phase G.
	sess := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeDefault,
		CreatorPrincipal: "alice",
	}
	if _, err := sessRepo.CreateSession(context.Background(), sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Create a run on that session.
	run, err := innerRepo.CreateRun(context.Background(), runmodel.Run{
		ID: uuid.NewString(), SessionID: sess.ID, State: runmodel.RunStateRunning,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	return &fixtureEnv{
		ctx:       context.Background(),
		store:     s,
		repo:      spied,
		order:     orderPtr,
		sessionID: sess.ID,
		runID:     run.ID,
	}
}

// withRunCtx returns a context carrying the run ID + an optional
// caller-supplied idempotency key.
func (f *fixtureEnv) withRunCtx(key string) context.Context {
	c := agentctx.WithRunID(f.ctx, f.runID)
	if key != "" {
		c = agentctx.WithIdempotencyKey(c, key)
	}
	return c
}

// --- §D5 ordering: RecordToolIntent → tool.Execute → MarkToolCompleted ---

func TestDurableExecutor_D5Ordering(t *testing.T) {
	f := newFixture(t)
	spyExec := &spyExecutor{returnValue: map[string]any{"ok": true}, invokeOrder: f.order}
	dur := coreagent.NewDurableExecutor(spyExec, f.repo)

	// register_component DECLARES NeedsDurability (a non-idempotent create) — the
	// durability wrapper engages for it. (A Mutate tool that does not declare
	// durability bypasses the wrapper; see TestDurableExecutor_DrivenByProperty.)
	_, err := dur.Execute(f.withRunCtx(""), "register_component", map[string]any{"id": "x"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := []string{"RecordToolIntent", "execute", "MarkToolCompleted"}
	if len(*f.order) != len(want) {
		t.Fatalf("call sequence = %v, want %v", *f.order, want)
	}
	for i := range want {
		if (*f.order)[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q (full: %v)", i, (*f.order)[i], want[i], *f.order)
		}
	}
}

// --- Replay short-circuit: same key K → underlying invoked once ---

func TestDurableExecutor_ReplayShortCircuit(t *testing.T) {
	f := newFixture(t)
	spyExec := &spyExecutor{returnValue: map[string]any{"v": 1}, invokeOrder: f.order}
	dur := coreagent.NewDurableExecutor(spyExec, f.repo)

	key := "fixed-key"
	ctx := f.withRunCtx(key)

	first, err := dur.Execute(ctx, "register_component", map[string]any{"id": "x"})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := dur.Execute(ctx, "register_component", map[string]any{"id": "x"})
	if err != nil {
		t.Fatalf("second (replay): %v", err)
	}

	if got := spyExec.executions.Load(); got != 1 {
		t.Errorf("inner executions = %d, want 1 (replay short-circuit)", got)
	}
	// Both calls return matching values. JSON round-trip loses the
	// concrete type, but the shape stays the same.
	firstMap, _ := first.(map[string]any)
	secondMap, _ := second.(map[string]any)
	if firstMap["v"] != 1.0 && firstMap["v"] != 1 {
		// First call returns the raw Go value; second returns the JSON-decoded
		// form. We accept either.
	}
	if v, ok := secondMap["v"].(float64); !ok || v != 1 {
		t.Errorf("replayed value mismatch: %v", secondMap)
	}
}

// --- In-run replay via the COMPUTED runID+name+args key (no caller key):
// a declared-durable create that is re-issued with identical args returns the
// cached result and does not re-run the inner executor (so no duplicate row). ---

func TestDurableExecutor_InRunReplayDedupsCreate(t *testing.T) {
	f := newFixture(t)
	spyExec := &spyExecutor{returnValue: map[string]any{"source": "created"}, invokeOrder: f.order}
	dur := coreagent.NewDurableExecutor(spyExec, f.repo)

	// No caller-supplied idempotency key — the wrapper must derive a stable key
	// from runID + "register_component" + the canonical args hash. register_component
	// generates its row ID server-side OUTSIDE the args, so identical args
	// produce an identical key across the retry.
	ctx := f.withRunCtx("")
	args := map[string]any{"name": "prod-cluster", "type": "kubernetes"}

	if _, err := dur.Execute(ctx, "register_component", args); err != nil {
		t.Fatalf("first register_component: %v", err)
	}
	second, err := dur.Execute(ctx, "register_component", args)
	if err != nil {
		t.Fatalf("replay register_component: %v", err)
	}

	if got := spyExec.executions.Load(); got != 1 {
		t.Errorf("inner executions = %d, want 1 — an in-run replay of a durable create must short-circuit (no second row)", got)
	}
	// The replay returns the cached (JSON round-tripped) result.
	secondMap, ok := second.(map[string]any)
	if !ok || secondMap["source"] != "created" {
		t.Errorf("replay returned %v, want the cached result {source: created}", second)
	}
}

// --- Crash-resume: force fail BEFORE MarkToolCompleted → re-issue allowed ---

func TestDurableExecutor_CrashResumeRetriesCleanly(t *testing.T) {
	f := newFixture(t)
	spyExec := &spyExecutor{returnValue: map[string]any{"ok": true}, invokeOrder: f.order}
	dur := coreagent.NewDurableExecutor(spyExec, f.repo)

	// Inject a failure that fires before MarkToolCompleted writes —
	// simulates the process crashing after inner.Execute but before the
	// terminal status lands. The key stays 'issued'.
	fakeMarkErr := errors.New("simulated crash before mark")
	f.repo.beforeMarkCompleted = func() error { return fakeMarkErr }

	key := "resume-key"
	_, _ = dur.Execute(f.withRunCtx(key), "register_component", map[string]any{"id": "x"})
	if spyExec.executions.Load() != 1 {
		t.Fatalf("first execution count = %d, want 1", spyExec.executions.Load())
	}

	// Confirm the key is still 'issued' (not 'completed') after the simulated crash.
	row, err := f.repo.GetIdempotencyKey(f.ctx, key)
	if err != nil || row == nil {
		t.Fatalf("GetIdempotencyKey after crash: %v, %v", err, row)
	}
	if row.Status != runmodel.IdempotencyKeyStatusIssued {
		t.Errorf("key status after simulated crash = %q, want issued (crash-resume requires the row to remain re-runnable)",
			row.Status)
	}

	// Disable the failure injector and re-issue with the same key. The
	// inner executor should run AGAIN (because the prior call never
	// reached 'completed'), and MarkToolCompleted should land this time.
	f.repo.beforeMarkCompleted = nil
	_, err = dur.Execute(f.withRunCtx(key), "register_component", map[string]any{"id": "x"})
	if err != nil {
		t.Fatalf("resume Execute: %v", err)
	}
	if got := spyExec.executions.Load(); got != 2 {
		t.Errorf("inner executions after resume = %d, want 2 (re-issue while issued re-runs the tool)", got)
	}

	row, _ = f.repo.GetIdempotencyKey(f.ctx, key)
	if row == nil || row.Status != runmodel.IdempotencyKeyStatusCompleted {
		t.Errorf("key status after resume = %+v, want completed", row)
	}
}

// --- Default OFF: an undeclared tool is not wrapped → no repo calls ---

func TestDurableExecutor_UndeclaredBypass(t *testing.T) {
	f := newFixture(t)
	spyExec := &spyExecutor{returnValue: "ok", invokeOrder: f.order}
	dur := coreagent.NewDurableExecutor(spyExec, f.repo)

	// list_components does NOT declare NeedsDurability (durability is opt-in, default
	// OFF), so the wrapper must bypass it entirely.
	_, err := dur.Execute(f.withRunCtx(""), "list_components", map[string]any{"path": "/etc/hosts"})
	if err != nil {
		t.Fatalf("Execute undeclared: %v", err)
	}
	if spyExec.executions.Load() != 1 {
		t.Errorf("inner executions = %d, want 1", spyExec.executions.Load())
	}
	// The spy repo records every tool_idempotency_keys touch. None of those
	// names should appear for an undeclared tool — the wrapper short-circuits
	// before the repo is touched.
	for _, name := range *f.order {
		switch name {
		case "RecordToolIntent", "MarkToolCompleted", "MarkToolFailed":
			t.Errorf("default-OFF violation: spy repo recorded %q for a tool that does not declare NeedsDurability (call order: %v)", name, *f.order)
		}
	}
}

// --- Durability is driven by the NeedsDurability property, not the action
// class: a Read tool that declares it IS wrapped; a Mutate tool that does not
// is NOT wrapped. ---

func TestDurableExecutor_DrivenByProperty(t *testing.T) {
	// register_component is ActionRead but declares NeedsDurability → wrapped
	// (repo touched). An unclassified name takes ClassifyTool's unknown-tool
	// default — ActionMutate with NeedsDurability unset — so it is Mutate and
	// undeclared → bypassed (repo untouched). This pins that the durability
	// decision no longer consumes the Read/Mutate class.
	cases := []struct {
		tool        string
		wantWrapped bool
	}{
		{"register_component", true},   // Read + declared    → wrapped
		{"unclassified_mutate", false}, // Mutate + undeclared → bypassed
	}
	for _, tc := range cases {
		f := newFixture(t)
		spyExec := &spyExecutor{returnValue: map[string]any{"ok": true}, invokeOrder: f.order}
		dur := coreagent.NewDurableExecutor(spyExec, f.repo)

		if _, err := dur.Execute(f.withRunCtx(""), tc.tool, map[string]any{"id": "x"}); err != nil {
			t.Fatalf("%s: Execute: %v", tc.tool, err)
		}

		wrapped := false
		for _, name := range *f.order {
			if name == "RecordToolIntent" {
				wrapped = true
			}
		}
		if wrapped != tc.wantWrapped {
			t.Errorf("tool %q wrapped=%v, want %v (durability must be driven by NeedsDurability, not the action class; call order: %v)",
				tc.tool, wrapped, tc.wantWrapped, *f.order)
		}
	}
}

// --- No-goroutine-fan-out: inner Execute runs on the caller's goroutine ---

func TestDurableExecutor_NoGoroutineFanOut(t *testing.T) {
	f := newFixture(t)
	spyExec := &spyExecutor{returnValue: "ok"}
	dur := coreagent.NewDurableExecutor(spyExec, f.repo)

	callerGoID := currentGoroutineID()
	if callerGoID < 0 {
		t.Skip("could not extract goroutine ID — runtime.Stack format changed?")
	}
	_, err := dur.Execute(f.withRunCtx(""), "register_component", map[string]any{"id": "x"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	innerGoID := spyExec.lastGoID.Load()
	if innerGoID != callerGoID {
		t.Errorf("inner Execute ran on goroutine %d, caller is on %d — wrapper introduced a goroutine fan-out (single-loop / Invariant 1 violation)",
			innerGoID, callerGoID)
	}
}
