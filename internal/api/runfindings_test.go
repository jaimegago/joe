package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runfindings"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// runFindingsFixture is a migrated store, a run-findings repository, an RBAC
// repository with the admin gate enabled, and a mux with the routes registered.
type runFindingsFixture struct {
	t    *testing.T
	s    *store.Store
	repo *runfindings.SQLRepository
	rbac *rbac.SQLRepository
	mux  *http.ServeMux
}

func newRunFindingsFixture(t *testing.T, rbacEnabled bool) *runFindingsFixture {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	auditRepo := audit.NewRepository(s.DB(), s.Driver())
	rbacRepo := rbac.NewRepositoryWithAudit(s.DB(), s.Driver(), auditRepo)
	services := core.New(&config.Config{}, s, s.DB(), s.Driver(), nil, nil)
	services.RBAC = rbacRepo
	services.Audit = auditRepo
	services.RBACEnabled = rbacEnabled
	services.RunFindings = runfindings.NewRepository(s.DB(), s.Driver())
	srv := New(services, TestingPolicyEngine(services))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return &runFindingsFixture{t: t, s: s, repo: runfindings.NewRepository(s.DB(), s.Driver()), rbac: rbacRepo, mux: mux}
}

func (f *runFindingsFixture) do(path string, principal rbac.Principal) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if principal != "" {
		req = req.WithContext(rbac.WithPrincipal(req.Context(), principal))
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	return w
}

func (f *runFindingsFixture) session() string {
	f.t.Helper()
	id := uuid.NewString()
	repo := sessionmodel.NewRepository(f.s.DB(), f.s.Driver())
	if _, err := repo.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "svc:agent:core",
	}); err != nil {
		f.t.Fatalf("create session: %v", err)
	}
	return id
}

func (f *runFindingsFixture) put(runID, scope, session string, at time.Time, findings ...runfindings.Finding) {
	f.t.Helper()
	for i := range findings {
		findings[i].ID = uuid.NewString()
		findings[i].RunID, findings[i].ScopeKey, findings[i].SessionID = runID, scope, session
		findings[i].EvidenceSessionID = session
		findings[i].EstablishedAt, findings[i].CreatedAt = at, at
		if findings[i].Weight == 0 {
			findings[i].Weight = 1
		}
		if findings[i].Derivation.EvidenceLine == "" {
			findings[i].Derivation.EvidenceLine = "e"
		}
	}
	if err := f.repo.Put(context.Background(), runfindings.FindingSet{RunID: runID, ScopeKey: scope, SessionID: session, CreatedAt: at}, findings); err != nil {
		f.t.Fatalf("put %s: %v", runID, err)
	}
}

func decodeRunFindings(t *testing.T, w *httptest.ResponseRecorder) runFindingsResponse {
	t.Helper()
	var out runFindingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return out
}

func issue(key, band string, label runfindings.Label) runfindings.Finding {
	return runfindings.Finding{Key: key, Type: runfindings.TypeIssue, Reference: runfindings.ReferenceSelfReport,
		SubjectNodeID: "n-" + key, SubjectComponentID: "k8s", Claim: "claim " + key,
		Consequence: runfindings.ConsequenceDegraded, Band: runfindings.Band(band), Label: label}
}

func graphUpdate(key string, label runfindings.Label) runfindings.Finding {
	return runfindings.Finding{Key: key, Type: runfindings.TypeGraphUpdate, Reference: runfindings.ReferenceGraph,
		SubjectNodeID: "n-" + key, SubjectComponentID: "k8s", Claim: "admit " + key, Band: runfindings.BandP3, Label: label}
}

