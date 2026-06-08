package safety

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaimegago/joe/internal/paths"
	"gopkg.in/yaml.v3"
)

// PolicyFileName is the name of the safety policy file within the Joe config directory.
const PolicyFileName = "safety-policy.yaml"

// SafetyPolicy defines what Joe is allowed to do. It is loaded once at startup
// from ~/.joe/safety-policy.yaml. Joe's own tools cannot read or modify this file.
type SafetyPolicy struct {
	Version int          `yaml:"version"`
	Record  RecordPolicy `yaml:"record"`
	Act     ActPolicy    `yaml:"act"`
}

// RecordPolicy controls T2 (internal state mutation) permissions.
type RecordPolicy struct {
	GraphMutations        bool `yaml:"graph_mutations"`
	ComponentRegistration bool `yaml:"source_registration"`
	OnboardingFacts       bool `yaml:"onboarding_facts"`
	AutonomousRefresh     bool `yaml:"autonomous_refresh"`
}

// ActPolicy controls T3 (external system mutation) permissions.
// Each action type has its own configuration block.
type ActPolicy struct {
	WriteFile           WriteFilePolicy  `yaml:"write_file"`
	RunCommand          RunCommandPolicy `yaml:"run_command"`
	K8sWrite            ActionToggle     `yaml:"k8s_write"`
	PagerdutyAck        ActionToggle     `yaml:"pagerduty_ack"`
	AlertmanagerSilence ActionToggle     `yaml:"alertmanager_silence"`
	GitPush             ActionToggle     `yaml:"git_push"`
}

// WriteFilePolicy controls the write_file tool.
type WriteFilePolicy struct {
	Enabled            bool     `yaml:"enabled"`
	AllowedDirectories []string `yaml:"allowed_directories"`
}

// RunCommandPolicy controls the run_command tool.
type RunCommandPolicy struct {
	Enabled         bool     `yaml:"enabled"`
	AllowedCommands []string `yaml:"allowed_commands"`
}

// ActionToggle is a simple enabled/disabled flag for future T3 actions.
type ActionToggle struct {
	Enabled bool `yaml:"enabled"`
}

// DefaultPolicy returns the most restrictive policy: all T2 enabled (internal
// state is safe by default), all T3 disabled. This is used when no policy file
// exists.
func DefaultPolicy() *SafetyPolicy {
	return &SafetyPolicy{
		Version: 1,
		Record: RecordPolicy{
			GraphMutations:        true,
			ComponentRegistration: true,
			OnboardingFacts:       true,
			AutonomousRefresh:     true,
		},
		Act: ActPolicy{
			WriteFile: WriteFilePolicy{
				Enabled:            false,
				AllowedDirectories: nil,
			},
			RunCommand: RunCommandPolicy{
				Enabled: true,
				AllowedCommands: []string{
					"ls", "cat", "head", "tail", "grep", "find", "wc",
				},
			},
			K8sWrite:            ActionToggle{Enabled: false},
			PagerdutyAck:        ActionToggle{Enabled: false},
			AlertmanagerSilence: ActionToggle{Enabled: false},
			GitPush:             ActionToggle{Enabled: false},
		},
	}
}

// LoadPolicy reads the safety policy from the given directory. If the file does
// not exist, it returns DefaultPolicy(). If the file exists but is malformed,
// it returns an error — we refuse to run with an unparseable policy.
func LoadPolicy(configDir string) (*SafetyPolicy, error) {
	path := filepath.Join(configDir, PolicyFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultPolicy(), nil
		}
		return nil, fmt.Errorf("read safety policy %s: %w", path, err)
	}

	policy := DefaultPolicy() // start from defaults so missing fields get safe values
	if err := yaml.Unmarshal(data, policy); err != nil {
		return nil, fmt.Errorf("parse safety policy %s: %w", path, err)
	}

	if policy.Version < 1 {
		return nil, fmt.Errorf("safety policy %s: version must be >= 1, got %d", path, policy.Version)
	}

	// Expand ~ in allowed_directories to the user's home directory.
	// Uses secure home resolution that bypasses HOME env var.
	if err := expandTildePaths(policy.Act.WriteFile.AllowedDirectories); err != nil {
		return nil, fmt.Errorf("safety policy %s: expand allowed_directories: %w", path, err)
	}

	// Normalize allowed_directories to absolute, cleaned paths and reject relative entries.
	if err := normalizeAllowedDirectories(policy.Act.WriteFile.AllowedDirectories); err != nil {
		return nil, fmt.Errorf("safety policy %s: normalize allowed_directories: %w", path, err)
	}

	return policy, nil
}

// IsT2Allowed checks whether a T2 (Record) action category is permitted.
func (p *SafetyPolicy) IsT2Allowed(category string) bool {
	switch category {
	case "graph_mutations":
		return p.Record.GraphMutations
	case "source_registration":
		return p.Record.ComponentRegistration
	case "onboarding_facts":
		return p.Record.OnboardingFacts
	case "autonomous_refresh":
		return p.Record.AutonomousRefresh
	default:
		return false // unknown category → deny
	}
}

// IsT3Allowed checks whether a T3 (Act) action is permitted.
func (p *SafetyPolicy) IsT3Allowed(action string) bool {
	switch action {
	case "write_file":
		return p.Act.WriteFile.Enabled
	case "run_command":
		return p.Act.RunCommand.Enabled
	case "k8s_write":
		return p.Act.K8sWrite.Enabled
	case "pagerduty_ack":
		return p.Act.PagerdutyAck.Enabled
	case "alertmanager_silence":
		return p.Act.AlertmanagerSilence.Enabled
	case "git_push":
		return p.Act.GitPush.Enabled
	default:
		return false // unknown action → deny
	}
}

// expandTildePaths expands ~ prefix in path slices to the user's home directory.
// Modifies the slice in place. Uses secure home resolution (bypasses HOME env).
func expandTildePaths(paths_ []string) error {
	var home string
	for i, p := range paths_ {
		if strings.HasPrefix(p, "~/") || p == "~" {
			if home == "" {
				var err error
				home, err = paths.SecureHomeDir()
				if err != nil {
					return fmt.Errorf("cannot resolve ~ in path %q: %w", p, err)
				}
			}
			if p == "~" {
				paths_[i] = home
			} else {
				paths_[i] = filepath.Join(home, p[2:])
			}
		}
	}
	return nil
}

// normalizeAllowedDirectories converts allowed_directories entries to absolute,
// cleaned paths and rejects any that are still relative after expansion.
func normalizeAllowedDirectories(paths_ []string) error {
	for i, p := range paths_ {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			return fmt.Errorf("allowed_directories must be absolute: %q", p)
		}
		absPath, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("failed to resolve allowed_directories path %q: %w", p, err)
		}
		paths_[i] = filepath.Clean(absPath)
	}
	return nil
}
