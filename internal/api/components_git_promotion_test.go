package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// repo-registration-path / D-0150 — the operator path to a git_read-able
// repository. These pin the whole chain the change restores: git registers, arms
// with either of its two credential kinds, and answers a read.
//
// The pre-change behaviour these replace was total refusal: registration 400'd on
// the unregistrable type and promotion 400'd on reject-unwired, so no git
// component could exist, let alone be read.

// --- registration ---

// TestGitRegisters proves git is registrable again through the governed create
// path, carrying its routing config (the repository URL) and the optional
// provider sibling declaration.
func TestGitRegisters(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "gh-main", "github", `{}`)
	registerComponent(t, f, "repo-main", "git",
		`{"url":"https://example.com/org/repo.git","provider_component_id":"gh-main"}`)

	cfg := componentConfigMap(t, f, "repo-main")
	if cfg["url"] != "https://example.com/org/repo.git" {
		t.Errorf("registration dropped the repository URL: config=%v", cfg)
	}
	if cfg["provider_component_id"] != "gh-main" {
		t.Errorf("registration dropped the provider declaration: config=%v", cfg)
	}
	// Registration is credential-less: the component lands inert.
	if _, armed := cfg["credential_provider"]; armed {
		t.Errorf("registration armed the component: config=%v", cfg)
	}
}

// TestGitRegistration_RefusesInlineCredentials proves the inline auth fields the
// git adapter used to consume are REFUSED at registration rather than accepted
// and persisted as inert-but-live secrets. The adapter no longer parses them, so
// the reflection-derived denylist cannot see them; they are held in the retired
// inline-fields declaration precisely so this path stays closed.
func TestGitRegistration_RefusesInlineCredentials(t *testing.T) {
	for _, field := range []string{"http_token", "ssh_key_path", "value", "env_var"} {
		t.Run(field, func(t *testing.T) {
			f := newLLMAdminFixture(t, true)
			f.markAdmin("user:alice")
			body := `{"id":"repo-x","type":"git","name":"repo","config":` +
				`{"url":"https://example.com/r.git","` + field + `":"secret-ish"}}`
			w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("registration with %q: status=%d body=%s; want 400", field, w.Code, w.Body.String())
			}
			comp, err := f.store.Components.Get(context.Background(), "repo-x")
			if err != nil {
				t.Fatalf("get component: %v", err)
			}
			if comp != nil {
				t.Errorf("registration with %q persisted the component despite rejection", field)
			}
		})
	}
}

// TestGitRegistration_ValidatesProviderReferenceShape proves the sibling
// declaration is shape-validated at registration against the component-ID format
// rule, while a reference naming no existing component is ACCEPTED — the
// declaration is a discovery hint, and making registration order-dependent would
// be a worse defect than a reference that resolves to nothing.
func TestGitRegistration_ValidatesProviderReferenceShape(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	body := `{"id":"repo-bad","type":"git","name":"repo","config":` +
		`{"url":"https://example.com/r.git","provider_component_id":"Not A Valid ID"}}`
	w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed provider reference: status=%d body=%s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "provider_component_id") {
		t.Errorf("rejection did not name the offending field: %s", w.Body.String())
	}

	// A well-formed reference to a component that does not exist is legal.
	registerComponent(t, f, "repo-dangling", "git",
		`{"url":"https://example.com/r.git","provider_component_id":"gh-not-registered-yet"}`)
}

// --- promotion: the static HTTPS-token reference ---

// TestGitPromote_StaticReference proves a git static promotion behaves exactly as
// every other static promotion: an env_var indirection is required, an inline
// value is refused, and the armed config carries the reference and the
// discriminator.
func TestGitPromote_StaticReference(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "repo-priv", "git", `{"url":"https://example.com/org/private.git"}`)

	// An inline secret is refused, as at every other promotion.
	w := f.do(http.MethodPost, "/api/v1/components/repo-priv/promote",
		`{"credential_provider":"static","value":"ghp_literal"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("inline value: status=%d body=%s; want 400", w.Code, w.Body.String())
	}

	w = f.do(http.MethodPost, "/api/v1/components/repo-priv/promote",
		`{"credential_provider":"static","env_var":"JOE_GIT_PROD"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("static promote: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	cfg := componentConfigMap(t, f, "repo-priv")
	if cfg["credential_provider"] != "static" {
		t.Errorf("armed config did not record the static kind: config=%v", cfg)
	}
	if cfg["env_var"] != "JOE_GIT_PROD" {
		t.Errorf("armed config did not record the reference: config=%v", cfg)
	}
	if cfg["url"] != "https://example.com/org/private.git" {
		t.Errorf("promotion dropped the routing config: config=%v", cfg)
	}
}

// --- promotion: the explicit no-credential arm ---

// TestGitPromote_NoneIsMandatoryAndDurable proves the no-credential arm is a
// deliberate, durable, audited statement rather than a defaulted absence: an
// unpromoted public repository is NOT armed, arming it with the none kind records
// that kind in the config, and the audit row states the unauthenticated reach-out
// in terms a later reader can act on.
func TestGitPromote_NoneIsMandatoryAndDurable(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "repo-pub", "git", `{"url":"https://example.com/org/public.git"}`)

	// Mandatory: registration alone leaves the component inert. There is no
	// "public repositories need no promotion" shortcut.
	if cfg := componentConfigMap(t, f, "repo-pub"); cfg["credential_provider"] != nil {
		t.Fatalf("a registered public repository was armed without promotion: config=%v", cfg)
	}

	w := f.do(http.MethodPost, "/api/v1/components/repo-pub/promote",
		`{"credential_provider":"none"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("none promote: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Provider string `json:"provider"`
		Armed    bool   `json:"armed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode promote response: %v", err)
	}
	if resp.Provider != "none" || !resp.Armed {
		t.Errorf("promote response = %+v; want provider=none armed=true", resp)
	}

	cfg := componentConfigMap(t, f, "repo-pub")
	if cfg["credential_provider"] != "none" {
		t.Errorf("armed config did not durably record the none kind: config=%v", cfg)
	}
	for _, locator := range []string{"env_var", "value", "in_cluster"} {
		if _, present := cfg[locator]; present {
			t.Errorf("the no-credential arm wrote locator %q: config=%v", locator, cfg)
		}
	}

	// The durable record must make the unauthenticated reach-out legible. An
	// empty reference alone reads as "nothing recorded", not as the statement it
	// is, so the row says it outright.
	blob := auditContextRaw(t, f, audit.ActionComponentPromote)
	if !strings.Contains(blob, `"unauthenticated":true`) {
		t.Errorf("audit row does not state the unauthenticated reach-out: %s", blob)
	}
	if !strings.Contains(blob, "no credential") {
		t.Errorf("audit row does not describe what was authorized: %s", blob)
	}
	if !strings.Contains(blob, `"provider":"none"`) {
		t.Errorf("audit row does not record the provider kind: %s", blob)
	}
}

