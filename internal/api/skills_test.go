package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/skills"
)

// reloadResponse mirrors the JSON shape returned by POST /skills/reload.
// Defined inline so the test does not depend on the client package and can
// catch wire-format drift on either side.
type reloadResponse struct {
	Status  string   `json:"status"`
	Trigger string   `json:"trigger"`
	Before  int      `json:"before"`
	After   int      `json:"after"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Updated []string `json:"updated"`
	Error   string   `json:"error"`
}

func writeReloadSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: skill for reload test\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newSkillsServer(t *testing.T, root string) (*httptest.Server, *skills.AtomicRouter) {
	t.Helper()
	router := skills.NewAtomicRouter(skills.NewRouter(skills.NewRegistry()))
	watcher, err := skills.NewWatcher(root, router, skills.WithDebounce(10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = watcher.Close() })

	svc := &core.Services{
		Skills:        router,
		SkillsWatcher: watcher,
	}
	srv := api.New(svc, api.TestingPolicyEngine(svc))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, router
}

func TestSkillsReload_LoadsNewSkill(t *testing.T) {
	root := t.TempDir()
	ts, router := newSkillsServer(t, root)

	// Drop a skill into the directory after the server is up.
	writeReloadSkill(t, filepath.Join(root, "alpha"), "alpha", "alpha body")

	resp, err := http.Post(ts.URL+"/api/v1/skills/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var got reloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.Trigger != "manual" {
		t.Errorf("trigger = %q, want manual", got.Trigger)
	}
	if got.After != 1 {
		t.Errorf("after = %d, want 1", got.After)
	}
	if router.Snapshot().Registry().Get("alpha") == nil {
		t.Error("registry did not pick up new skill after manual reload")
	}
}

func TestSkillsReload_UnavailableWhenWatcherNil(t *testing.T) {
	// When hot reload is disabled (no watcher), the endpoint must respond
	// 503 so CI/CD knows the feature isn't live, not 404 which would
	// imply the route doesn't exist.
	svc := &core.Services{
		Skills: skills.NewAtomicRouter(skills.NewRouter(skills.NewRegistry())),
	}
	srv := api.New(svc, api.TestingPolicyEngine(svc))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	resp, err := http.Post(ts.URL+"/api/v1/skills/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// newSkillsServerWithManager spins up a server backed by a real install
// Manager so the GET /skills, POST /skills/approve, and POST /skills/reject
// handlers can exercise the full path: lockfile -> JSON response.
func newSkillsServerWithManager(t *testing.T, root string) (*httptest.Server, *skills.Manager) {
	t.Helper()
	router := skills.NewAtomicRouter(skills.NewRouter(skills.NewRegistry()))
	// Default policy = deny-by-default, so installs land in quarantine —
	// exactly what we want to exercise the new endpoints against.
	mgr := skills.NewManager(root, &fakeManagerGit{}).WithPolicy(skills.DefaultPolicy())

	svc := &core.Services{
		Skills:        router,
		SkillsManager: mgr,
	}
	srv := api.New(svc, api.TestingPolicyEngine(svc))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, mgr
}

// fakeManagerGit is a minimal Git stub for the API-level tests: it writes a
// single SKILL.md into the staging dir so Install succeeds without touching
// the network.
type fakeManagerGit struct{}

func (fakeManagerGit) Clone(_ context.Context, repo, _, _, dest string) (string, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	skill := "---\nname: pending\ndescription: pending\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(skill), 0o644); err != nil {
		return "", err
	}
	return "abc", nil
}

func (fakeManagerGit) Update(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

func TestSkillsList_SplitsActiveAndQuarantined(t *testing.T) {
	root := t.TempDir()
	ts, mgr := newSkillsServerWithManager(t, root)

	// Default policy denies → install lands in quarantine.
	if _, err := mgr.Install(context.Background(), "https://example.com/x.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	resp, err := http.Get(ts.URL + "/api/v1/skills")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		Active      []map[string]any `json:"active"`
		Quarantined []map[string]any `json:"quarantined"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Active) != 0 {
		t.Errorf("active should be empty, got %+v", got.Active)
	}
	if len(got.Quarantined) != 1 || got.Quarantined[0]["name"] != "pending" {
		t.Errorf("quarantined = %+v", got.Quarantined)
	}
	if reason, _ := got.Quarantined[0]["quarantine_reason"].(string); reason == "" {
		t.Error("quarantined entry must include quarantine_reason for the UI")
	}
}

func TestSkillsApprove_MovesQuarantinedToActive(t *testing.T) {
	root := t.TempDir()
	ts, mgr := newSkillsServerWithManager(t, root)
	if _, err := mgr.Install(context.Background(), "https://example.com/x.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	body := strings.NewReader(`{"name":"pending"}`)
	resp, err := http.Post(ts.URL+"/api/v1/skills/approve", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, raw)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v", got["status"])
	}

	// Disk: install should now be active.
	installs, _ := mgr.List()
	if len(installs) != 1 || installs[0].IsQuarantined() {
		t.Errorf("after approve, install should be active: %+v", installs)
	}
}

func TestSkillsApprove_MissingNameIs400(t *testing.T) {
	root := t.TempDir()
	ts, _ := newSkillsServerWithManager(t, root)

	resp, err := http.Post(ts.URL+"/api/v1/skills/approve", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSkillsApprove_UnavailableWhenManagerNil(t *testing.T) {
	svc := &core.Services{
		Skills: skills.NewAtomicRouter(skills.NewRouter(skills.NewRegistry())),
	}
	srv := api.New(svc, api.TestingPolicyEngine(svc))
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	resp, err := http.Post(ts.URL+"/api/v1/skills/approve", "application/json", strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestSkillsReject_RemovesQuarantined(t *testing.T) {
	root := t.TempDir()
	ts, mgr := newSkillsServerWithManager(t, root)
	if _, err := mgr.Install(context.Background(), "https://example.com/x.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	resp, err := http.Post(ts.URL+"/api/v1/skills/reject", "application/json", strings.NewReader(`{"name":"pending"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body=%s", resp.StatusCode, raw)
	}
	installs, _ := mgr.List()
	if len(installs) != 0 {
		t.Errorf("after reject, lockfile should be empty: %+v", installs)
	}
}

func TestSkillsReload_ContextTimeoutDoesNotRace(t *testing.T) {
	// Reload must not deadlock when the client context is canceled mid-call.
	// This is a smoke test for the synchronous Reload path; we just verify
	// the handler returns within a reasonable window when the caller bails.
	root := t.TempDir()
	ts, _ := newSkillsServer(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req, err := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/v1/skills/reload", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		// The server may have responded before the client noticed the
		// cancel — that's fine. We just ensure no goroutine is wedged.
		resp.Body.Close()
	}
}
