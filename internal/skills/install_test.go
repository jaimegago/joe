package skills

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fakeGit is a stand-in for ExecGit that materialises the on-disk effect of
// `git clone` and `git fetch && git reset --hard` from a virtual filesystem
// the test owns. Each entry in `repos` is keyed by repo URL and maps to a
// snapshot the next Clone/Update call will copy out.
type fakeGit struct {
	repos       map[string]*fakeRepo
	cloneCalls  int
	updateCalls int
	lastSubdir  string
	lastRef     string
	lastRepoDir string
}

type fakeRepo struct {
	commit string
	// files maps relative paths (forward slashes) to file contents.
	files map[string]string
}

func (f *fakeGit) Clone(_ context.Context, repo, ref, subdir, dest string) (string, error) {
	f.cloneCalls++
	f.lastSubdir = subdir
	f.lastRef = ref
	r, ok := f.repos[repo]
	if !ok {
		return "", &gitError{msg: "fakeGit: unknown repo " + repo}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	for path, content := range r.files {
		if subdir != "" && !strings.HasPrefix(path, strings.TrimSuffix(subdir, "/")+"/") && path != subdir {
			continue
		}
		full := filepath.Join(dest, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return "", err
		}
	}
	return r.commit, nil
}

func (f *fakeGit) Update(_ context.Context, repoDir, ref string) (string, error) {
	f.updateCalls++
	f.lastRepoDir = repoDir
	f.lastRef = ref
	// Find the repo whose commit we should advance to. The test seeds a
	// single repo per fakeGit instance so we just take the first.
	var r *fakeRepo
	for _, v := range f.repos {
		r = v
		break
	}
	if r == nil {
		return "", &gitError{msg: "fakeGit: no repos configured"}
	}
	if err := os.RemoveAll(repoDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", err
	}
	for path, content := range r.files {
		full := filepath.Join(repoDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return "", err
		}
	}
	return r.commit, nil
}

type gitError struct{ msg string }

func (e *gitError) Error() string { return e.msg }

func skillBytes(name, desc, body string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + body + "\n"
}

func TestManager_Install_SingleSkillRepo(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/foo/bar.git": {
			commit: "abc123def456abcdef",
			files: map[string]string{
				"SKILL.md": skillBytes("solo", "solo skill description", "body"),
				"README":   "ignored",
			},
		},
	}}
	mgr := NewManager(root, g)

	got, err := mgr.Install(context.Background(), "https://example.com/foo/bar.git", "", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got.Repo != "https://example.com/foo/bar.git" {
		t.Errorf("Repo = %q", got.Repo)
	}
	if got.Commit != "abc123def456abcdef" {
		t.Errorf("Commit = %q", got.Commit)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "solo" {
		t.Errorf("Skills = %+v", got.Skills)
	}

	// Lockfile persisted with the install.
	lf, err := mgr.LoadLockfile()
	if err != nil {
		t.Fatalf("LoadLockfile: %v", err)
	}
	if len(lf.Installs) != 1 || lf.Installs[0].Dir != got.Dir {
		t.Errorf("lockfile installs = %+v", lf.Installs)
	}

	// The cloned SKILL.md is at the resolved path on disk.
	if _, err := os.Stat(filepath.Join(root, got.Dir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md missing on disk: %v", err)
	}

	// Registry loader should discover the skill (and skip staging).
	reg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if reg.Get("solo") == nil {
		t.Error("LoadDir did not pick up the installed skill")
	}
}

func TestManager_Install_RejectsEmptyRepoAndNoSkills(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/empty.git": {
			commit: "deadbeef",
			files: map[string]string{
				"README.md": "just a readme",
			},
		},
	}}
	mgr := NewManager(root, g)

	if _, err := mgr.Install(context.Background(), "", "", ""); err == nil {
		t.Fatal("expected error on empty repo URL")
	}
	if _, err := mgr.Install(context.Background(), "https://example.com/empty.git", "", ""); err == nil {
		t.Fatal("expected error when repo has no SKILL.md")
	}
	// Failed install must not leave a registry entry.
	lf, _ := mgr.LoadLockfile()
	if len(lf.Installs) != 0 {
		t.Errorf("failed install should not append to lockfile, got %d entries", len(lf.Installs))
	}
}

