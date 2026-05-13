package skills

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeRepoForTrust(t *testing.T) {
	cases := map[string]string{
		"https://github.com/foo/bar.git": "github.com/foo/bar",
		"https://github.com/foo/bar/":    "github.com/foo/bar",
		"http://github.com/Foo/Bar":      "github.com/foo/bar",
		"git@github.com:foo/bar.git":     "github.com/foo/bar",
		"ssh://git@example.com/x/y":      "example.com/x/y",
		"github.com":                     "github.com",
		"":                               "",
	}
	for in, want := range cases {
		if got := normalizeRepoForTrust(in); got != want {
			t.Errorf("normalizeRepoForTrust(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestManager_Install_TrustedSources_AllowedByHost(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://github.com/myorg/skills.git": {
			commit: "abc123",
			files:  map[string]string{"SKILL.md": skillBytes("ok", "trusted skill", "body")},
		},
	}}
	mgr := NewManager(root, g).WithTrustedSources([]string{"github.com"})

	got, err := mgr.Install(context.Background(), "https://github.com/myorg/skills.git", "", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got.Skills[0].Name != "ok" {
		t.Errorf("unexpected skill name %q", got.Skills[0].Name)
	}
}

func TestManager_Install_TrustedSources_AllowedByOrgPrefix(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://github.com/myorg/skills.git": {
			commit: "abc123",
			files:  map[string]string{"SKILL.md": skillBytes("ok", "trusted org skill", "body")},
		},
	}}
	mgr := NewManager(root, g).WithTrustedSources([]string{"github.com/myorg"})

	if _, err := mgr.Install(context.Background(), "https://github.com/myorg/skills.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
}

func TestManager_Install_TrustedSources_RejectsOtherOrg(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://github.com/evil/skills.git": {
			commit: "abc123",
			files:  map[string]string{"SKILL.md": skillBytes("ok", "untrusted skill", "body")},
		},
	}}
	mgr := NewManager(root, g).WithTrustedSources([]string{"github.com/myorg"})

	_, err := mgr.Install(context.Background(), "https://github.com/evil/skills.git", "", "")
	if err == nil {
		t.Fatal("expected install to fail for non-trusted repo")
	}
	if !strings.Contains(err.Error(), "trusted_sources") {
		t.Errorf("error should mention trusted_sources; got %v", err)
	}
	if g.cloneCalls != 0 {
		t.Errorf("clone should not run when trust check fails; got %d calls", g.cloneCalls)
	}
}

func TestManager_Install_TrustedSources_EmptyAllowlistAcceptsAll(t *testing.T) {
	// The Phase 2 behavior — no allowlist means any URL is accepted — must
	// be preserved when the operator hasn't opted into trusted-source-only
	// mode. Existing installs would otherwise break on upgrade.
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://random.example.com/x.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("ok", "any source", "body")},
		},
	}}
	mgr := NewManager(root, g) // no WithTrustedSources call
	if _, err := mgr.Install(context.Background(), "https://random.example.com/x.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
}
