package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPolicy_DenyByDefault(t *testing.T) {
	p := DefaultPolicy()
	if p.AutoApprove.TrustedSources {
		t.Error("DefaultPolicy must not auto-approve trusted sources — that is the operator opt-in")
	}
	if p.AutoApprove.NewSkillsInExistingRepos {
		t.Error("DefaultPolicy must not auto-approve new skills in existing repos")
	}
	if len(p.TrustedSources) != 0 {
		t.Errorf("DefaultPolicy must have no trusted sources, got %d", len(p.TrustedSources))
	}
}

func TestLoadPolicy_MissingFile(t *testing.T) {
	dir := t.TempDir()
	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy on missing file: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil DefaultPolicy on missing file")
	}
	if p.AutoApprove.TrustedSources {
		t.Error("missing file must fall back to deny-by-default")
	}
}

func TestLoadPolicy_ValidFile(t *testing.T) {
	dir := t.TempDir()
	content := `version: 1
trusted_sources:
  - github.com/myorg
auto_approve:
  trusted_sources: true
  new_skills_in_existing_repos: false
`
	if err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if !p.AutoApprove.TrustedSources {
		t.Error("policy should reflect auto_approve.trusted_sources=true")
	}
	if p.AutoApprove.NewSkillsInExistingRepos {
		t.Error("policy should reflect auto_approve.new_skills_in_existing_repos=false")
	}
	if len(p.TrustedSources) != 1 || p.TrustedSources[0] != "github.com/myorg" {
		t.Errorf("trusted_sources = %v", p.TrustedSources)
	}
}

func TestLoadPolicy_MalformedFileIsFatal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte("not: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(dir); err == nil {
		t.Fatal("expected error on malformed skills-policy.yaml")
	}
}

func TestLoadPolicy_VersionMustBeAtLeastOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte("version: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(dir); err == nil {
		t.Fatal("expected error when version < 1")
	}
}

func TestPolicy_EvaluateInstall(t *testing.T) {
	cases := []struct {
		name        string
		policy      *Policy
		repo        string
		wantApprove bool
	}{
		{
			name: "nil policy denies",
			repo: "https://github.com/anyone/x.git",
		},
		{
			name: "default policy denies",
			policy: &Policy{
				Version: 1,
			},
			repo: "https://github.com/myorg/x.git",
		},
		{
			name: "trusted host with auto_approve",
			policy: &Policy{
				Version:        1,
				TrustedSources: []string{"github.com/myorg"},
				AutoApprove:    AutoApprovePolicy{TrustedSources: true},
			},
			repo:        "https://github.com/myorg/some-skills.git",
			wantApprove: true,
		},
		{
			name: "trusted host without auto_approve still denies",
			policy: &Policy{
				Version:        1,
				TrustedSources: []string{"github.com/myorg"},
			},
			repo: "https://github.com/myorg/some-skills.git",
		},
		{
			name: "non-matching repo denies even with auto_approve",
			policy: &Policy{
				Version:        1,
				TrustedSources: []string{"github.com/myorg"},
				AutoApprove:    AutoApprovePolicy{TrustedSources: true},
			},
			repo: "https://github.com/evil/x.git",
		},
		{
			name: "host-only trusted source matches any owner",
			policy: &Policy{
				Version:        1,
				TrustedSources: []string{"github.com"},
				AutoApprove:    AutoApprovePolicy{TrustedSources: true},
			},
			repo:        "https://github.com/anyone/x.git",
			wantApprove: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.policy.EvaluateInstall(tc.repo)
			if d.AutoApprove != tc.wantApprove {
				t.Errorf("AutoApprove = %v, want %v (reason=%q)", d.AutoApprove, tc.wantApprove, d.Reason)
			}
			if !d.AutoApprove && d.Reason == "" {
				t.Error("denials must carry a reason for the audit log")
			}
		})
	}
}

func TestPolicy_EvaluateUpdate_NewSkillsGuard(t *testing.T) {
	p := &Policy{
		Version:        1,
		TrustedSources: []string{"github.com/myorg"},
		AutoApprove:    AutoApprovePolicy{TrustedSources: true, NewSkillsInExistingRepos: false},
	}

	// No new skills — update auto-approves.
	if d := p.EvaluateUpdate("https://github.com/myorg/x.git", nil); !d.AutoApprove {
		t.Errorf("expected auto-approve when no new skills; reason=%q", d.Reason)
	}
	// New skills + flag off — quarantine.
	if d := p.EvaluateUpdate("https://github.com/myorg/x.git", []string{"surprise"}); d.AutoApprove {
		t.Error("new skill in trusted repo must require approval when flag off")
	}
	// Flip the flag — auto-approve resumes.
	p.AutoApprove.NewSkillsInExistingRepos = true
	if d := p.EvaluateUpdate("https://github.com/myorg/x.git", []string{"surprise"}); !d.AutoApprove {
		t.Errorf("expected auto-approve with new_skills flag on; reason=%q", d.Reason)
	}
}
