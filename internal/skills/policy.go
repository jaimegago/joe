package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PolicyFileName is the name of the skills policy file within the Joe config
// directory. It is parallel to safety.PolicyFileName.
//
// Like the safety policy, this file is a *protected config*: Joe's tools cannot
// read or write it (enforced by safety.IsPathAllowed). Humans edit it by hand;
// the LLM cannot influence its contents at runtime.
const PolicyFileName = "skills-policy.yaml"

// Policy controls which skill components Joe will install without explicit
// approval and which lifecycle events land in quarantine. It is parallel in
// spirit to safety.SafetyPolicy but governs skill ingestion, not tool
// execution.
//
// Loaded once at startup from ~/.joe/skills-policy.yaml. A missing file falls
// back to DefaultPolicy, which is the most conservative configuration —
// nothing auto-approves, every install lands in quarantine until a human
// explicitly approves it.
type Policy struct {
	Version        int               `yaml:"version"`
	TrustedSources []string          `yaml:"trusted_sources"`
	AutoApprove    AutoApprovePolicy `yaml:"auto_approve"`
}

// AutoApprovePolicy describes which install/update flows skip quarantine.
// The defaults are deliberately strict: every change is reviewed unless the
// operator opts in.
type AutoApprovePolicy struct {
	// TrustedSources, when true, auto-approves installs and updates whose
	// repo URL matches an entry in Policy.TrustedSources. Off by default so
	// even installs from listed components land in quarantine until an operator
	// flips the switch.
	TrustedSources bool `yaml:"trusted_sources"`
	// NewSkillsInExistingRepos, when true, auto-approves updates that
	// introduce a brand-new skill into a previously-installed (trusted)
	// repo. Off by default because "this repo adds a new skill" is a
	// distinct trust decision from "this repo updates an existing skill" —
	// see docs/joe-skills-design.md "Three trust layers".
	NewSkillsInExistingRepos bool `yaml:"new_skills_in_existing_repos"`
}

// DefaultPolicy returns the safest possible configuration: no trusted
// components, no auto-approve. Every install and update is quarantined; an
// operator must run `joe skills approve` (or POST /api/v1/skills/approve)
// before a skill becomes active.
//
// This matches the safety framework's stance: deny by default, opt in to
// looser behavior.
func DefaultPolicy() *Policy {
	return &Policy{
		Version: 1,
		AutoApprove: AutoApprovePolicy{
			TrustedSources:           false,
			NewSkillsInExistingRepos: false,
		},
	}
}

// LoadPolicy reads ~/.joe/skills-policy.yaml. If the file does not exist, it
// returns DefaultPolicy() with no error — a fresh install has no policy and
// should fall through to the conservative defaults. A malformed file is a
// hard error: silently dropping policy would let a corrupted file flip the
// system into "trust everything" mode, which is the exact opposite of safe.
func LoadPolicy(configDir string) (*Policy, error) {
	path := filepath.Join(configDir, PolicyFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultPolicy(), nil
		}
		return nil, fmt.Errorf("read skills policy %s: %w", path, err)
	}

	policy := DefaultPolicy()
	if err := yaml.Unmarshal(data, policy); err != nil {
		return nil, fmt.Errorf("parse skills policy %s: %w", path, err)
	}
	if policy.Version < 1 {
		return nil, fmt.Errorf("skills policy %s: version must be >= 1, got %d", path, policy.Version)
	}
	return policy, nil
}

// ApprovalDecision is the outcome of evaluating a candidate install against
// the policy. Either the install can land in the active tree, or it must
// wait in quarantine for explicit human approval.
type ApprovalDecision struct {
	// AutoApprove is true when the install can skip quarantine.
	AutoApprove bool
	// Reason names which policy rule produced this decision. Used for the
	// audit log so an operator can reconstruct *why* something was
	// quarantined or auto-approved.
	Reason string
}

// EvaluateInstall decides whether a fresh install (one with no prior version
// on disk) can skip quarantine. Returns AutoApprove=false unless the repo
// matches a trusted source AND auto_approve.trusted_sources is enabled.
func (p *Policy) EvaluateInstall(repo string) ApprovalDecision {
	if p == nil {
		return ApprovalDecision{AutoApprove: false, Reason: "no policy configured"}
	}
	if !p.matchesTrustedSource(repo) {
		return ApprovalDecision{AutoApprove: false, Reason: "repo is not in trusted_sources"}
	}
	if !p.AutoApprove.TrustedSources {
		return ApprovalDecision{AutoApprove: false, Reason: "auto_approve.trusted_sources is disabled"}
	}
	return ApprovalDecision{AutoApprove: true, Reason: "trusted source + auto_approve.trusted_sources"}
}

// EvaluateUpdate decides whether an in-place update (one with the same repo
// already installed) can skip quarantine. The presence of `newSkills`
// (names not in the previous SkillRecord list) routes the update to
// quarantine unless auto_approve.new_skills_in_existing_repos is set.
func (p *Policy) EvaluateUpdate(repo string, newSkills []string) ApprovalDecision {
	if p == nil {
		return ApprovalDecision{AutoApprove: false, Reason: "no policy configured"}
	}
	if !p.matchesTrustedSource(repo) {
		return ApprovalDecision{AutoApprove: false, Reason: "repo is not in trusted_sources"}
	}
	if !p.AutoApprove.TrustedSources {
		return ApprovalDecision{AutoApprove: false, Reason: "auto_approve.trusted_sources is disabled"}
	}
	if len(newSkills) > 0 && !p.AutoApprove.NewSkillsInExistingRepos {
		return ApprovalDecision{AutoApprove: false, Reason: "update introduces new skills; auto_approve.new_skills_in_existing_repos is disabled"}
	}
	return ApprovalDecision{AutoApprove: true, Reason: "trusted source + auto_approve.trusted_sources"}
}

// matchesTrustedSource reuses the same normalization the installer uses for
// its allowlist check, so policy entries written as "github.com/myorg" match
// "https://github.com/myorg/anything.git" the same way the install gate does.
func (p *Policy) matchesTrustedSource(repo string) bool {
	if p == nil || len(p.TrustedSources) == 0 {
		return false
	}
	repoKey := normalizeRepoForTrust(repo)
	if repoKey == "" {
		return false
	}
	for _, src := range p.TrustedSources {
		srcKey := normalizeRepoForTrust(src)
		if srcKey == "" {
			continue
		}
		if repoKey == srcKey || hasTrustedPrefix(repoKey, srcKey) {
			return true
		}
	}
	return false
}

// hasTrustedPrefix reports whether repoKey extends srcKey along a `/`
// boundary — the same shape the Manager.checkTrusted comparison uses.
func hasTrustedPrefix(repoKey, srcKey string) bool {
	if len(repoKey) <= len(srcKey)+1 {
		return false
	}
	if repoKey[len(srcKey)] != '/' {
		return false
	}
	return repoKey[:len(srcKey)] == srcKey
}

// ErrNoPolicy is returned by callers that require a policy but were given a
// nil one. The Manager treats nil as "use defaults"; this error exists for
// API/CLI layers that need to surface the missing policy to the user.
var ErrNoPolicy = errors.New("skills policy is not configured")
