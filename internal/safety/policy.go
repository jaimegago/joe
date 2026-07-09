package safety

// SafetyPolicy defines what Joe is allowed to do. It is constructed at runtime
// from DefaultPolicy() and modulated per request by the task's safety_tier
// (see internal/api resolveSafetyPolicy). There is no on-disk policy file and no
// runtime decode of this struct: the former ~/.joe/safety-policy.yaml loader was
// never wired in production and was removed. The yaml tags are retained only so
// the struct still round-trips if a file-backed source is ever reintroduced
// deliberately.
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
	K8sWrite            ActionToggle `yaml:"k8s_write"`
	PagerdutyAck        ActionToggle `yaml:"pagerduty_ack"`
	AlertmanagerSilence ActionToggle `yaml:"alertmanager_silence"`
	GitPush             ActionToggle `yaml:"git_push"`
}

// ActionToggle is a simple enabled/disabled flag for a T3 action.
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
			K8sWrite:            ActionToggle{Enabled: false},
			PagerdutyAck:        ActionToggle{Enabled: false},
			AlertmanagerSilence: ActionToggle{Enabled: false},
			GitPush:             ActionToggle{Enabled: false},
		},
	}
}

// IsT3Allowed checks whether a T3 (Act) action is permitted.
func (p *SafetyPolicy) IsT3Allowed(action string) bool {
	switch action {
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
