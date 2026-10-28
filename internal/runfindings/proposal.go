package runfindings

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ProposalShape is where the declared state the proposal changes lives.
type ProposalShape string

const (
	// ShapeRepositoryChange — a unified diff against a pinned commit of a
	// registered git component, file paths named. The shape
	// remediation-execution executes as a branch push and a pull request.
	ShapeRepositoryChange ProposalShape = "repository_change"
	// ShapeLiveObjectChange — the target by its own identifier (for
	// Kubernetes: group/version/resource, namespace, name), the base by the
	// observed generation, the change as a patch. Executable only by an apply
	// operation AUTOJOE-RW would own; nothing in joe applies one today.
	ShapeLiveObjectChange ProposalShape = "live_object_change"
	// ShapeOperatorAction — steps for a human, where joe cannot execute. Every
	// graph update's proposal is this shape. Not executable.
	ShapeOperatorAction ProposalShape = "operator_action"
)

// ParseProposalShape maps a declared value to the closed set.
func ParseProposalShape(s string) (ProposalShape, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.ReplaceAll(v, " ", "_")
	v = strings.ReplaceAll(v, "-", "_")
	switch ProposalShape(v) {
	case ShapeRepositoryChange, ShapeLiveObjectChange, ShapeOperatorAction:
		return ProposalShape(v), true
	}
	return "", false
}

// ProposalStatusUnverified is the label every proposal carries (D-0140): a
// proposal is labelled, unverified session material, never a verified change.
const ProposalStatusUnverified = "unverified"

// ProposalTarget names what the proposal changes: the graph node and the
// system's own identifier for it, plus the component the node belongs to,
// resolved by code so the in-scope invariant can be checked.
type ProposalTarget struct {
	NodeID      string `json:"node_id"`
	ComponentID string `json:"component_id"`
	SystemID    string `json:"system_id"`
}

// LiveObjectRef identifies a Kubernetes object by group/version/resource,
// namespace and name. Group is empty for the core group.
type LiveObjectRef struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// Proposal is the change that would resolve one finding, stated against a
// pinned base, and never executed here. The five common fields — target, base,
// change, expected effect, limits — are required on every shape; Validate
// rejects a proposal missing any of them.
type Proposal struct {
	Shape  ProposalShape  `json:"shape"`
	Target ProposalTarget `json:"target"`
	// Base is the observed state the proposal was computed from: a commit for a
	// repository change, a generation for a live-object change, the observed
	// state in words for an operator action. A proposal with no base is not
	// emitted — an executor cannot tell whether it is stale.
	Base string `json:"base"`
	// Change is the unified diff, the patch, or the steps.
	Change string `json:"change"`
	// ExpectedEffect is how the reference would show the finding resolved —
	// "the Available condition becomes True".
	ExpectedEffect string `json:"expected_effect"`
	// Limits is what was not checked: that an image tag exists, that anything
	// was tested.
	Limits string `json:"limits"`
	// Status is always ProposalStatusUnverified.
	Status string `json:"status"`
	// Executable is a property of the shape, not of what joe can do today: a
	// repository change and a live-object change have an executor shape in the
	// design; an operator action is marked not executable.
	Executable bool `json:"executable"`

	// Paths are the file paths a repository change names. Required for that
	// shape.
	Paths []string `json:"paths,omitempty"`
	// LiveObject is the target of a live-object change. Required for that
	// shape.
	LiveObject *LiveObjectRef `json:"live_object,omitempty"`
}

// Proposal validation errors. Each is a sentinel so a caller can tell why a
// proposal was not emitted without matching message text.
var (
	ErrProposalShape      = errors.New("proposal: unknown shape")
	ErrProposalNoTarget   = errors.New("proposal: target requires node_id and system_id")
	ErrProposalNoBase     = errors.New("proposal: no base")
	ErrProposalNoChange   = errors.New("proposal: no change")
	ErrProposalNoEffect   = errors.New("proposal: no expected effect")
	ErrProposalNoLimits   = errors.New("proposal: no limits")
	ErrProposalOutOfScope = errors.New("proposal: target outside the run's resolved component set")
	ErrProposalSecret     = errors.New("proposal: carries what looks like a secret value")
	ErrProposalNoPaths    = errors.New("proposal: repository change names no file paths")
	ErrProposalNoObject   = errors.New("proposal: live-object change names no object")
)

