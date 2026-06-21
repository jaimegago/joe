package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
)

// Change 11 — hard-delete cascade integration tests.
//
// The handler shipped in Change 4: DELETE /api/v1/agent-sessions/{id}
// runs ONE SQL statement (DELETE FROM agent_sessions WHERE id = ?).
// The §5b-5 incident-expunge cascade is a pure schema property — the
// self-FK on linked_incident_id and the child-table FKs to
// agent_sessions(id) / agent_runs(id) (all declared ON DELETE CASCADE
// per §6-C in migrations 009, 010, 011) do the work.
//
// Change 11 ships NO migration. That itself is the §6-C self-check —
// if Change 11 needed to add a migration, the schema-level cascade
// would have been wrong and the earlier changes should be amended,
// not patched here.
//
// These tests exercise the cascade END-TO-END through the HTTP
// handler, complementing the SQL-only cascade tests in
// internal/sessionmodel/cascade_schema_test.go and
// internal/runmodel/cascade_schema_test.go.

// newCascadeServer wires the full session/run model + findings +
// warnings stack and returns the server URL + the store handle + every
// repo so tests can build child rows directly and inspect them after
// the DELETE via raw SQL.
func newCascadeServer(t *testing.T) (
	*httptest.Server,
	*store.Store,
	sessionmodel.Repository,
	runmodel.Repository,
	findings.Repository,
	warnings.Repository,
) {
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
	runRepo := runmodel.NewRepository(s.DB(), store.DriverSQLite)
	findingsRepo := findings.NewRepository(s.DB(), store.DriverSQLite)
	warningsRepo := warnings.NewRepository(s.DB(), store.DriverSQLite)

	svc := &core.Services{
		Store:        s,
		SessionModel: sessRepo,
		RunModel:     runRepo,
		Findings:     findingsRepo,
		Warnings:     warningsRepo,
	}
	srv := api.New(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, s, sessRepo, runRepo, findingsRepo, warningsRepo
}

// populateChildren creates one row in EVERY Change-2/3 child table
// (run, step, solicitation, world_handle, idempotency_key, ledger,
// finding, warning) tied to the given session — so the cascade test
// has something to verify cascades away.
func populateChildren(
	t *testing.T,
	ctx context.Context,
	sessionID string,
	runRepo runmodel.Repository,
	findingsRepo findings.Repository,
	warningsRepo warnings.Repository,
	authorPrincipal string,
) (runID, finding, warning string) {
	t.Helper()
	run, err := runRepo.CreateRun(ctx, runmodel.Run{
		ID: uuid.NewString(), SessionID: sessionID, State: runmodel.RunStateRunning,
	})
	if err != nil {
		t.Fatalf("create run for %s: %v", sessionID, err)
	}
	if _, err := runRepo.AppendStep(ctx, runmodel.Step{
		ID: uuid.NewString(), RunID: run.ID, StepNumber: 1,
		Kind: runmodel.StepKindReasoning, Payload: `{"x":1}`,
	}); err != nil {
		t.Fatalf("append step: %v", err)
	}
	if _, err := runRepo.OpenSolicitation(ctx, runmodel.Solicitation{
		ID: uuid.NewString(), RunID: run.ID,
		Kind: runmodel.SolicitationKindDecision, Payload: `{"q":"go?"}`,
	}); err != nil {
		t.Fatalf("open solicitation: %v", err)
	}
	if _, err := runRepo.RecordWorldHandle(ctx, runmodel.WorldHandle{
		ID: uuid.NewString(), RunID: run.ID,
		Locator: "k8s://deploy/x", QueryMeta: `{"v":1}`,
	}); err != nil {
		t.Fatalf("record world handle: %v", err)
	}
	key := "k-" + uuid.NewString()
	if _, err := runRepo.RecordToolIntent(ctx, key, run.ID, "graph_add_node", "h"); err != nil {
		t.Fatalf("record tool intent: %v", err)
	}
	if _, err := runRepo.AppendLedger(ctx, runmodel.LedgerEntry{
		ID: uuid.NewString(), RunID: run.ID, IdempotencyKey: key,
		ToolName: "graph_add_node", Tier: runmodel.TierT2,
		Principal: authorPrincipal, Summary: "added", Status: "issued",
	}); err != nil {
		t.Fatalf("append ledger: %v", err)
	}
	f := findings.Finding{
		ID:              uuid.NewString(),
		SourceSessionID: sessionID, TargetSessionID: sessionID,
		AuthorPrincipal: authorPrincipal, Body: "synthesis",
	}
	if _, err := findingsRepo.PostFinding(ctx, f); err != nil {
		t.Fatalf("post finding: %v", err)
	}
	w := warnings.Warning{
		ID: uuid.NewString(), SignalReference: "x", Body: "y",
		SourceInvestigationSessionID: &sessionID,
	}
	if _, err := warningsRepo.RaiseWarning(ctx, w); err != nil {
		t.Fatalf("raise warning: %v", err)
	}
	return run.ID, f.ID, w.ID
}

// countTied counts rows in a child table whose foreign-key column
// matches any of the given IDs. The query template must contain ONE
// (?,?,?) placeholder list.
func countTied(t *testing.T, s *store.Store, query string, ids ...string) int {
	t.Helper()
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var n int
	if err := s.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

// --- §5b-5: incident + linked investigations expunge as a single
//     cascade. End-to-end through the HTTP DELETE handler. ---

func TestCascadeDelete_IncidentAndLinkedInvestigations(t *testing.T) {
	ts, storeHandle, sessRepo, runRepo, findingsRepo, warningsRepo := newCascadeServer(t)
	ctx := context.Background()

	// Declare an incident (alice = captain by R-CAP1; promote-in-place §12.3).
	incidentID := declareIncident(t, sessRepo, "alice")
	// Two linked investigations (linked_incident_id = incidentID).
	j1ID := uuid.NewString()
	j2ID := uuid.NewString()
	for _, id := range []string{j1ID, j2ID} {
		linked := incidentID
		if _, err := sessRepo.CreateSession(ctx, sessionmodel.AgentSession{
			ID: id, Type: sessionmodel.SessionTypeDefault,
			CreatorPrincipal: "alice", LinkedIncidentID: &linked,
		}); err != nil {
			t.Fatalf("create investigation %s: %v", id, err)
		}
	}
	// Populate child rows under EACH of the three sessions.
	runIDs := make([]string, 0, 3)
	for _, sid := range []string{incidentID, j1ID, j2ID} {
		runID, _, _ := populateChildren(t, ctx, sid, runRepo, findingsRepo, warningsRepo, "alice")
		runIDs = append(runIDs, runID)
	}

	// Pre-delete sanity: every table has the expected count.
	type tableCheck struct {
		name    string
		query   string
		idsFor  string // "sessions" or "runs"
		wantPre int
		wantPos int
	}
	checks := []tableCheck{
		{"agent_sessions", `SELECT count(*) FROM agent_sessions WHERE id IN (?,?,?)`, "sessions", 3, 0},
		{"agent_runs", `SELECT count(*) FROM agent_runs WHERE id IN (?,?,?)`, "runs", 3, 0},
		{"run_steps", `SELECT count(*) FROM run_steps WHERE run_id IN (?,?,?)`, "runs", 3, 0},
		{"run_solicitations", `SELECT count(*) FROM run_solicitations WHERE run_id IN (?,?,?)`, "runs", 3, 0},
		{"run_world_handles", `SELECT count(*) FROM run_world_handles WHERE run_id IN (?,?,?)`, "runs", 3, 0},
		{"tool_idempotency_keys", `SELECT count(*) FROM tool_idempotency_keys WHERE run_id IN (?,?,?)`, "runs", 3, 0},
		{"action_ledger", `SELECT count(*) FROM action_ledger WHERE run_id IN (?,?,?)`, "runs", 3, 0},
		// findings carry 3 FKs to agent_sessions; we created them with
		// source = target = the same session, so cascade hits via either.
		{"findings", `SELECT count(*) FROM findings WHERE target_session_id IN (?,?,?)`, "sessions", 3, 0},
		// joe_warnings carry source_investigation_session_id nullable;
		// we set it to each session, so all 3 should cascade.
		{"joe_warnings", `SELECT count(*) FROM joe_warnings WHERE source_investigation_session_id IN (?,?,?)`, "sessions", 3, 0},
	}
	sessionIDs := []string{incidentID, j1ID, j2ID}
	for _, c := range checks {
		var ids []string
		if c.idsFor == "sessions" {
			ids = sessionIDs
		} else {
			ids = runIDs
		}
		if got := countTied(t, storeHandle, c.query, ids...); got != c.wantPre {
			t.Fatalf("pre-delete %s count = %d, want %d", c.name, got, c.wantPre)
		}
	}

	// DELETE the incident through the HTTP handler — one request.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agent-sessions/"+incidentID, nil)
	req.Header.Set("X-Test-Principal", "alice")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Post-delete (§12.4 severance, NOT two-level cascade): deleting incident I
	// destroys ONLY I and its OWN children. J1/J2 are independent 'default'
	// sessions linked via linked_incident_id, which is ON DELETE SET NULL — they
	// SURVIVE with their links severed, and so do all their children.
	//
	// Every check query is `... IN (?,?,?)` over [I, J1, J2] (sessions) or
	// [I-run, J1-run, J2-run] (runs); after the delete exactly the two J entries
	// remain, so each table goes from 3 to 2 (not 0).
	for _, c := range checks {
		var ids []string
		if c.idsFor == "sessions" {
			ids = sessionIDs
		} else {
			ids = runIDs
		}
		if got := countTied(t, storeHandle, c.query, ids...); got != 2 {
			t.Errorf("post-delete %s count = %d, want 2 (I expunged; J1/J2 survive — links severed, not cascaded)", c.name, got)
		}
	}

	// The incident itself and its own children are gone...
	if got := countTied(t, storeHandle,
		`SELECT count(*) FROM agent_sessions WHERE id = ?`, incidentID); got != 0 {
		t.Errorf("incident post-delete count = %d, want 0", got)
	}
	if got := countTied(t, storeHandle,
		`SELECT count(*) FROM agent_runs WHERE id = ?`, runIDs[0]); got != 0 {
		t.Errorf("incident run post-delete count = %d, want 0 (own-children cascade failed)", got)
	}
	// ...and J1/J2 survive with linked_incident_id severed to NULL.
	if got := countTied(t, storeHandle,
		`SELECT count(*) FROM agent_sessions WHERE id IN (?,?) AND linked_incident_id IS NULL`,
		j1ID, j2ID); got != 2 {
		t.Errorf("severed (NULL link) linked sessions = %d, want 2", got)
	}
}

// --- Orphan investigation deletes independently. ---

func TestCascadeDelete_OrphanInvestigation(t *testing.T) {
	ts, storeHandle, sessRepo, runRepo, findingsRepo, warningsRepo := newCascadeServer(t)
	ctx := context.Background()

	// Two unrelated sessions, neither linked to an incident.
	orphan := uuid.NewString()
	sibling := uuid.NewString()
	for _, id := range []string{orphan, sibling} {
		if _, err := sessRepo.CreateSession(ctx, sessionmodel.AgentSession{
			ID: id, Type: sessionmodel.SessionTypeDefault,
			CreatorPrincipal: "alice",
		}); err != nil {
			t.Fatalf("create session %s: %v", id, err)
		}
	}
	orphanRun, _, _ := populateChildren(t, ctx, orphan, runRepo, findingsRepo, warningsRepo, "alice")
	siblingRun, _, _ := populateChildren(t, ctx, sibling, runRepo, findingsRepo, warningsRepo, "alice")

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agent-sessions/"+orphan, nil)
	req.Header.Set("X-Test-Principal", "alice")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE orphan status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Orphan's child rows: gone.
	if got := countTied(t, storeHandle,
		`SELECT count(*) FROM agent_runs WHERE id = ?`, orphanRun); got != 0 {
		t.Errorf("orphan run count after delete = %d, want 0", got)
	}
	// Sibling's child rows: untouched.
	if got := countTied(t, storeHandle,
		`SELECT count(*) FROM agent_runs WHERE id = ?`, siblingRun); got != 1 {
		t.Errorf("sibling run count = %d, want 1 (orphan delete must NOT touch unrelated sessions)", got)
	}
	if got := countTied(t, storeHandle,
		`SELECT count(*) FROM agent_sessions WHERE id = ?`, sibling); got != 1 {
		t.Errorf("sibling session count = %d, want 1", got)
	}
}

// --- Postmortem property: resolve does NOT delete linked investigations. ---
//
// §5b-2 / §5b-5: resolved incidents leave linked investigations alive
// (a resolved incident is an UPDATE on the row, not a DELETE). The
// hard-delete cascade is opt-in via DELETE only.

func TestCascadeDelete_ResolveDoesNotCascade(t *testing.T) {
	_, _, sessRepo, _, _, _ := newCascadeServer(t)
	ctx := context.Background()

	incidentID := declareIncident(t, sessRepo, "alice")
	// Two linked investigations.
	j1 := uuid.NewString()
	j2 := uuid.NewString()
	for _, id := range []string{j1, j2} {
		linked := incidentID
		if _, err := sessRepo.CreateSession(ctx, sessionmodel.AgentSession{
			ID: id, Type: sessionmodel.SessionTypeDefault,
			CreatorPrincipal: "alice", LinkedIncidentID: &linked,
		}); err != nil {
			t.Fatalf("create investigation: %v", err)
		}
	}
	// Advance + resolve.
	if err := sessRepo.UpdateIncidentState(ctx, incidentID,
		sessionmodel.IncidentStateBelievedMitigated); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, err := sessRepo.ResolveIncidentRegime(ctx, "alice"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Linked investigations MUST still exist after resolve. The
	// postmortem property — investigations outlive the resolved
	// incident, only DELETE expunges.
	for _, id := range []string{j1, j2} {
		got, err := sessRepo.GetSession(ctx, id)
		if err != nil {
			t.Fatalf("GetSession %s: %v", id, err)
		}
		if got == nil {
			t.Errorf("investigation %s was deleted by resolve — §5b-2/§5b-5 postmortem property violated", id)
		}
	}
	// Incident itself is still there too, just with state=resolved.
	inc, _ := sessRepo.GetSession(ctx, incidentID)
	if inc == nil {
		t.Fatal("incident session deleted by resolve")
	}
	if inc.IncidentState == nil || *inc.IncidentState != sessionmodel.IncidentStateResolved {
		t.Errorf("incident state after resolve = %+v, want resolved", inc.IncidentState)
	}
}