func TestManager_Install_NameCollision(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/a.git": {
			commit: "111",
			files:  map[string]string{"SKILL.md": skillBytes("shared", "first", "body")},
		},
		"https://example.com/b.git": {
			commit: "222",
			files:  map[string]string{"SKILL.md": skillBytes("shared", "second", "body")},
		},
	}}
	mgr := NewManager(root, g)

	if _, err := mgr.Install(context.Background(), "https://example.com/a.git", "", ""); err != nil {
		t.Fatalf("first install: %v", err)
	}
	_, err := mgr.Install(context.Background(), "https://example.com/b.git", "", "")
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("expected name collision error, got %v", err)
	}
}

func TestManager_Install_Subdir_PassedToGitAndScansSubdir(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/multi.git": {
			commit: "deadbeef",
			files: map[string]string{
				"alpha/SKILL.md": skillBytes("alpha", "alpha desc", "alpha body"),
				"beta/SKILL.md":  skillBytes("beta", "beta desc", "beta body"),
			},
		},
	}}
	mgr := NewManager(root, g)
	got, err := mgr.Install(context.Background(), "https://example.com/multi.git", "main", "alpha")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if g.lastSubdir != "alpha" || g.lastRef != "main" {
		t.Errorf("git call args = subdir=%q ref=%q", g.lastSubdir, g.lastRef)
	}
	if len(got.Skills) != 1 || got.Skills[0].Name != "alpha" {
		t.Errorf("expected single alpha skill, got %+v", got.Skills)
	}
	if got.Subdir != "alpha" {
		t.Errorf("Subdir = %q, want alpha", got.Subdir)
	}
}

func TestManager_Remove_SingleSkill(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/foo.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("solo", "solo", "body")},
		},
	}}
	mgr := NewManager(root, g)
	in, err := mgr.Install(context.Background(), "https://example.com/foo.git", "", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	removed, err := mgr.Remove(context.Background(), "solo", false)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !reflect.DeepEqual(removed, []string{"solo"}) {
		t.Errorf("removed = %v, want [solo]", removed)
	}
	if _, err := os.Stat(filepath.Join(root, in.Dir)); !os.IsNotExist(err) {
		t.Errorf("install dir should be gone, got err=%v", err)
	}
	lf, _ := mgr.LoadLockfile()
	if len(lf.Installs) != 0 {
		t.Errorf("lockfile should be empty after remove, got %d entries", len(lf.Installs))
	}
}

func TestManager_Remove_MultiSkillRequiresForce(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/multi.git": {
			commit: "xyz",
			files: map[string]string{
				"alpha/SKILL.md": skillBytes("alpha", "alpha", "a"),
				"beta/SKILL.md":  skillBytes("beta", "beta", "b"),
			},
		},
	}}
	mgr := NewManager(root, g)
	if _, err := mgr.Install(context.Background(), "https://example.com/multi.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := mgr.Remove(context.Background(), "alpha", false); err == nil {
		t.Fatal("expected error without --force on multi-skill install")
	}
	removed, err := mgr.Remove(context.Background(), "alpha", true)
	if err != nil {
		t.Fatalf("Remove(force): %v", err)
	}
	sort.Strings(removed)
	if !reflect.DeepEqual(removed, []string{"alpha", "beta"}) {
		t.Errorf("removed = %v, want [alpha beta]", removed)
	}
}

