package git

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/jaimegago/joe/internal/store"
)

// --- resolveAuth internal tests ---
//
// resolveAuth is the git adapter's use-time credential seam (D-0150). It replaces
// the former buildAuth, which read an inline http_token / ssh_key_path straight
// out of the component config; those fields no longer exist and are refused at
// registration. What these pin is the mapping from a resolved credential to a
// go-git AuthMethod, including the two ways a nil (anonymous) method is correct.

func resolveAuthFor(t *testing.T, config string) (transport.AuthMethod, error) {
	t.Helper()
	return resolveAuth(context.Background(), store.Component{
		ID: "c-git", Type: store.ComponentTypeGit, Config: json.RawMessage(config),
	})
}

// A component armed with the explicit no-credential kind clones ANONYMOUSLY: the
// none provider resolves successfully and yields no credential, so the auth
// method is nil and go-git makes an unauthenticated fetch. This is the whole
// point of the none arm, so it is pinned rather than assumed.
func TestResolveAuth_NoneKindIsAnonymous(t *testing.T) {
	auth, err := resolveAuthFor(t, `{"url":"https://example.com/r.git","credential_provider":"none"}`)
	if err != nil {
		t.Fatalf("resolveAuth(none) error = %v", err)
	}
	if auth != nil {
		t.Errorf("resolveAuth(none) = %v, want nil (anonymous clone)", auth)
	}
}

// An UNPROMOTED component has no discriminator, so Select defaults to the static
// provider, which resolves an empty value. That must also be anonymous rather
// than an error: the component is inert, and whether the remote accepts an
// anonymous fetch is the remote's answer to give.
func TestResolveAuth_UnpromotedIsAnonymous(t *testing.T) {
	auth, err := resolveAuthFor(t, `{"url":"https://example.com/r.git"}`)
	if err != nil {
		t.Fatalf("resolveAuth(unpromoted) error = %v", err)
	}
	if auth != nil {
		t.Errorf("resolveAuth(unpromoted) = %v, want nil (anonymous clone)", auth)
	}
}

// A static reference resolves the token from the named environment variable at
// USE TIME and applies it as HTTPS basic auth with the token as the password —
// the convention every major forge accepts for a personal access token.
func TestResolveAuth_StaticReferenceBecomesHTTPSBasicAuth(t *testing.T) {
	t.Setenv("JOE_GIT_PROD", "ghp_example")
	auth, err := resolveAuthFor(t,
		`{"url":"https://example.com/r.git","credential_provider":"static","env_var":"JOE_GIT_PROD"}`)
	if err != nil {
		t.Fatalf("resolveAuth(static) error = %v", err)
	}
	basic, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatalf("resolveAuth(static) = %T, want *githttp.BasicAuth", auth)
	}
	if basic.Password != "ghp_example" {
		t.Errorf("resolved token did not reach the auth method")
	}
}

// A static reference naming an UNSET variable is an operational failure, and the
// error carries the provider's non-sensitive reason only.
func TestResolveAuth_StaticReferenceUnsetVariableFails(t *testing.T) {
	_, err := resolveAuthFor(t,
		`{"url":"https://example.com/r.git","credential_provider":"static","env_var":"JOE_GIT_DEFINITELY_UNSET"}`)
	if err == nil {
		t.Fatal("resolveAuth with an unset referenced variable should fail")
	}
	if !strings.Contains(err.Error(), "not set") {
		t.Errorf("error should carry the provider's reason, got: %v", err)
	}
}

// --- repoDir ---

func TestRepoDir_Success(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := repoDir("https://github.com/example/repo.git")
	if err != nil {
		t.Fatalf("repoDir() error = %v", err)
	}
	if path == "" {
		t.Error("repoDir() should return non-empty path")
	}
}

// TestResolveCommit_HeadError tests resolveCommit("HEAD") on an empty repo (no commits).
func TestResolveCommit_HeadError(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.resolveCommit("HEAD")
	if err == nil {
		t.Error("resolveCommit(HEAD) on empty repo should return error")
	}
}

// TestLog_EmptyRepo tests Log on an empty repo where repo.Log fails.
func TestLog_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.Log(context.Background(), 10)
	if err == nil {
		t.Error("Log() on empty repo should return error")
	}
}

// TestReadFile_EmptyRepo tests ReadFile on an empty repo where Head fails.
func TestReadFile_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.ReadFile(context.Background(), "README.md")
	if err == nil {
		t.Error("ReadFile() on empty repo should return error")
	}
}

// TestListFiles_EmptyRepo tests ListFiles on an empty repo where Head fails.
func TestListFiles_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init empty repo: %v", err)
	}
	a := &Adapter{repo: repo, connected: true}
	_, err = a.ListFiles(context.Background(), "")
	if err == nil {
		t.Error("ListFiles() on empty repo should return error")
	}
}
