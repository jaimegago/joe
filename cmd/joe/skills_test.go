package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/skills"
)

type fakeSkillManager struct {
	installResp *skills.Install
	installErr  error
	removeResp  []string
	removeErr   error
	updateResp  []*skills.Install
	updateErr   error
	listResp    []skills.Install
	listErr     error

	installArgs struct {
		repo, ref, subdir string
	}
	removeArgs struct {
		name  string
		force bool
	}
	updateArg string
}

func (f *fakeSkillManager) Install(_ context.Context, repo, ref, subdir string) (*skills.Install, error) {
	f.installArgs.repo = repo
	f.installArgs.ref = ref
	f.installArgs.subdir = subdir
	return f.installResp, f.installErr
}

func (f *fakeSkillManager) Remove(_ context.Context, name string, force bool) ([]string, error) {
	f.removeArgs.name = name
	f.removeArgs.force = force
	return f.removeResp, f.removeErr
}

func (f *fakeSkillManager) Update(_ context.Context, name string) ([]*skills.Install, error) {
	f.updateArg = name
	return f.updateResp, f.updateErr
}

func (f *fakeSkillManager) List() ([]skills.Install, error) {
	return f.listResp, f.listErr
}

func skillsDeps(t *testing.T, joeDir string, mgr *fakeSkillManager) runDeps {
	deps := testDeps(&fakeRepl{}, joeDir)
	deps.newSkillManager = func(root string, trusted []string) skillManager {
		return mgr
	}
	return deps
}

func TestRunSkillsCommand_NoArgs(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("expected usage in stderr, got %q", stderr.String())
	}
}

func TestRunSkillsCommand_Install_Success(t *testing.T) {
	mgr := &fakeSkillManager{
		installResp: &skills.Install{
			Repo:   "https://example.com/foo.git",
			Commit: "abcdef0123456789",
			Skills: []skills.SkillRecord{{Name: "a"}, {Name: "b"}},
		},
	}
	deps := skillsDeps(t, t.TempDir(), mgr)

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "install", "https://example.com/foo.git", "--ref", "main", "--subdir", "alpha"},
		&stdout, &stderr, deps,
	)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	if mgr.installArgs.repo != "https://example.com/foo.git" || mgr.installArgs.ref != "main" || mgr.installArgs.subdir != "alpha" {
		t.Errorf("install args = %+v", mgr.installArgs)
	}
	out := stdout.String()
	if !strings.Contains(out, "Installed https://example.com/foo.git") || !strings.Contains(out, "abcdef012345") {
		t.Errorf("stdout missing install summary: %q", out)
	}
	if !strings.Contains(out, "joe-core will pick up the new skills") {
		t.Errorf("install should mention hot reload behaviour, got %q", out)
	}
}

func TestRunSkillsCommand_Install_MissingURL(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "install"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "repo-url") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunSkillsCommand_Install_ManagerError(t *testing.T) {
	mgr := &fakeSkillManager{installErr: errors.New("clone refused")}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "install", "https://example.com/foo.git"},
		&stdout, &stderr, deps,
	)
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "clone refused") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunSkillsCommand_List_Empty(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "list"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "No skills installed") {
		t.Errorf("expected empty-state hint, got %q", stdout.String())
	}
}

func TestRunSkillsCommand_List_Populated(t *testing.T) {
	mgr := &fakeSkillManager{
		listResp: []skills.Install{{
			Repo: "https://example.com/foo.git",
			Ref:  "main",
			Skills: []skills.SkillRecord{
				{Name: "alpha"},
				{Name: "beta"},
			},
		}},
	}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "list"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"alpha", "beta", "main", "https://example.com/foo.git"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q in %q", want, out)
		}
	}
}

func TestRunSkillsCommand_Remove_Success(t *testing.T) {
	mgr := &fakeSkillManager{removeResp: []string{"alpha"}}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "remove", "alpha"},
		&stdout, &stderr, deps,
	)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	if mgr.removeArgs.name != "alpha" || mgr.removeArgs.force {
		t.Errorf("remove args = %+v", mgr.removeArgs)
	}
	if !strings.Contains(stdout.String(), `Removed skill "alpha"`) {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunSkillsCommand_Remove_Force(t *testing.T) {
	mgr := &fakeSkillManager{removeResp: []string{"alpha", "beta"}}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "remove", "alpha", "--force"},
		&stdout, &stderr, deps,
	)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	if !mgr.removeArgs.force {
		t.Errorf("expected force=true, got %+v", mgr.removeArgs)
	}
	if !strings.Contains(stdout.String(), "Removed 2 skill(s)") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunSkillsCommand_Remove_MissingName(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "remove"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
}

