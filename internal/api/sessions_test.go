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
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
)

// newSessionModelServer spins up an httptest.Server with the session-model,
// findings, warnings, and regime routes registered against an in-memory
// SQLite database. Returns the URL and the repositories so tests can
// observe state directly.
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
	srv := api.New(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, sessionRepo, findingsRepo, warningsRepo
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestSessionsAPI_CreatePerType(t *testing.T) {
	ts, _, _, _ := newSessionModelServer(t)

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{
			name: "investigation",
			body: map[string]any{"type": "investigation", "creator_principal": "alice"},
			want: http.StatusCreated,
		},
		{
			name: "incident with state",
			body: map[string]any{
				"type":              "incident",
				"incident_state":    "declared",
				"creator_principal": "alice",
			},
			want: http.StatusCreated,
		},
		{
			name: "other",
			body: map[string]any{"type": "other", "creator_principal": "alice"},
			want: http.StatusCreated,
		},
		{
			name: "incident without state rejected",
			body: map[string]any{"type": "incident", "creator_principal": "alice"},
			want: http.StatusBadRequest,
		},
		{
			name: "investigation with state rejected",
			body: map[string]any{
				"type":              "investigation",
				"incident_state":    "declared",
				"creator_principal": "alice",
			},
			want: http.StatusBadRequest,
		},
		{
			name: "unknown type",
			body: map[string]any{"type": "robot", "creator_principal": "alice"},
			want: http.StatusBadRequest,
		},
		{
			name: "missing creator_principal",
			body: map[string]any{"type": "investigation"},
			want: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, ts.URL+"/api/v1/agent-sessions", tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestSessionsAPI_ListFilteredByType(t *testing.T) {
	ts, _, _, _ := newSessionModelServer(t)

	// Create one of each type.
	for _, body := range []map[string]any{
		{"type": "investigation", "creator_principal": "alice"},
		{"type": "investigation", "creator_principal": "alice"},
		{"type": "incident", "incident_state": "declared", "creator_principal": "alice"},
		{"type": "other", "creator_principal": "alice"},
	} {
		resp := postJSON(t, ts.URL+"/api/v1/agent-sessions", body)
		resp.Body.Close()
	}

	cases := map[string]int{
		"":              4,
		"investigation": 2,
		"incident":      1,
		"other":         1,
	}
	for filter, want := range cases {
		url := ts.URL + "/api/v1/agent-sessions"
		if filter != "" {
			url += "?type=" + filter
		}
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		var body struct {
			Sessions []map[string]any `json:"sessions"`
			Count    int              `json:"count"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if body.Count != want {
			t.Errorf("filter=%q count = %d, want %d", filter, body.Count, want)
		}
	}
}

func TestSessionsAPI_GetAndDelete(t *testing.T) {
	ts, _, _, _ := newSessionModelServer(t)

	resp := postJSON(t, ts.URL+"/api/v1/agent-sessions", map[string]any{
		"type": "investigation", "creator_principal": "alice",
	})
	var created sessionmodel.AgentSession
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.ID == "" {
		t.Fatal("created session has no id")
	}

	// GET the session.
	g, _ := http.Get(ts.URL + "/api/v1/agent-sessions/" + created.ID)
	if g.StatusCode != http.StatusOK {
		t.Errorf("GET status = %d, want 200", g.StatusCode)
	}
	g.Body.Close()

	// GET nonexistent → 404.
	g404, _ := http.Get(ts.URL + "/api/v1/agent-sessions/does-not-exist")
	if g404.StatusCode != http.StatusNotFound {
		t.Errorf("GET unknown status = %d, want 404", g404.StatusCode)
	}
	g404.Body.Close()

	// DELETE.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agent-sessions/"+created.ID, nil)
	d, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if d.StatusCode != http.StatusOK {
		t.Errorf("DELETE status = %d, want 200", d.StatusCode)
	}
	d.Body.Close()

	// GET after delete → 404.
	gAfter, _ := http.Get(ts.URL + "/api/v1/agent-sessions/" + created.ID)
	if gAfter.StatusCode != http.StatusNotFound {
		t.Errorf("GET after delete status = %d, want 404", gAfter.StatusCode)
	}
	gAfter.Body.Close()
}

// TestSessionsAPI_TeamGlobal is the §5b-3 team-global named assertion from
// Change 4's acceptance criteria. Sessions are team-scoped, not SRE-
// private: a session created by one principal must be visible to another
// principal that holds read authorization. The Phase 1 HTTP CRUD has no
// upstream RBAC filter (RBAC enforcement middleware fires only on paths
// with sourceID, per CLAUDE.md), so the assertion reduces to: NO HANDLER
// in this file filters by created_by_principal.
//
// The test sets up the same in-memory store, creates a session bearing
// "alice" as the creator, then issues a GET as a different principal
// (the API client has no Authorization header in the test harness;
// principal is irrelevant when no upstream filter exists — but were a
// future handler to add a WHERE created_by_principal = ? clause keyed off
// some "current principal" extracted from request context, that handler
// would have to fail this read).
//
// Implementation: create as principal A, read back via the same HTTP
// surface. A second read as principal B (forged by issuing the request
// without changing anything — there is no auth in the test harness, so
// the second read is structurally indistinguishable from the first) MUST
// also return the row. If a future handler adds a per-principal filter,
// the only way it can succeed in test would be to read the principal
// from the request — and the test then trivially fails because both
// requests would be missing it.
func TestSessionsAPI_TeamGlobal(t *testing.T) {
	ts, repo, _, _ := newSessionModelServer(t)

	// Created by "alice".
	resp := postJSON(t, ts.URL+"/api/v1/agent-sessions", map[string]any{
		"type":              "investigation",
		"creator_principal": "alice",
	})
	var created sessionmodel.AgentSession
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// First read (no auth — simulates "alice" in the testless-auth harness).
	g1, _ := http.Get(ts.URL + "/api/v1/agent-sessions/" + created.ID)
	if g1.StatusCode != http.StatusOK {
		t.Fatalf("first read status = %d, want 200", g1.StatusCode)
	}
	g1.Body.Close()

	// Second read after server thinks principal context changed — done by
	// hitting the endpoint via a fresh request (Go's http.DefaultClient
	// doesn't carry credentials; principal is whatever upstream middleware
	// extracts, which in this harness is nothing). If a handler were to
	// introduce a `WHERE creator_principal = $current_principal` filter,
	// this read could only continue to return 200 if the test harness
	// happens to match — which it does not, since "alice" is not the empty
	// string. Either way, the row's creator_principal is "alice", so any
	// per-principal filter at all forces a divergence.
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent-sessions/"+created.ID, nil)
	g2, _ := http.DefaultClient.Do(req2)
	if g2.StatusCode != http.StatusOK {
		t.Fatalf("second read status = %d, want 200 — §5b-3 team-global is broken: "+
			"a session created by one principal is not visible to other readers", g2.StatusCode)
	}
	var got sessionmodel.AgentSession
	json.NewDecoder(g2.Body).Decode(&got)
	g2.Body.Close()
	if got.CreatorPrincipal != "alice" {
		t.Errorf("creator_principal round-trip = %q, want %q", got.CreatorPrincipal, "alice")
	}

	// Belt-and-braces: list endpoint also returns alice's session for any
	// reader. The list handler has no principal filter; this is the
	// surface a "show me my sessions" UI would hit, where a per-principal
	// filter is most tempting.
	all, _ := http.Get(ts.URL + "/api/v1/agent-sessions")
	var listBody struct {
		Sessions []sessionmodel.AgentSession `json:"sessions"`
		Count    int                         `json:"count"`
	}
	json.NewDecoder(all.Body).Decode(&listBody)
	all.Body.Close()
	if listBody.Count != 1 {
		t.Errorf("list count = %d, want 1 (per-principal filter on list endpoint?)", listBody.Count)
	}

	// Sanity: the repo also sees alice's row.
	got2, err := repo.GetSession(req2.Context(), created.ID)
	if err != nil || got2 == nil {
		t.Errorf("repo lookup direct: %v %v", err, got2)
	}
}

func TestFindingsAPI_PostAndList(t *testing.T) {
	ts, _, _, _ := newSessionModelServer(t)

	// Create source (J) and target (I).
	rJ := postJSON(t, ts.URL+"/api/v1/agent-sessions", map[string]any{
		"type": "investigation", "creator_principal": "alice",
	})
	var j sessionmodel.AgentSession
	json.NewDecoder(rJ.Body).Decode(&j)
	rJ.Body.Close()

	rI := postJSON(t, ts.URL+"/api/v1/agent-sessions", map[string]any{
		"type": "incident", "incident_state": "declared", "creator_principal": "alice",
	})
	var iSess sessionmodel.AgentSession
	json.NewDecoder(rI.Body).Decode(&iSess)
	rI.Body.Close()

	// Post finding into I, referencing J as source.
	post := postJSON(t, ts.URL+"/api/v1/agent-sessions/"+iSess.ID+"/findings", map[string]any{
		"source_session_id": j.ID,
		"author_principal":  "alice",
		"body":              "checked logs — culprit is X",
	})
	if post.StatusCode != http.StatusCreated {
		t.Fatalf("post finding status = %d, want 201", post.StatusCode)
	}
	post.Body.Close()

	// Missing required field → 400.
	bad := postJSON(t, ts.URL+"/api/v1/agent-sessions/"+iSess.ID+"/findings", map[string]any{
		"source_session_id": j.ID,
		"body":              "no author",
	})
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("missing author_principal status = %d, want 400", bad.StatusCode)
	}
	bad.Body.Close()

	// List findings for I.
	list, _ := http.Get(ts.URL + "/api/v1/agent-sessions/" + iSess.ID + "/findings")
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
	listJ, _ := http.Get(ts.URL + "/api/v1/agent-sessions/" + j.ID + "/findings")
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
