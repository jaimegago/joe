package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// quarantinePolicyAcceptingTrusted returns a Policy that auto-approves any
// repo under a single trusted source. Used to drive both quarantine and
// auto-approve paths from one helper.
func quarantinePolicyAcceptingTrusted(trusted string, newSkillsOk bool) *Policy {
	return &Policy{
		Version:        1,
		TrustedSources: []string{trusted},
		AutoApprove: AutoApprovePolicy{
			TrustedSources:           true,
			NewSkillsInExistingRepos: newSkillsOk,
		},
	}
}

func TestManager_Install_QuarantinesWhenPolicyDenies(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/foo/bar.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("untrusted", "untrusted", "body")},
		},
	}}
	// Default policy denies everything — install must land in quarantine,
	// not get rejected outright.
	mgr := NewManager(root, g).WithPolicy(DefaultPolicy())

	in, err := mgr.Install(context.Background(), "https://example.com/foo/bar.git", "", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !in.IsQuarantined() {
		t.Errorf("expected quarantined install, got status=%q", in.Status)
	}
	if in.QuarantineReason == "" {
		t.Error("quarantined install must carry a reason for the audit log")
	}

	// Directory lives under .quarantine/ — invisible to LoadDir.
	qPath := filepath.Join(root, quarantineDirName, in.Dir)
	if _, err := os.Stat(qPath); err != nil {
		t.Errorf("quarantine path missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, in.Dir)); !os.IsNotExist(err) {
		t.Error("quarantined install must not appear in the active root")
	}

	// The registry loader skips .quarantine/ (it's a dotfile dir).
	reg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if reg.Get("untrusted") != nil {
		t.Error("quarantined skill must not show up in the active registry")
	}
}

func TestManager_Install_AutoApprovesTrustedSourceWhenPolicyAllows(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://github.com/myorg/skills.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("blessed", "blessed", "body")},
		},
	}}
	mgr := NewManager(root, g).WithPolicy(quarantinePolicyAcceptingTrusted("github.com/myorg", false))

	in, err := mgr.Install(context.Background(), "https://github.com/myorg/skills.git", "", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if in.IsQuarantined() {
		t.Fatalf("expected active install, got quarantined (reason=%q)", in.QuarantineReason)
	}
	if _, err := os.Stat(filepath.Join(root, in.Dir, "SKILL.md")); err != nil {
		t.Errorf("active skill missing on disk: %v", err)
	}
}

func TestManager_Approve_MovesFromQuarantineToActive(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/x.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("pending", "pending", "body")},
		},
	}}
	mgr := NewManager(root, g).WithPolicy(DefaultPolicy())

	in, err := mgr.Install(context.Background(), "https://example.com/x.git", "", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !in.IsQuarantined() {
		t.Fatal("setup: expected quarantine")
	}

	approved, err := mgr.Approve(context.Background(), "pending")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.IsQuarantined() {
		t.Error("approved install must not still be quarantined")
	}
	if approved.QuarantineReason != "" {
		t.Errorf("approved install should clear quarantine_reason, got %q", approved.QuarantineReason)
	}

	// Disk: directory moved into active tree.
	if _, err := os.Stat(filepath.Join(root, in.Dir, "SKILL.md")); err != nil {
		t.Errorf("approved install missing in active root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, quarantineDirName, in.Dir)); !os.IsNotExist(err) {
		t.Error("quarantine entry must be gone after approve")
	}

	// LoadDir now picks up the skill — same path the watcher would take.
	reg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if reg.Get("pending") == nil {
		t.Error("approved skill should be visible to LoadDir")
	}
}

func TestManager_Approve_RejectsAlreadyActive(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://github.com/myorg/skills.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("blessed", "blessed", "body")},
		},
	}}
	mgr := NewManager(root, g).WithPolicy(quarantinePolicyAcceptingTrusted("github.com/myorg", false))
	if _, err := mgr.Install(context.Background(), "https://github.com/myorg/skills.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	_, err := mgr.Approve(context.Background(), "blessed")
	if err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("approve should fail on active install; got err=%v", err)
	}
}

func TestManager_Approve_UnknownSkill(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root, &fakeGit{repos: map[string]*fakeRepo{}}).WithPolicy(DefaultPolicy())
	if _, err := mgr.Approve(context.Background(), "ghost"); err == nil {
		t.Fatal("expected error approving non-installed skill")
	}
}