// Validate enforces the proposal contract. scope is the run's resolved
// component set; a proposal whose target lies outside it is not emitted, so a
// run never widens its aim through what it proposes.
func (p *Proposal) Validate(scope Set) error {
	if p == nil {
		return errors.New("proposal: nil")
	}
	if _, ok := ParseProposalShape(string(p.Shape)); !ok {
		return fmt.Errorf("%w: %q", ErrProposalShape, p.Shape)
	}
	if strings.TrimSpace(p.Target.NodeID) == "" || strings.TrimSpace(p.Target.SystemID) == "" {
		return ErrProposalNoTarget
	}
	if strings.TrimSpace(p.Base) == "" {
		return ErrProposalNoBase
	}
	if strings.TrimSpace(p.Change) == "" {
		return ErrProposalNoChange
	}
	if strings.TrimSpace(p.ExpectedEffect) == "" {
		return ErrProposalNoEffect
	}
	if strings.TrimSpace(p.Limits) == "" {
		return ErrProposalNoLimits
	}
	switch p.Shape {
	case ShapeRepositoryChange:
		if len(p.Paths) == 0 {
			return ErrProposalNoPaths
		}
	case ShapeLiveObjectChange:
		if p.LiveObject == nil || strings.TrimSpace(p.LiveObject.Resource) == "" ||
			strings.TrimSpace(p.LiveObject.Name) == "" || strings.TrimSpace(p.LiveObject.Version) == "" {
			return ErrProposalNoObject
		}
	}
	if scope == nil || !scope(p.Target.ComponentID) {
		return ErrProposalOutOfScope
	}
	if LooksLikeSecret(p.Change) || LooksLikeSecret(p.Base) {
		return ErrProposalSecret
	}
	return nil
}

// Normalise fills the derived fields — Status and Executable — from the shape.
func (p *Proposal) Normalise() {
	p.Status = ProposalStatusUnverified
	p.Executable = p.Shape != ShapeOperatorAction
}

// The secret guard. A proposal never carries a secret value; it names a secret
// by reference. This is a guard, not a proof: it refuses the shapes a secret
// value takes in the manifests and patches a proposal carries, and a value that
// looks like none of them passes. The patterns are deliberately conservative
// in the other direction too — a key named with no value, a `$VAR` or
// `<placeholder>` reference, and a secretKeyRef name all pass, because naming a
// secret by reference is exactly what a proposal is supposed to do.
var (
	// A Kubernetes Secret manifest with a data or stringData section is a
	// secret value by construction.
	secretManifestKind = regexp.MustCompile(`(?im)^\s*kind:\s*["']?Secret["']?\s*$`)
	secretManifestData = regexp.MustCompile(`(?im)^\s*(data|stringData):\s*$`)
	// A key that names a credential — with any prefix, so DB_PASSWORD and
	// prod_api_key match — assigned a literal value. The value's first character
	// excludes the reference and placeholder forms; the capture is checked by
	// looksLikeSecretValue, which asks for credential shape (a digit, or sixteen
	// or more characters) so a bare secret NAME such as "db-credentials" passes.
	secretAssignment = regexp.MustCompile(
		`(?i)(?:^|[^a-z0-9])[a-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|apikey|private[_-]?key|client[_-]?secret|access[_-]?key)["']?\s*[:=]\s*["']?([^\s"'<>$\{\(\[][^\s"']{7,})`)
	// PEM-armoured private key material.
	pemPrivateKey = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
)

// LooksLikeSecret reports whether s carries what looks like a secret value.
func LooksLikeSecret(s string) bool {
	if s == "" {
		return false
	}
	if secretManifestKind.MatchString(s) && secretManifestData.MatchString(s) {
		return true
	}
	if pemPrivateKey.MatchString(s) {
		return true
	}
	for _, m := range secretAssignment.FindAllStringSubmatch(s, -1) {
		if looksLikeSecretValue(m[1]) {
			return true
		}
	}
	return false
}

// looksLikeSecretValue is the shape test on a captured value: credential
// material carries a digit or is long; a short, digit-free word after
// "secret:" is a name, which a proposal is allowed and expected to use.
func looksLikeSecretValue(v string) bool {
	if len(v) >= 16 {
		return true
	}
	for _, r := range v {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