func TestRunFindings_ListReturnsLatestRunPerScopeInDeliveryOrder(t *testing.T) {
	f := newRunFindingsFixture(t, false)
	sess := f.session()
	t1 := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	f.put("run-old", "scope-a", sess, t1, issue("stale", "P0", runfindings.LabelNew))
	f.put("run-new", "scope-a", sess, t2,
		issue("low", "P2", runfindings.LabelStanding),
		issue("high", "P0", runfindings.LabelNew),
		graphUpdate("gu", runfindings.LabelNew),
	)

	w := f.do("/api/v1/run-findings", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	out := decodeRunFindings(t, w)
	if len(out.Runs) != 1 || out.Runs[0].RunID != "run-new" || out.Runs[0].FindingCount != 3 {
		t.Errorf("runs = %+v, want run-new with 3", out.Runs)
	}
	if out.Count != 3 || len(out.Findings) != 3 {
		t.Fatalf("count = %d, findings = %d", out.Count, len(out.Findings))
	}
	if out.Findings[0].Key != "high" || out.Findings[1].Key != "low" || out.Findings[2].Key != "gu" {
		t.Errorf("order = %s, %s, %s; want high, low, gu", out.Findings[0].Key, out.Findings[1].Key, out.Findings[2].Key)
	}
	for _, fi := range out.Findings {
		if fi.Key == "stale" {
			t.Error("the previous run's finding leaked into the latest view")
		}
	}

	// One run by id.
	w = f.do("/api/v1/run-findings?run_id=run-old", "user:alice")
	out = decodeRunFindings(t, w)
	if w.Code != http.StatusOK || out.Count != 1 || out.Findings[0].Key != "stale" {
		t.Errorf("run_id filter: %d %+v", w.Code, out)
	}
	if w := f.do("/api/v1/run-findings?run_id=nope", "user:alice"); w.Code != http.StatusNotFound {
		t.Errorf("unknown run_id: %d, want 404", w.Code)
	}
}

func TestRunFindings_CleanRunReadsAsClean(t *testing.T) {
	f := newRunFindingsFixture(t, false)
	sess := f.session()
	f.put("run-1", "scope-a", sess, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), issue("a", "P1", runfindings.LabelNew))
	f.put("run-2", "scope-a", sess, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))
	out := decodeRunFindings(t, f.do("/api/v1/run-findings", "user:alice"))
	if len(out.Runs) != 1 || out.Runs[0].RunID != "run-2" || out.Count != 0 {
		t.Errorf("clean run view = %+v", out)
	}
	// An install with no run yet is an empty list, not an error.
	g := newRunFindingsFixture(t, false)
	w := g.do("/api/v1/run-findings", "user:alice")
	out = decodeRunFindings(t, w)
	if w.Code != http.StatusOK || out.Count != 0 || out.Runs == nil || out.Findings == nil {
		t.Errorf("empty install = %d %s", w.Code, w.Body.String())
	}
}

func TestRunFindings_AdmissionsAreGraphUpdatesForAdmins(t *testing.T) {
	f := newRunFindingsFixture(t, true)
	sess := f.session()
	at := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	f.put("run-1", "scope-a", sess, at,
		issue("i", "P1", runfindings.LabelNew),
		graphUpdate("new", runfindings.LabelNew),
		graphUpdate("standing", runfindings.LabelStanding),
		graphUpdate("resolved", runfindings.LabelResolved),
		graphUpdate("unknown", runfindings.LabelUnknown),
	)

	if w := f.do("/api/v1/run-findings/admissions", "user:bob"); w.Code != http.StatusForbidden {
		t.Errorf("non-admin: %d, want 403", w.Code)
	}
	if err := f.rbac.AddAdmin(context.Background(), rbac.Admin{Principal: "user:alice", GrantedBy: "test", Reason: "test"}, "test"); err != nil {
		t.Fatal(err)
	}
	w := f.do("/api/v1/run-findings/admissions", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin: %d %s", w.Code, w.Body.String())
	}
	out := decodeRunFindings(t, w)
	keys := map[string]bool{}
	for _, fi := range out.Findings {
		keys[fi.Key] = true
		if fi.Type != runfindings.TypeGraphUpdate {
			t.Errorf("non graph update %s in admissions", fi.Key)
		}
	}
	if !keys["new"] || !keys["standing"] || keys["resolved"] || keys["unknown"] || keys["i"] || out.Count != 2 {
		t.Errorf("admissions = %v", keys)
	}

	// The ranked list is team-public: the same non-admin reads it.
	if w := f.do("/api/v1/run-findings", "user:bob"); w.Code != http.StatusOK {
		t.Errorf("non-admin list: %d, want 200", w.Code)
	}
}

// TestRunFindings_RoutesAbsentWhenUnwired: a harness with no repository
// registers no route, matching the rest of the store-backed surfaces.
func TestRunFindings_RoutesAbsentWhenUnwired(t *testing.T) {
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	if w := doRequest(mux, http.MethodGet, "/api/v1/run-findings", nil); w.Code != http.StatusNotFound {
		t.Errorf("unwired route: %d, want 404", w.Code)
	}
}
