package safety

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/paths"
)

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()

	if p.Version != 1 {
		t.Errorf("Version = %d, want 1", p.Version)
	}

	// T2 defaults: all enabled
	if !p.Record.GraphMutations {
		t.Error("Record.GraphMutations should be true by default")
	}
	if !p.Record.ComponentRegistration {
		t.Error("Record.ComponentRegistration should be true by default")
	}
	if !p.Record.OnboardingFacts {
		t.Error("Record.OnboardingFacts should be true by default")
	}
	if !p.Record.AutonomousRefresh {
		t.Error("Record.AutonomousRefresh should be true by default")
	}

	// T3 defaults: all disabled except run_command (read-only commands only)
	if p.Act.WriteFile.Enabled {
		t.Error("Act.WriteFile should be disabled by default")
	}
	if len(p.Act.WriteFile.AllowedDirectories) != 0 {
		t.Errorf("Act.WriteFile.AllowedDirectories = %v, want empty", p.Act.WriteFile.AllowedDirectories)
	}

	if !p.Act.RunCommand.Enabled {
		t.Error("Act.RunCommand should be enabled by default (read-only commands)")
	}
	wantCmds := []string{"ls", "cat", "head", "tail", "grep", "find", "wc"}
	if len(p.Act.RunCommand.AllowedCommands) != len(wantCmds) {
		t.Errorf("Act.RunCommand.AllowedCommands = %v, want %v", p.Act.RunCommand.AllowedCommands, wantCmds)
	}

	if p.Act.K8sWrite.Enabled {
		t.Error("Act.K8sWrite should be disabled by default")
	}
	if p.Act.PagerdutyAck.Enabled {
		t.Error("Act.PagerdutyAck should be disabled by default")
	}
	if p.Act.AlertmanagerSilence.Enabled {
		t.Error("Act.AlertmanagerSilence should be disabled by default")
	}
	if p.Act.GitPush.Enabled {
		t.Error("Act.GitPush should be disabled by default")
	}
}

func TestLoadPolicy_MissingFile(t *testing.T) {
	dir := t.TempDir()

	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}

	// Should return default policy
	if p.Version != 1 {
		t.Errorf("Version = %d, want 1 (default)", p.Version)
	}
	if p.Act.WriteFile.Enabled {
		t.Error("missing file should produce default policy with WriteFile disabled")
	}
}

func TestLoadPolicy_ValidFile(t *testing.T) {
	dir := t.TempDir()
	policyYAML := `
version: 1

record:
  graph_mutations: true
  source_registration: false
  onboarding_facts: true
  autonomous_refresh: false

act:
  write_file:
    enabled: true
    allowed_directories:
      - /tmp/joe-workspace
      - /home/user/projects
  run_command:
    enabled: true
    allowed_commands:
      - ls
      - cat
      - kubectl
  k8s_write:
    enabled: false
  git_push:
    enabled: true
`
	err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte(policyYAML), 0644)
	if err != nil {
		t.Fatalf("write test policy: %v", err)
	}

	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}

	// Verify T2
	if !p.Record.GraphMutations {
		t.Error("Record.GraphMutations should be true")
	}
	if p.Record.ComponentRegistration {
		t.Error("Record.ComponentRegistration should be false")
	}
	if !p.Record.OnboardingFacts {
		t.Error("Record.OnboardingFacts should be true")
	}
	if p.Record.AutonomousRefresh {
		t.Error("Record.AutonomousRefresh should be false")
	}

	// Verify T3
	if !p.Act.WriteFile.Enabled {
		t.Error("Act.WriteFile.Enabled should be true")
	}
	if len(p.Act.WriteFile.AllowedDirectories) != 2 {
		t.Errorf("AllowedDirectories count = %d, want 2", len(p.Act.WriteFile.AllowedDirectories))
	}
	if p.Act.WriteFile.AllowedDirectories[0] != "/tmp/joe-workspace" {
		t.Errorf("AllowedDirectories[0] = %s, want /tmp/joe-workspace", p.Act.WriteFile.AllowedDirectories[0])
	}

	if len(p.Act.RunCommand.AllowedCommands) != 3 {
		t.Errorf("AllowedCommands count = %d, want 3", len(p.Act.RunCommand.AllowedCommands))
	}

	if p.Act.K8sWrite.Enabled {
		t.Error("Act.K8sWrite should be false")
	}
	if !p.Act.GitPush.Enabled {
		t.Error("Act.GitPush should be true")
	}
}