func TestRunSkillsCommand_Update_All(t *testing.T) {
	mgr := &fakeSkillManager{
		updateResp: []*skills.Install{{
			Repo:   "https://example.com/foo.git",
			Commit: "newcommit1234567",
			Skills: []skills.SkillRecord{{Name: "alpha"}},
		}},
	}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "update"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	if mgr.updateArg != "" {
		t.Errorf("expected empty update arg, got %q", mgr.updateArg)
	}
	if !strings.Contains(stdout.String(), "Updated https://example.com/foo.git") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunSkillsCommand_Update_Targeted(t *testing.T) {
	mgr := &fakeSkillManager{
		updateResp: []*skills.Install{{
			Repo:   "https://example.com/foo.git",
			Commit: "newcommit1234567",
			Skills: []skills.SkillRecord{{Name: "alpha"}},
		}},
	}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "update", "alpha"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	if mgr.updateArg != "alpha" {
		t.Errorf("expected update arg=alpha, got %q", mgr.updateArg)
	}
}

func TestRunSkillsCommand_Update_TooManyArgs(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "update", "a", "b"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
}

func TestRunSkillsCommand_Update_Empty(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "update"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout.String(), "Nothing to update") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestRunSkillsCommand_Unknown(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "frobnicate"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "Unknown skills subcommand") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunSkillsCommand_JoeDirError(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	deps.joeDirPath = func() (string, error) {
		return "", errors.New("no home")
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "list"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "Joe config directory") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunSkillsCommand_Reload_Success(t *testing.T) {
	// Spin up a fake joe-core that returns a reload summary, then call
	// `joe skills reload` against it. Verifies the client+CLI wire shape.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/reload" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"trigger": "manual",
			"before":  1,
			"after":   2,
			"added":   []string{"new-skill"},
		})
	}))
	t.Cleanup(ts.Close)
	addr := strings.TrimPrefix(ts.URL, "http://")

	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	deps.loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			Server: config.ServerConfig{Address: addr},
		}, nil
	}
	// newClient is the real one via defaultRunDeps so it talks to ts.
	deps.newClient = client.New

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "reload"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Reload ok") || !strings.Contains(out, "1 skill(s) before, 2 after") {
		t.Errorf("stdout missing summary: %q", out)
	}
	if !strings.Contains(out, "new-skill") {
		t.Errorf("stdout missing Added list: %q", out)
	}
}

func TestRunSkillsCommand_Reload_FailureExitCode(t *testing.T) {
	// joe-core returns 500 with a failure payload. The CLI must propagate
	// a non-zero exit code so CI/CD pipelines fail loudly.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal","message":"reload failed: walk error"}`))
	}))
	t.Cleanup(ts.Close)
	addr := strings.TrimPrefix(ts.URL, "http://")

	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	deps.loadConfig = func(string) (*config.Config, error) {
		return &config.Config{Server: config.ServerConfig{Address: addr}}, nil
	}
	deps.newClient = client.New

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "reload"}, &stdout, &stderr, deps)
	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0 (stdout=%q)", stdout.String())
	}
	if !strings.Contains(stderr.String(), "reload failed") {
		t.Errorf("stderr missing failure detail: %q", stderr.String())
	}
}

func TestRunSkillsCommand_Reload_RejectsPositionalArgs(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	deps.loadConfig = func(string) (*config.Config, error) {
		return &config.Config{}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "reload", "extra"}, &stdout, &stderr, deps,
	)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no positional") {
		t.Errorf("stderr should explain the misuse: %q", stderr.String())
	}
}

func TestRunSkillsCommand_Install_PassesTrustedSourcesFromConfig(t *testing.T) {
	// Verifies the wiring between config.Skills.TrustedSources and the
	// manager factory. Without this, the trust check happens with an
	// empty allowlist and silently accepts everything.
	mgr := &fakeSkillManager{installResp: &skills.Install{Skills: []skills.SkillRecord{{Name: "x"}}}}
	deps := skillsDeps(t, t.TempDir(), mgr)
	deps.loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			Skills: config.SkillsConfig{TrustedSources: []string{"github.com/myorg"}},
		}, nil
	}
	var observed []string
	deps.newSkillManager = func(root string, trusted []string) skillManager {
		observed = trusted
		return mgr
	}

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(),
		[]string{"skills", "install", "https://github.com/myorg/skills.git"},
		&stdout, &stderr, deps,
	)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, stderr.String())
	}
	if len(observed) != 1 || observed[0] != "github.com/myorg" {
		t.Errorf("trusted sources not threaded through; got %v", observed)
	}
}