func TestManager_Reject_RemovesQuarantinedInstall(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://example.com/x.git": {
			commit: "abc",
			files: map[string]string{
				"alpha/SKILL.md": skillBytes("alpha", "alpha", "a"),
				"beta/SKILL.md":  skillBytes("beta", "beta", "b"),
			},
		},
	}}
	mgr := NewManager(root, g).WithPolicy(DefaultPolicy())
	in, err := mgr.Install(context.Background(), "https://example.com/x.git", "", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !in.IsQuarantined() {
		t.Fatal("setup: expected quarantine")
	}

	removed, err := mgr.Reject(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	// Both skills should be reported as removed — the install carries them
	// together and Reject is "delete the whole quarantined entry".
	if len(removed) != 2 {
		t.Errorf("expected 2 removed, got %v", removed)
	}
	if _, err := os.Stat(filepath.Join(root, quarantineDirName, in.Dir)); !os.IsNotExist(err) {
		t.Error("quarantine entry must be gone after reject")
	}

	// Lockfile no longer mentions the install.
	lf, _ := mgr.LoadLockfile()
	if len(lf.Installs) != 0 {
		t.Errorf("lockfile should be empty after reject, got %d installs", len(lf.Installs))
	}
}

func TestManager_Reject_RefusesActiveInstall(t *testing.T) {
	root := t.TempDir()
	g := &fakeGit{repos: map[string]*fakeRepo{
		"https://github.com/myorg/skills.git": {
			commit: "abc",
			files:  map[string]string{"SKILL.md": skillBytes("blessed", "blessed", "body")},
		},
	}}
	mgr := NewManager(root, g).WithPolicy(quarantinePolicyAcceptingTrusted("github.com/myorg", false))
	if _, err := mgr.Install(context.Background(), "https://github.com/myorg/skills.git", "", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	_, err := mgr.Reject(context.Background(), "blessed")
	if err == nil || !strings.Contains(err.Error(), "joe skills remove") {
		t.Fatalf("expected error pointing to `remove`; got %v", err)
	}
}

func TestManager_Update_QuarantinesWhenNewSkillsAppear(t *testing.T) {
	root := t.TempDir()
	repo := "https://github.com/myorg/skills.git"
	g := &fakeGit{repos: map[string]*fakeRepo{
		repo: {
			commit: "v1",
			files: map[string]string{
				"alpha/SKILL.md": skillBytes("alpha", "alpha", "a"),
			},
		},
	}}
	mgr := NewManager(root, g).WithPolicy(quarantinePolicyAcceptingTrusted("github.com/myorg", false))

	// Initial install — auto-approved (trusted source).
	in, err := mgr.Install(context.Background(), repo, "main", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if in.IsQuarantined() {
		t.Fatal("setup: expected auto-approve on initial install")
	}

	// Upstream adds a new skill — the trusted-source flag alone is not
	// enough; new skills require auto_approve.new_skills_in_existing_repos.
	g.repos[repo].commit = "v2"
	g.repos[repo].files["beta/SKILL.md"] = skillBytes("beta", "beta", "b")

	updated, err := mgr.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated) != 1 {
		t.Fatalf("expected 1 updated install, got %d", len(updated))
	}
	if !updated[0].IsQuarantined() {
		t.Fatal("update introducing a new skill must land in quarantine")
	}
	if updated[0].QuarantineReason == "" {
		t.Error("quarantined update must carry a reason")
	}

	// Disk: directory moved to .quarantine/.
	if _, err := os.Stat(filepath.Join(root, quarantineDirName, in.Dir, "alpha", "SKILL.md")); err != nil {
		t.Errorf("expected updated tree under quarantine: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, in.Dir)); !os.IsNotExist(err) {
		t.Error("active path should be gone after policy demotion")
	}
}

func TestManager_Update_StaysActiveWhenOnlyExistingSkillsChange(t *testing.T) {
	root := t.TempDir()
	repo := "https://github.com/myorg/skills.git"
	g := &fakeGit{repos: map[string]*fakeRepo{
		repo: {
			commit: "v1",
			files:  map[string]string{"SKILL.md": skillBytes("solo", "v1", "body v1")},
		},
	}}
	mgr := NewManager(root, g).WithPolicy(quarantinePolicyAcceptingTrusted("github.com/myorg", false))
	if _, err := mgr.Install(context.Background(), repo, "main", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	g.repos[repo].commit = "v2"
	g.repos[repo].files["SKILL.md"] = skillBytes("solo", "v2", "body v2")

	updated, err := mgr.Update(context.Background(), "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated[0].IsQuarantined() {
		t.Errorf("update with no new skills should stay active (reason=%q)", updated[0].QuarantineReason)
	}
}
