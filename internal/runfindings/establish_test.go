package runfindings

import (
	"context"
	"strings"
	"testing"
	"time"
)

func establishFixture(t *testing.T) (GraphReader, EstablishInput) {
	t.Helper()
	g := reachTopology(t)
	in := EstablishInput{
		Reader:    g,
		Scope:     SetOf("k8s", "pg"),
		RunID:     "run-1",
		ScopeKey:  ScopeKey([]string{"k8s", "pg"}),
		SessionID: "sess-1",
		Now:       time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
	}
	return g, in
}

func TestEstablish_ComputesBandReachAndKeyFromTheGraph(t *testing.T) {
	_, in := establishFixture(t)
	cands := []Candidate{
		{Type: "issue", SubjectNodeID: "db", Claim: "the database reports it is not accepting connections", Consequence: "unavailable", EvidenceLine: "pg_isready: no response"},
		{Type: "risk", SubjectNodeID: "island", Claim: "single replica", Consequence: "would_be_unavailable", Condition: "a node drain takes the only pod down", EvidenceLine: "replicas: 1"},
	}
	fs, dropped, err := Establish(context.Background(), cands, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 0 {
		t.Fatalf("dropped = %+v", dropped)
	}
	if len(fs) != 2 {
		t.Fatalf("got %d findings", len(fs))
	}
	db := fs[0]
	if db.Band != BandP0 || db.ReachCount != 3 || db.Derivation.Movement != MovementRaised {
		t.Errorf("db finding = band %s reach %d movement %s; want P0, 3, raised", db.Band, db.ReachCount, db.Derivation.Movement)
	}
	if db.SubjectComponentID != "pg" || db.Reference != ReferenceSelfReport || db.Weight != DefaultWeight {
		t.Errorf("db finding component/reference/weight = %q/%s/%g", db.SubjectComponentID, db.Reference, db.Weight)
	}
	if db.Key != ComputeKey(TypeIssue, "db", "the database reports it is not accepting connections") || db.Key == "" {
		t.Errorf("key not derived from type, subject and claim")
	}
	if db.Label != "" {
		t.Errorf("Establish labelled a finding (%s); labelling is LabelAgainstPrevious's", db.Label)
	}
	if db.EvidenceSessionID != "sess-1" || !db.EstablishedAt.Equal(in.Now) {
		t.Errorf("evidence pointer = %s@%s", db.EvidenceSessionID, db.EstablishedAt)
	}
	island := fs[1]
	if island.Band != BandP2 || island.ReachCount != 0 || island.Condition == "" {
		t.Errorf("island risk = band %s reach %d condition %q; want P2, 0, set", island.Band, island.ReachCount, island.Condition)
	}
}

func TestEstablish_DropsWhatTheFindingModelForbids(t *testing.T) {
	_, in := establishFixture(t)
	cands := []Candidate{
		{Type: "improvement", SubjectNodeID: "db", Claim: "x", Consequence: "degraded", EvidenceLine: "e"},
		{Type: "risk", SubjectNodeID: "db", Claim: "no condition", Consequence: "would_be_degraded", EvidenceLine: "e"},
		{Type: "issue", SubjectNodeID: "db", Claim: "no evidence", Consequence: "degraded"},
		{Type: "issue", SubjectNodeID: "db", Claim: "wrong consequence", Consequence: "would_be_degraded", EvidenceLine: "e"},
		{Type: "graph_update", SubjectNodeID: "db", Claim: "has consequence", Consequence: "degraded", EvidenceLine: "e"},
		{Type: "issue", SubjectNodeID: "ghost", Claim: "not in graph", Consequence: "degraded", EvidenceLine: "e"},
		{Type: "issue", SubjectNodeID: "svc-b", Claim: "outside scope", Consequence: "degraded", EvidenceLine: "e"},
		{Type: "issue", SubjectNodeID: "", Claim: "no subject", Consequence: "degraded", EvidenceLine: "e"},
		{Type: "issue", SubjectNodeID: "db", Claim: "dup", Consequence: "degraded", EvidenceLine: "e"},
		{Type: "issue", SubjectNodeID: "db", Claim: "  DUP ", Consequence: "unavailable", EvidenceLine: "e"},
	}
	fs, dropped, err := Establish(context.Background(), cands, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Claim != "dup" {
		t.Errorf("findings = %+v, want only the first 'dup'", fs)
	}
	if len(dropped) != 9 {
		t.Fatalf("dropped %d, want 9: %+v", len(dropped), dropped)
	}
	wantReasons := []string{"not one of", "condition", "evidence_line", "not admissible", "not admissible", "not in the graph", "outside the run's resolved component set", "required", "duplicate"}
	for i, d := range dropped {
		if d.Kind != DroppedFinding || !strings.Contains(d.Reason, wantReasons[i]) {
			t.Errorf("dropped[%d] = %+v, want reason containing %q", i, d, wantReasons[i])
		}
	}
}

func TestEstablish_ProposalInvariants(t *testing.T) {
	_, in := establishFixture(t)
	good := &CandidateProposal{Shape: "live_object_change", TargetNodeID: "svc-a", TargetSystemID: "default/svc-a", Base: "generation 7", Change: `{"spec":{"replicas":2}}`, ExpectedEffect: "2 replicas available", Limits: "capacity not checked", LiveObject: &LiveObjectRef{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "default", Name: "svc-a"}}
	noBase := *good
	noBase.Base = ""
	outOfScope := *good
	outOfScope.TargetNodeID = "svc-b"
	secret := *good
	secret.Change = "password: hunter2hunter2"
	graphUpdateWrongShape := *good
	cands := []Candidate{
		{Type: "issue", SubjectNodeID: "svc-a", Claim: "a", Consequence: "degraded", EvidenceLine: "e", Proposal: good},
		{Type: "issue", SubjectNodeID: "svc-a", Claim: "b", Consequence: "degraded", EvidenceLine: "e", Proposal: &noBase},
		{Type: "issue", SubjectNodeID: "svc-a", Claim: "c", Consequence: "degraded", EvidenceLine: "e", Proposal: &outOfScope},
		{Type: "issue", SubjectNodeID: "svc-a", Claim: "d", Consequence: "degraded", EvidenceLine: "e", Proposal: &secret},
		{Type: "graph_update", SubjectNodeID: "svc-a", Claim: "e", EvidenceLine: "e", Proposal: &graphUpdateWrongShape},
	}
	fs, dropped, err := Establish(context.Background(), cands, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 5 {
		t.Fatalf("got %d findings, want 5 (a dropped proposal keeps its finding)", len(fs))
	}
	if fs[0].Proposal == nil || fs[0].Proposal.Target.ComponentID != "k8s" || fs[0].Proposal.Status != ProposalStatusUnverified {
		t.Errorf("good proposal = %+v", fs[0].Proposal)
	}
	for i := 1; i < 5; i++ {
		if fs[i].Proposal != nil {
			t.Errorf("finding %q kept an invalid proposal", fs[i].Claim)
		}
	}
	if len(dropped) != 4 {
		t.Fatalf("dropped = %+v, want 4 proposals", dropped)
	}
	for _, d := range dropped {
		if d.Kind != DroppedProposal || d.Key == "" {
			t.Errorf("dropped = %+v, want a keyed proposal drop", d)
		}
	}
	if !strings.Contains(dropped[0].Reason, "no base") || !strings.Contains(dropped[1].Reason, "outside") ||
		!strings.Contains(dropped[2].Reason, "secret") || !strings.Contains(dropped[3].Reason, "operator action") {
		t.Errorf("reasons = %q %q %q %q", dropped[0].Reason, dropped[1].Reason, dropped[2].Reason, dropped[3].Reason)
	}
}

func TestEstablish_RequiresGraphAndScope(t *testing.T) {
	g, in := establishFixture(t)
	if _, _, err := Establish(context.Background(), nil, EstablishInput{Scope: in.Scope}); err == nil {
		t.Error("nil reader accepted")
	}
	if _, _, err := Establish(context.Background(), nil, EstablishInput{Reader: g}); err == nil {
		t.Error("nil scope accepted")
	}
}