// TestGitPromote_NoneRefusesLocators proves a locator supplied alongside the
// no-credential arm is REFUSED rather than ignored: silently dropping it would
// arm an unauthenticated component while the operator believed they had supplied
// a credential.
func TestGitPromote_NoneRefusesLocators(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "repo-pub", "git", `{"url":"https://example.com/r.git"}`)

	for _, body := range []string{
		`{"credential_provider":"none","env_var":"JOE_GIT_PROD"}`,
		`{"credential_provider":"none","value":"ghp_literal"}`,
		`{"credential_provider":"none","in_cluster":true}`,
	} {
		w := f.do(http.MethodPost, "/api/v1/components/repo-pub/promote", body, "user:alice")
		if w.Code != http.StatusBadRequest {
			t.Errorf("promote %s: status=%d body=%s; want 400", body, w.Code, w.Body.String())
		}
	}
	if cfg := componentConfigMap(t, f, "repo-pub"); cfg["credential_provider"] != nil {
		t.Errorf("a refused promotion armed the component: config=%v", cfg)
	}
}

// TestGitPromote_RefusesForeignKind proves git's two selectable kinds are the
// whole set: a kind belonging to another type is refused.
func TestGitPromote_RefusesForeignKind(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "repo-x", "git", `{"url":"https://example.com/r.git"}`)

	w := f.do(http.MethodPost, "/api/v1/components/repo-x/promote",
		`{"credential_provider":"static-bearer","env_var":"JOE_GIT_PROD"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("foreign kind: status=%d body=%s; want 400", w.Code, w.Body.String())
	}
}

// --- end to end: a none-armed repository answers a read ---

// TestGitEndToEnd_NoneArmedRepositoryIsReadable is the acceptance test for the
// whole item: an operator registers a repository, arms it with the no-credential
// kind, and git_read returns its contents through the governed accessor — the
// chain that had no operator path at all before this change.
//
// The repository is a real on-disk git repository cloned over a local path, so
// the test exercises the actual go-git clone the adapter performs with a real
// anonymous (nil) auth method, without reaching the network.
func TestGitEndToEnd_NoneArmedRepositoryIsReadable(t *testing.T) {
	// The adapter clones under the joe home, which resolves from HOME.
	t.Setenv("HOME", t.TempDir())
	origin := seedLocalRepo(t, "README.md", "joe reads this\n")

	f := newLLMAdminFixtureWithAdapters(t)
	f.markAdmin("user:alice")
	registerComponent(t, f, "repo-pub", "git", `{"url":`+jsonString(origin)+`}`)

	w := f.do(http.MethodPost, "/api/v1/components/repo-pub/promote",
		`{"credential_provider":"none"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("none promote: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	// Promotion registers the live adapter eagerly, which is what performed the
	// clone. Read back through the guarded accessor as the git_read tool does.
	content, err := f.server.accessor.GitReadFile(
		context.Background(), rbac.Principal("user:alice"), "repo-pub", "README.md")
	if err != nil {
		t.Fatalf("git read through the accessor: %v", err)
	}
	if content != "joe reads this\n" {
		t.Errorf("git read returned %q, want the seeded file contents", content)
	}
}

// seedLocalRepo creates a real git repository on disk with one committed file and
// returns its path, usable directly as a clone URL.
func seedLocalRepo(t *testing.T, name, contents string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := wt.Commit("seed", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Unix(0, 0)},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir
}

// jsonString quotes a value for embedding in a JSON literal.
func jsonString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}
