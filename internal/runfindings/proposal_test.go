package runfindings

import (
	"errors"
	"testing"
)

func validOperatorAction() Proposal {
	p := Proposal{
		Shape:          ShapeOperatorAction,
		Target:         ProposalTarget{NodeID: "svc-a", ComponentID: "k8s", SystemID: "default/svc-a"},
		Base:           "observed replicas=1, generation 7",
		Change:         "scale the deployment to 2 replicas",
		ExpectedEffect: "the finding 'single replica' is resolved; the deployment reports 2 available replicas",
		Limits:         "did not check node capacity",
	}
	p.Normalise()
	return p
}

// TestProposal_RejectsMissingAnyOfTheFiveCommonFields pins the contract: a
// proposal missing target, base, change, expected effect or limits is not
// emitted.
func TestProposal_RejectsMissingAnyOfTheFiveCommonFields(t *testing.T) {
	scope := SetOf("k8s")
	tests := []struct {
		name string
		mut  func(*Proposal)
		want error
	}{
		{"no target node", func(p *Proposal) { p.Target.NodeID = "" }, ErrProposalNoTarget},
		{"no target system id", func(p *Proposal) { p.Target.SystemID = "" }, ErrProposalNoTarget},
		{"no base", func(p *Proposal) { p.Base = "  " }, ErrProposalNoBase},
		{"no change", func(p *Proposal) { p.Change = "" }, ErrProposalNoChange},
		{"no expected effect", func(p *Proposal) { p.ExpectedEffect = "" }, ErrProposalNoEffect},
		{"no limits", func(p *Proposal) { p.Limits = "" }, ErrProposalNoLimits},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validOperatorAction()
			tt.mut(&p)
			err := p.Validate(scope)
			if !errors.Is(err, tt.want) {
				t.Errorf("Validate = %v, want %v", err, tt.want)
			}
		})
	}
	p := validOperatorAction()
	if err := p.Validate(scope); err != nil {
		t.Errorf("complete proposal rejected: %v", err)
	}
}

// TestProposal_TargetOutsideScopeIsNotEmitted pins the in-scope invariant: a
// run never widens its aim through what it proposes.
func TestProposal_TargetOutsideScopeIsNotEmitted(t *testing.T) {
	p := validOperatorAction()
	if err := p.Validate(SetOf("other-component")); !errors.Is(err, ErrProposalOutOfScope) {
		t.Errorf("Validate = %v, want %v", err, ErrProposalOutOfScope)
	}
	// A target whose component could not be resolved is outside every scope.
	p.Target.ComponentID = ""
	if err := p.Validate(SetOf("k8s")); !errors.Is(err, ErrProposalOutOfScope) {
		t.Errorf("unresolved target: Validate = %v, want %v", err, ErrProposalOutOfScope)
	}
	if err := p.Validate(nil); !errors.Is(err, ErrProposalOutOfScope) {
		t.Errorf("nil scope: Validate = %v, want %v", err, ErrProposalOutOfScope)
	}
}

func TestProposal_ShapeSpecificFields(t *testing.T) {
	scope := SetOf("k8s")
	repo := validOperatorAction()
	repo.Shape = ShapeRepositoryChange
	repo.Normalise()
	if err := repo.Validate(scope); !errors.Is(err, ErrProposalNoPaths) {
		t.Errorf("repository change without paths: %v, want %v", err, ErrProposalNoPaths)
	}
	repo.Paths = []string{"deploy/svc-a.yaml"}
	if err := repo.Validate(scope); err != nil {
		t.Errorf("repository change with paths rejected: %v", err)
	}
	if !repo.Executable {
		t.Error("repository change should be executable by shape")
	}

	live := validOperatorAction()
	live.Shape = ShapeLiveObjectChange
	live.Normalise()
	if err := live.Validate(scope); !errors.Is(err, ErrProposalNoObject) {
		t.Errorf("live-object change without object: %v, want %v", err, ErrProposalNoObject)
	}
	live.LiveObject = &LiveObjectRef{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "default", Name: "svc-a"}
	if err := live.Validate(scope); err != nil {
		t.Errorf("live-object change with object rejected: %v", err)
	}

	op := validOperatorAction()
	if op.Executable {
		t.Error("operator action must be marked not executable")
	}
	if op.Status != ProposalStatusUnverified {
		t.Errorf("status = %q, want %q", op.Status, ProposalStatusUnverified)
	}

	bad := validOperatorAction()
	bad.Shape = "hotfix"
	if err := bad.Validate(scope); !errors.Is(err, ErrProposalShape) {
		t.Errorf("unknown shape: %v, want %v", err, ErrProposalShape)
	}
}

func TestProposal_NeverCarriesASecretValue(t *testing.T) {
	scope := SetOf("k8s")
	secrets := []string{
		"apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\ndata:\n  password: cGFzc3dvcmQxMjM=\n",
		"env:\n  DB_PASSWORD=hunter2hunter2\n",
		"stringData:\n  api_key: \"sk-live-0123456789abcdef\"\nkind: Secret",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----",
		"token: ghp_abcdefghijklmnop",
	}
	for _, s := range secrets {
		p := validOperatorAction()
		p.Change = s
		if err := p.Validate(scope); !errors.Is(err, ErrProposalSecret) {
			t.Errorf("change %q accepted (%v), want %v", s, err, ErrProposalSecret)
		}
	}
	references := []string{
		"env:\n- name: DB_PASSWORD\n  valueFrom:\n    secretKeyRef:\n      name: db-credentials\n      key: password\n",
		"password: ${DB_PASSWORD}",
		"api_key: <redacted>",
		"rotate the token named prod-api-token in the vault",
		"secret: db-credentials",
		"token:",
	}
	for _, s := range references {
		p := validOperatorAction()
		p.Change = s
		if err := p.Validate(scope); err != nil {
			t.Errorf("reference %q rejected: %v", s, err)
		}
	}
}
