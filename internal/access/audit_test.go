package access_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// recordingAudit captures every audit row inserted; tests assert on the
// captured slice. concurrency-safe so it's also fine for parallel tests.
type recordingAudit struct {
	mu    sync.Mutex
	rows  []audit.Event
	errFn func(audit.Event) error // optional failure injection
}

func (r *recordingAudit) Insert(_ context.Context, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.errFn != nil {
		if err := r.errFn(e); err != nil {
			return err
		}
	}
	r.rows = append(r.rows, e)
	return nil
}

func (r *recordingAudit) snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Event, len(r.rows))
	copy(out, r.rows)
	return out
}

// TestPhaseF_AllowedAccessProducesOneAllowAuditRow — Phase F behavioural
// acceptance: a permitted infra action writes exactly one audit row with
// the correct principal, action, zone, source, decision=allow.
func TestPhaseF_AllowedAccessProducesOneAllowAuditRow(t *testing.T) {
	repo := newFakeRepo()
	repo.assign("s-k8s", "z-read")
	repo.grant("alice", "z-read")
	engine := rbac.NewPolicyEngine(repo)

	called := false
	reg := adapters.NewRegistry()
	reg.Register("s-k8s", fakeK8s{called: &called})

	rec := &recordingAudit{}
	a := access.New(reg, nil, engine, rec)

	if _, err := a.K8sListResources(context.Background(), "alice", "s-k8s", "pods", ""); err != nil {
		t.Fatalf("allowed call returned error: %v", err)
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("got %d audit rows, want exactly 1: %#v", len(rows), rows)
	}
	r := rows[0]
	if r.Principal != "alice" {
		t.Errorf("principal = %q, want %q", r.Principal, "alice")
	}
	if r.Action != string(rbac.ActionRead) {
		t.Errorf("action = %q, want %q", r.Action, rbac.ActionRead)
	}
	if r.Zone != "z-read" {
		t.Errorf("zone = %q, want %q", r.Zone, "z-read")
	}
	if r.Source != "s-k8s" {
		t.Errorf("source = %q, want %q", r.Source, "s-k8s")
	}
	if r.Decision != audit.DecisionAllow {
		t.Errorf("decision = %q, want %q", r.Decision, audit.DecisionAllow)
	}
	if r.Kind != audit.KindInfraAccess {
		t.Errorf("kind = %q, want %q", r.Kind, audit.KindInfraAccess)
	}
	if r.Reason == "" {
		t.Error("reason was empty; allow rows should record a minimal marker (e.g. policy_allow)")
	}
}

// TestPhaseF_DeniedAccessProducesOneDenyAuditRow — denied infra action
// writes exactly one row with decision=deny and the denial reason.
func TestPhaseF_DeniedAccessProducesOneDenyAuditRow(t *testing.T) {
	repo := newFakeRepo()
	repo.assign("s-k8s", "z-read")
	// alice is granted z-read, mallory is not.
	repo.grant("alice", "z-read")
	engine := rbac.NewPolicyEngine(repo)

	called := false
	reg := adapters.NewRegistry()
	reg.Register("s-k8s", fakeK8s{called: &called})

	rec := &recordingAudit{}
	a := access.New(reg, nil, engine, rec)

	_, err := a.K8sListResources(context.Background(), "mallory", "s-k8s", "pods", "")
	if !errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
	if called {
		t.Fatal("adapter was called despite a denied decision")
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("got %d audit rows, want 1: %#v", len(rows), rows)
	}
	r := rows[0]
	if r.Decision != audit.DecisionDeny {
		t.Errorf("decision = %q, want %q", r.Decision, audit.DecisionDeny)
	}
	if r.Principal != "mallory" {
		t.Errorf("principal = %q, want %q", r.Principal, "mallory")
	}
	if r.Reason == "" {
		t.Error("reason was empty; deny rows must carry the structured denial reason")
	}
	if r.Reason != "no_grant" {
		t.Errorf("reason = %q, want %q (mallory has the right zone-action but no policy)", r.Reason, "no_grant")
	}
}

// TestPhaseF_FailClosedOnMutate — design §4: a mutating action whose
// audit row cannot be written MUST NOT proceed. The adapter must not be
// invoked, and the caller must see an error.
func TestPhaseF_FailClosedOnMutate(t *testing.T) {
	repo := newFakeRepo()
	repo.assign("s-github", "z-write")
	repo.grant("alice", "z-write")
	engine := rbac.NewPolicyEngine(repo)

	// Use a fake GitHub mutate adapter.
	mutateCalled := false
	reg := adapters.NewRegistry()
	reg.Register("s-github", fakeMutatingGitHub{called: &mutateCalled})

	auditFail := errors.New("audit store down")
	rec := &recordingAudit{
		errFn: func(e audit.Event) error {
			if e.Action == string(rbac.ActionMutate) {
				return auditFail
			}
			return nil
		},
	}
	a := access.New(reg, nil, engine, rec)

	err := a.GitHubPostComment(context.Background(), "alice", "s-github", "o", "r", 1, "hi")
	if err == nil {
		t.Fatal("expected an error from the mutating action when audit-write fails (fail-closed); got nil")
	}
	if mutateCalled {
		t.Fatal("mutating adapter was invoked despite audit-write failure; fail-closed was not enforced")
	}
	// And the principal-deny path was NOT exercised (the principal had a grant).
	if errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("error was ErrPermissionDenied; the failure should reflect the audit-write, got: %v", err)
	}
}

// TestPhaseF_FailOpenOnRead — design §4: a read action whose audit row
// cannot be written MUST still proceed (availability > audit
// completeness for reads). The adapter must be invoked, and the caller
// must see no error.
func TestPhaseF_FailOpenOnRead(t *testing.T) {
	repo := newFakeRepo()
	repo.assign("s-k8s", "z-read")
	repo.grant("alice", "z-read")
	engine := rbac.NewPolicyEngine(repo)

	called := false
	reg := adapters.NewRegistry()
	reg.Register("s-k8s", fakeK8s{called: &called})

	rec := &recordingAudit{
		errFn: func(_ audit.Event) error { return errors.New("audit store down") },
	}
	a := access.New(reg, nil, engine, rec)

	_, err := a.K8sListResources(context.Background(), "alice", "s-k8s", "pods", "")
	if err != nil {
		t.Fatalf("read returned %v; reads must fail-open even when audit-write fails", err)
	}
	if !called {
		t.Fatal("adapter was NOT invoked; reads must proceed despite audit-write failure")
	}
}

// --- fake mutating GitHub adapter (Phase F audit tests only) ---
//
// Implements the full github.GitHubAdapter interface; only PostComment is
// the one whose call we observe to assert fail-closed.

type fakeMutatingGitHub struct {
	base
	called *bool
}

func (f fakeMutatingGitHub) WebhookSecret() string { return "" }
func (f fakeMutatingGitHub) GetPR(_ context.Context, _, _ string, _ int) (*githubadapter.PRInfo, error) {
	return nil, nil
}
func (f fakeMutatingGitHub) GetPRDiff(_ context.Context, _, _ string, _ int) (string, error) {
	return "", nil
}
func (f fakeMutatingGitHub) PostComment(_ context.Context, _, _ string, _ int, _ string) error {
	*f.called = true
	return nil
}
func (f fakeMutatingGitHub) RequestChanges(_ context.Context, _, _ string, _ int, _ string) error {
	*f.called = true
	return nil
}
func (f fakeMutatingGitHub) ListPRs(_ context.Context, _, _ string, _ string) ([]*githubadapter.PRInfo, error) {
	return nil, nil
}
