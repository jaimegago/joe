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

	// Act defaults: all mutating actions disabled
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
