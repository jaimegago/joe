package runfindings

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

func newSession(t *testing.T, s *store.Store) string {
	t.Helper()
	repo := sessionmodel.NewRepository(s.DB(), store.DriverSQLite)
	id := uuid.NewString()
	if _, err := repo.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: id, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "svc:agent:core",
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return id
}

func labelled(key, run, scope, session string, label Label, at time.Time) Finding {
	return Finding{ID: uuid.NewString(), Key: key, RunID: run, ScopeKey: scope, SessionID: session,
		Type: TypeIssue, Reference: ReferenceSelfReport, SubjectNodeID: "n-" + key, SubjectComponentID: "k8s",
		Claim: "claim " + key, Consequence: ConsequenceDegraded, Band: BandP2, Weight: 1, Label: label,
		Derivation:        Derivation{Consequence: ConsequenceDegraded, EvidenceLine: "e", Weight: 1, BaseBand: BandP2, Band: BandP2, Movement: MovementNone, Reach: Reach{Dependents: []string{}, Components: []string{}}},
		EvidenceSessionID: session, EstablishedAt: at, CreatedAt: at}
}

func TestRepository_PutListAndLatest(t *testing.T) {
	s := newTestStore(t)
	repo := NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()
	scope := ScopeKey([]string{"k8s"})
	t1 := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	sess1, sess2 := newSession(t, s), newSession(t, s)

	f1 := labelled("a", "run-1", scope, sess1, LabelNew, t1)
	f1.Proposal = &Proposal{Shape: ShapeOperatorAction, Target: ProposalTarget{NodeID: "n-a", ComponentID: "k8s", SystemID: "x"}, Base: "b", Change: "c", ExpectedEffect: "e", Limits: "l", Status: ProposalStatusUnverified}
	f1.EvidenceMessageID = "m-1"
	if err := repo.Put(ctx, FindingSet{RunID: "run-1", ScopeKey: scope, SessionID: sess1, CreatedAt: t1}, []Finding{f1}); err != nil {
		t.Fatal(err)
	}
	// A second run on the same scope with no findings is still recorded.
	if err := repo.Put(ctx, FindingSet{RunID: "run-2", ScopeKey: scope, SessionID: sess2, CreatedAt: t2}, nil); err != nil {
		t.Fatal(err)
	}
	// A run on another scope.
	other := ScopeKey([]string{"pg"})
	if err := repo.Put(ctx, FindingSet{RunID: "run-3", ScopeKey: other, SessionID: sess2, CreatedAt: t1}, []Finding{labelled("z", "run-3", other, sess2, LabelNew, t1)}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListByRun(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "a" || got[0].Proposal == nil || got[0].Proposal.Shape != ShapeOperatorAction ||
		got[0].EvidenceMessageID != "m-1" || got[0].Derivation.EvidenceLine != "e" || !got[0].EstablishedAt.Equal(t1) {
		t.Errorf("round trip = %+v", got)
	}

	set, err := repo.GetSet(ctx, "run-2")
	if err != nil || set == nil || set.FindingCount != 0 || set.ScopeKey != scope {
		t.Errorf("GetSet(run-2) = %+v, %v", set, err)
	}
	if set, err := repo.GetSet(ctx, "nope"); err != nil || set != nil {
		t.Errorf("GetSet(nope) = %+v, %v; want nil, nil", set, err)
	}

	latest, err := repo.LatestSetForScope(ctx, scope, time.Time{})
	if err != nil || latest == nil || latest.RunID != "run-2" {
		t.Errorf("LatestSetForScope = %+v, %v; want run-2", latest, err)
	}
	// A run labelling itself looks behind its own created_at.
	prev, err := repo.LatestSetForScope(ctx, scope, t2)
	if err != nil || prev == nil || prev.RunID != "run-1" {
		t.Errorf("LatestSetForScope(before t2) = %+v, %v; want run-1", prev, err)
	}
	if none, err := repo.LatestSetForScope(ctx, "unknown-scope", time.Time{}); err != nil || none != nil {
		t.Errorf("unknown scope = %+v, %v", none, err)
	}

	sets, findings, err := repo.ListLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Latest per scope: run-2 (clean) and run-3. run-1's finding is NOT in the
	// view — a clean run reads as clean, not as the previous run's list.
	if len(sets) != 2 {
		t.Fatalf("sets = %+v", sets)
	}
	runs := map[string]bool{}
	for _, st := range sets {
		runs[st.RunID] = true
	}
	if !runs["run-2"] || !runs["run-3"] {
		t.Errorf("latest sets = %v", runs)
	}
	if len(findings) != 1 || findings[0].Key != "z" {
		t.Errorf("latest findings = %+v, want only z", findings)
	}
}

func TestRepository_PutIsAtomicAndValidated(t *testing.T) {
	s := newTestStore(t)
	repo := NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()
	sess := newSession(t, s)
	now := time.Now().UTC()

	bad := labelled("a", "other-run", "scope", sess, LabelNew, now)
	if err := repo.Put(ctx, FindingSet{RunID: "run-1", ScopeKey: "scope", SessionID: sess}, []Finding{bad}); err == nil {
		t.Error("a finding from another run was accepted")
	}
	unlabelled := labelled("a", "run-1", "scope", sess, "", now)
	if err := repo.Put(ctx, FindingSet{RunID: "run-1", ScopeKey: "scope", SessionID: sess}, []Finding{unlabelled}); err == nil {
		t.Error("an unlabelled finding was accepted")
	}
	// Nothing landed: the set row is written in the same transaction.
	if set, _ := repo.GetSet(ctx, "run-1"); set != nil {
		t.Error("set row survived a failed Put")
	}
	// Duplicate (run, key) fails the whole batch.
	dup := []Finding{labelled("a", "run-1", "scope", sess, LabelNew, now), labelled("a", "run-1", "scope", sess, LabelNew, now)}
	if err := repo.Put(ctx, FindingSet{RunID: "run-1", ScopeKey: "scope", SessionID: sess}, dup); err == nil {
		t.Error("duplicate (run, key) accepted")
	}
	if set, _ := repo.GetSet(ctx, "run-1"); set != nil {
		t.Error("set row survived a failed batch")
	}
	if err := repo.Put(ctx, FindingSet{RunID: "", ScopeKey: "scope", SessionID: sess}, nil); err == nil {
		t.Error("set without run_id accepted")
	}
}

// TestRepository_SessionExpungeCascades: deleting a run's session expunges its
// finding set and its findings (§6-C).
func TestRepository_SessionExpungeCascades(t *testing.T) {
	s := newTestStore(t)
	repo := NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()
	sess := newSession(t, s)
	now := time.Now().UTC()
	if err := repo.Put(ctx, FindingSet{RunID: "run-1", ScopeKey: "scope", SessionID: sess, CreatedAt: now},
		[]Finding{labelled("a", "run-1", "scope", sess, LabelNew, now)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM agent_sessions WHERE id = ?`, sess); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM run_findings`).Scan(&n); err != nil || n != 0 {
		t.Errorf("run_findings after expunge = %d, %v; want 0", n, err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM run_finding_sets`).Scan(&n); err != nil || n != 0 {
		t.Errorf("run_finding_sets after expunge = %d, %v; want 0", n, err)
	}
}

func TestRepository_SchemaEnforcesClosedVocabularies(t *testing.T) {
	s := newTestStore(t)
	repo := NewRepository(s.DB(), store.DriverSQLite)
	ctx := context.Background()
	sess := newSession(t, s)
	now := time.Now().UTC()
	for _, mut := range []func(*Finding){
		func(f *Finding) { f.Type = "severity" },
		func(f *Finding) { f.Band = "P4" },
		func(f *Finding) { f.Label = "gone" },
	} {
		f := labelled("a", "run-x", "scope", sess, LabelNew, now)
		mut(&f)
		if err := repo.Put(ctx, FindingSet{RunID: "run-x", ScopeKey: "scope", SessionID: sess}, []Finding{f}); err == nil {
			t.Errorf("schema accepted %+v", f)
		}
	}
}

func TestScopeKey_IsOrderAndDuplicateInsensitive(t *testing.T) {
	a := ScopeKey([]string{"k8s", "pg", "k8s"})
	b := ScopeKey([]string{"pg", " k8s "})
	if a != b {
		t.Error("same component set yielded two scope keys")
	}
	if a == ScopeKey([]string{"pg"}) {
		t.Error("different component sets yielded one scope key")
	}
}

func TestComputeKey_IgnoresTheRunAndNormalisesTheClaim(t *testing.T) {
	a := ComputeKey(TypeIssue, "db", "Not  accepting connections")
	b := ComputeKey(TypeIssue, "db", "not accepting connections ")
	if a != b {
		t.Error("whitespace and case changed the key")
	}
	if a == ComputeKey(TypeRisk, "db", "not accepting connections") {
		t.Error("type did not enter the key")
	}
	if a == ComputeKey(TypeIssue, "db2", "not accepting connections") {
		t.Error("subject did not enter the key")
	}
}