func TestManager_Update_SingleInstall(t *testing.T) {
	root := t.TempDir()
	repo := "https://example.com/foo.git"
	g := &fakeGit{repos: map[string]*fakeRepo{
		repo: {
			commit: "v1",
			files:  map[string]string{"SKILL.md": skillBytes("solo", "v1", "body v1")},
		},
	}}
	mgr := NewManager(root, g)
	if _, err := mgr.Install(context.Background(), repo, "main", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Bump the fake repo so Update returns a new commit + new body.
	g.repos[repo].commit = "v2"
	g.repos[repo].files["SKILL.md"] = skillBytes("solo", "v2", "body v2")

	updated, err := mgr.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated) != 1 || updated[0].Commit != "v2" {
		t.Errorf("Update result = %+v", updated)
	}
	if g.lastRef != "main" {
		t.Errorf("expected fetch ref=main, got %q", g.lastRef)
	}

	// Hash on the lockfile should track the new body.
	lf, _ := mgr.LoadLockfile()
	if got := lf.Installs[0].Skills[0].Hash; got == "" {
		t.Error("updated skill hash should not be empty")
	}
}

func TestManager_Update_TargetedSkillNotFound(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, &fakeGit{repos: map[string]*fakeRepo{}})
	if _, err := mgr.Update(context.Background(), "missing"); err == nil {
		t.Fatal("expected error updating missing skill")
	}
}

func TestManager_List_EmptyAndPopulated(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/foo.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("solo", "solo", "body")},
		},
	}}
	mgr := NewManager(root, g)

	installs, err := mgr.List()
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(installs) != 0 {
		t.Errorf("expected empty list, got %d", len(installs))
	}

	if _, err := mgr.Install(context.Background(), "https://example.com/foo.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installs, _ = mgr.List()
	if len(installs) != 1 || installs[0].Skills[0].Name != "solo" {
		t.Errorf("unexpected list: %+v", installs)
	}
}

func TestDeriveInstallDir(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/foo/bar.git", "github.com-foo-bar"},
		{"https://github.com/foo/bar", "github.com-foo-bar"},
		{"git@github.com:foo/bar.git", "github.com-foo-bar"},
		{"ssh://git@gitlab.com/group/proj.git", "gitlab.com-group-proj"},
	}
	for _, tc := range cases {
		if got := deriveInstallDir(tc.in); got != tc.want {
			t.Errorf("deriveInstallDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateRepoURL(t *testing.T) {
	good := []string{
		"https://github.com/foo/bar.git",
		"http://example.com/x.git",
		"ssh://git@example.com/x.git",
		"git@github.com:foo/bar.git",
	}
	for _, u := range good {
		if err := validateRepoURL(u); err != nil {
			t.Errorf("validateRepoURL(%q) unexpected error: %v", u, err)
		}
	}
	bad := []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"not-a-url",
		"https://",
		"foo\nbar",
	}
	for _, u := range bad {
		if err := validateRepoURL(u); err == nil {
			t.Errorf("validateRepoURL(%q) should have failed", u)
		}
	}
}

func TestCleanSubdir(t *testing.T) {
	cases := map[string]string{
		"":         "",
		".":        "",
		"foo":      "foo",
		"./foo":    "foo",
		"/foo":     "foo",
		"foo/":     "foo",
		"foo/bar":  "foo/bar",
		"foo\\bar": "foo/bar",
	}
	for in, want := range cases {
		if got := cleanSubdir(in); got != want {
			t.Errorf("cleanSubdir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadLockfile_MalformedYAML(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, LockfileName), []byte("not: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(root, &fakeGit{})
	if _, err := mgr.LoadLockfile(); err == nil {
		t.Fatal("expected error on malformed lockfile")
	}
}

func TestLoadDir_SkipsHiddenStaging(t *testing.T) {
	root := t.TempDir()
	// Live skill.
	live := filepath.Join(root, "live")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "SKILL.md"), []byte(skillBytes("live", "live", "body")), 0o644); err != nil {
		t.Fatal(err)
	}
	// Half-cloned staged skill that the loader must ignore.
	staged := filepath.Join(root, ".staging", "install-xyz")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "SKILL.md"), []byte(skillBytes("staged", "staged", "body")), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if reg.Get("live") == nil {
		t.Error("live skill should have been loaded")
	}
	if reg.Get("staged") != nil {
		t.Error("staged skill must not activate from .staging/")
	}
}