func TestLoadPolicy_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte("{{invalid yaml"), 0644)
	if err != nil {
		t.Fatalf("write test policy: %v", err)
	}

	_, err = LoadPolicy(dir)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestLoadPolicy_InvalidVersion(t *testing.T) {
	dir := t.TempDir()
	policyYAML := `version: 0
record:
  graph_mutations: true
`
	err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte(policyYAML), 0644)
	if err != nil {
		t.Fatalf("write test policy: %v", err)
	}

	_, err = LoadPolicy(dir)
	if err == nil {
		t.Fatal("expected error for version 0, got nil")
	}
}

func TestLoadPolicy_RejectsRelativeAllowedDirectories(t *testing.T) {
	dir := t.TempDir()
	policyYAML := `
version: 1
act:
  write_file:
    enabled: true
    allowed_directories:
      - ./relative-path
`
	err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte(policyYAML), 0644)
	if err != nil {
		t.Fatalf("write test policy: %v", err)
	}

	_, err = LoadPolicy(dir)
	if err == nil {
		t.Fatal("expected error for relative allowed_directories, got nil")
	}
}

func TestLoadPolicy_PartialFile(t *testing.T) {
	dir := t.TempDir()
	// Only specify version and one field — rest should get defaults
	policyYAML := `
version: 1
act:
  write_file:
    enabled: true
    allowed_directories:
      - /tmp
`
	err := os.WriteFile(filepath.Join(dir, PolicyFileName), []byte(policyYAML), 0644)
	if err != nil {
		t.Fatalf("write test policy: %v", err)
	}

	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}

	// Explicitly set field
	if !p.Act.WriteFile.Enabled {
		t.Error("Act.WriteFile.Enabled should be true (explicitly set)")
	}

	// Fields not in the YAML should have defaults
	if !p.Record.GraphMutations {
		t.Error("Record.GraphMutations should be true (default)")
	}
	if !p.Act.RunCommand.Enabled {
		t.Error("Act.RunCommand.Enabled should be true (default)")
	}
	if len(p.Act.RunCommand.AllowedCommands) != 7 {
		t.Errorf("AllowedCommands = %v, want 7 default commands", p.Act.RunCommand.AllowedCommands)
	}
}

func TestIsT2Allowed(t *testing.T) {
	p := DefaultPolicy()
	p.Record.ComponentRegistration = false

	tests := []struct {
		category string
		want     bool
	}{
		{"graph_mutations", true},
		{"source_registration", false},
		{"onboarding_facts", true},
		{"autonomous_refresh", true},
		{"unknown_category", false},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := p.IsT2Allowed(tt.category)
			if got != tt.want {
				t.Errorf("IsT2Allowed(%q) = %v, want %v", tt.category, got, tt.want)
			}
		})
	}
}

func TestIsT3Allowed(t *testing.T) {
	p := DefaultPolicy()
	p.Act.GitPush.Enabled = true

	tests := []struct {
		action string
		want   bool
	}{
		{"write_file", false},
		{"run_command", true},
		{"k8s_write", false},
		{"pagerduty_ack", false},
		{"alertmanager_silence", false},
		{"git_push", true},
		{"unknown_action", false},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			got := p.IsT3Allowed(tt.action)
			if got != tt.want {
				t.Errorf("IsT3Allowed(%q) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}

func TestLoadPolicy_TildeExpansion(t *testing.T) {
	home, err := paths.SecureHomeDir()
	if err != nil {
		t.Fatalf("SecureHomeDir: %v", err)
	}

	dir := t.TempDir()
	policyYAML := `
version: 1
act:
  write_file:
    enabled: true
    allowed_directories:
      - ~/projects
      - /tmp/joe-output
      - ~/Documents/work
`
	err = os.WriteFile(filepath.Join(dir, PolicyFileName), []byte(policyYAML), 0644)
	if err != nil {
		t.Fatalf("write test policy: %v", err)
	}

	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy error: %v", err)
	}

	wantDirs := []string{
		filepath.Join(home, "projects"),
		"/tmp/joe-output",
		filepath.Join(home, "Documents/work"),
	}

	if len(p.Act.WriteFile.AllowedDirectories) != len(wantDirs) {
		t.Fatalf("AllowedDirectories = %v, want %v", p.Act.WriteFile.AllowedDirectories, wantDirs)
	}

	for i, got := range p.Act.WriteFile.AllowedDirectories {
		if got != wantDirs[i] {
			t.Errorf("AllowedDirectories[%d] = %q, want %q", i, got, wantDirs[i])
		}
	}

	// Verify no tilde remains
	for i, d := range p.Act.WriteFile.AllowedDirectories {
		if strings.HasPrefix(d, "~") {
			t.Errorf("AllowedDirectories[%d] = %q still has tilde prefix", i, d)
		}
	}
}
