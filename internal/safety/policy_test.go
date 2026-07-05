package safety

import (
	"testing"
)

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()

	if p.Version != 1 {
		t.Errorf("Version = %d, want 1", p.Version)
	}

	// Record defaults: all enabled
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

	// Act defaults: all disabled except run_command (read-only commands only)
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
