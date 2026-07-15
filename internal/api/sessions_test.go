package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
)

// newSessionModelServer spins up an httptest.Server with the session-model,
// findings, warnings, and regime routes registered against an in-memory
// SQLite database. Returns the URL and the repositories so tests can
// observe state directly.
//
// Note (B005): the legacy team-global /api/v1/agent-sessions CRUD namespace was
// removed; tests seed sessions directly via the returned repository. The
// per-user /api/v1/sessions surface is covered in webui_test.go.
func newSessionModelServer(t *testing.T) (*httptest.Server, sessionmodel.Repository, findings.Repository, warnings.Repository) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sessionRepo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	findingsRepo := findings.NewRepository(s.DB(), store.DriverSQLite)
	warningsRepo := warnings.NewRepository(s.DB(), store.DriverSQLite)

	svc := &core.Services{
		Store:        s,
		SessionModel: sessionRepo,
		Findings:     findingsRepo,
		Warnings:     warningsRepo,
	}
	srv := api.New(svc, api.TestingPolicyEngine(svc))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	// Wire identity middleware so the author/creator principal is context-derived
	// (§12.1): handlers read the principal from context, never the body. A request
	// with no X-Test-Principal resolves to rbac.Unknown.
	handler := rbac.IdentityMiddleware(testPrincipalProvider{})(mux)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, sessionRepo, findingsRepo, warningsRepo
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	return postJSONAs(t, url, "", body)
}

// postJSONAs POSTs body as the given principal (via X-Test-Principal). An empty
// principal sends no header, so the server resolves rbac.Unknown.
func postJSONAs(t *testing.T, url, principal string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if principal != "" {
		req.Header.Set("X-Test-Principal", principal)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// TestFindingsAPI_PostAndList exercises the re-homed findings sub-resource
// (POST/GET /api/v1/sessions/{id}/findings, B005 §12.8) and proves the author
// principal is CONTEXT-DERIVED, not body-supplied — the spoof-closed
// accountability fix mirroring the B002 creator fix.
func TestFindingsAPI_PostAndList(t *testing.T) {
	ts, repo, findingsRepo, _ := newSessionModelServer(t)

	// Seed a source (J) and a target (I) session directly — the team-global
	// create route was removed in B005; findings only need their target to exist.
	for _, id := range []string{"sess-j", "sess-i"} {
		if _, err := repo.CreateSession(t.Context(), sessionmodel.AgentSession{
			ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "alice",
		}); err != nil {
			t.Fatalf("seed session %s: %v", id, err)
		}
	}

	// POST a finding into I as bob, with a SPOOFED author_principal in the body.
	// The stored author must be the context principal (bob), not the body value.
	post := postJSONAs(t, ts.URL+"/api/v1/sessions/sess-i/findings", "bob", map[string]any{
		"source_session_id": "sess-j",
		"author_principal":  "attacker", // spoof attempt — must be ignored
		"body":              "checked logs — culprit is X",
	})
	if post.StatusCode != http.StatusCreated {
		t.Fatalf("post finding status = %d, want 201", post.StatusCode)
	}
	post.Body.Close()

	// The stored finding is attributed to bob (the caller), not "attacker".
	items, err := findingsRepo.ListFindingsForTarget(t.Context(), "sess-i")
	if err != nil {
		t.Fatalf("ListFindingsForTarget: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("findings for I = %d, want 1", len(items))
	}
	if items[0].AuthorPrincipal != "bob" {
		t.Errorf("author_principal = %q, want %q (body spoof must be ignored — context-derived)",
			items[0].AuthorPrincipal, "bob")
	}

	// Unauthenticated POST → 401: the author cannot be derived, so the write is
	// refused (no spoofable fallback).
	un := postJSON(t, ts.URL+"/api/v1/sessions/sess-i/findings", map[string]any{
		"source_session_id": "sess-j", "body": "no principal",
	})
	if un.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated post status = %d, want 401", un.StatusCode)
	}
	un.Body.Close()

	// Missing source_session_id → 400 (author_principal is no longer a required
	// body field — it is context-derived).
	bad := postJSONAs(t, ts.URL+"/api/v1/sessions/sess-i/findings", "bob", map[string]any{
		"body": "no source",
	})
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("missing source_session_id status = %d, want 400", bad.StatusCode)
	}
	bad.Body.Close()

	// List findings for I (team-wide read) → 1.
	list, _ := http.Get(ts.URL + "/api/v1/sessions/sess-i/findings")
	var body struct {
		Findings []findings.Finding `json:"findings"`
		Count    int                `json:"count"`
	}
	json.NewDecoder(list.Body).Decode(&body)
	list.Body.Close()
	if body.Count != 1 {
		t.Errorf("findings count for I = %d, want 1", body.Count)
	}

	// List findings for J → 0.
	listJ, _ := http.Get(ts.URL + "/api/v1/sessions/sess-j/findings")
	var bodyJ struct {
		Count int `json:"count"`
	}
	json.NewDecoder(listJ.Body).Decode(&bodyJ)
	listJ.Body.Close()
	if bodyJ.Count != 0 {
		t.Errorf("findings count for J = %d, want 0", bodyJ.Count)
	}
}

func TestWarningsAPI_ListOnly(t *testing.T) {
	ts, _, _, warningsRepo := newSessionModelServer(t)

	// Raise a warning internally (no HTTP path — humans don't raise).
	if _, err := warningsRepo.RaiseWarning(t.Context(), warnings.Warning{
		ID:              "w1",
		SignalReference: "alert://x",
		Body:            "noisy alert",
	}); err != nil {
		t.Fatalf("RaiseWarning: %v", err)
	}

	resp, _ := http.Get(ts.URL + "/api/v1/warnings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /warnings status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Warnings []warnings.Warning `json:"warnings"`
		Count    int                `json:"count"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body.Count != 1 {
		t.Errorf("warnings count = %d, want 1", body.Count)
	}

	// POST /warnings does NOT exist — humans cannot raise.
	post := postJSON(t, ts.URL+"/api/v1/warnings", map[string]any{
		"signal_reference": "x", "body": "y",
	})
	if post.StatusCode != http.StatusMethodNotAllowed && post.StatusCode != http.StatusNotFound {
		t.Errorf("POST /warnings status = %d, want 404 or 405 (no human raise path)", post.StatusCode)
	}
	post.Body.Close()
}

func TestRegimeAPI_Read(t *testing.T) {
	ts, _, _, _ := newSessionModelServer(t)

	resp, _ := http.Get(ts.URL + "/api/v1/regime")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /regime status = %d, want 200", resp.StatusCode)
	}
	var reg sessionmodel.Regime
	json.NewDecoder(resp.Body).Decode(&reg)
	resp.Body.Close()
	if reg.Mode != sessionmodel.RegimeModeNormal {
		t.Errorf("seeded regime mode = %q, want normal", reg.Mode)
	}
}
