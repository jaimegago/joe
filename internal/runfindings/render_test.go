package runfindings

import (
	"strings"
	"testing"
	"time"
)

func renderFixture() []Finding {
	ts := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	return []Finding{
		{Key: "k-db", Type: TypeIssue, SubjectNodeID: "db", SubjectComponentID: "pg", Claim: "database not accepting connections",
			Consequence: ConsequenceUnavailable, Band: BandP0, ReachCount: 3, Weight: 1, Label: LabelNew,
			Derivation:        Derivation{Consequence: ConsequenceUnavailable, EvidenceLine: "pg_isready: no response", Reach: Reach{Count: 3}, Weight: 1, BaseBand: BandP1, Band: BandP0, Movement: MovementRaised},
			EvidenceSessionID: "sess-1", EvidenceMessageID: "m-7", EstablishedAt: ts},
		{Key: "k-island", Type: TypeRisk, SubjectNodeID: "island", SubjectComponentID: "k8s", Claim: "single replica", Condition: "a node drain takes the only pod down",
			Consequence: ConsequenceWouldBeUnavailable, Band: BandP2, Label: LabelStanding, Weight: 1,
			Derivation:        Derivation{Consequence: ConsequenceWouldBeUnavailable, EvidenceLine: "replicas: 1", Weight: 1, BaseBand: BandP2, Band: BandP2, Movement: MovementNone},
			EvidenceSessionID: "sess-0", EstablishedAt: ts.Add(-time.Hour),
			Proposal: &Proposal{Shape: ShapeLiveObjectChange, Target: ProposalTarget{NodeID: "island", ComponentID: "k8s", SystemID: "default/island"}, Base: "generation 7", Change: "spec:\n  replicas: 2", ExpectedEffect: "2 replicas available", Limits: "capacity not checked", Status: ProposalStatusUnverified, Executable: true}},
		{Key: "k-old", Type: TypeIssue, SubjectNodeID: "svc-a", SubjectComponentID: "k8s", Claim: "crashlooping", Consequence: ConsequenceDegraded, Band: BandP2, Label: LabelResolved, Weight: 1,
			Derivation: Derivation{EvidenceLine: "CrashLoopBackOff"}, EvidenceSessionID: "sess-0", EstablishedAt: ts.Add(-2 * time.Hour)},
		{Key: "k-gu", Type: TypeGraphUpdate, SubjectNodeID: "svc-a", SubjectComponentID: "k8s", Claim: "stores_in points at a datastore no component covers", Band: BandP3, Label: LabelNew, Weight: 1,
			Derivation: Derivation{EvidenceLine: "edge svc-a -stores_in-> redis-unknown"}, EvidenceSessionID: "sess-1", EstablishedAt: ts,
			Proposal: &Proposal{Shape: ShapeOperatorAction, Target: ProposalTarget{NodeID: "svc-a", ComponentID: "k8s", SystemID: "default/svc-a"}, Base: "no redis component registered", Change: "register the redis instance as a component", ExpectedEffect: "the stores_in edge resolves to a registered component", Limits: "type may be blocked by the allow-list", Status: ProposalStatusUnverified}},
	}
}

// TestRender_NothingExistsOnlyInProse: every finding's claim, band, label and
// evidence are in the output, graph updates are delivered apart, and the
// ranked list is by band.
func TestRender_NothingExistsOnlyInProse(t *testing.T) {
	fs := renderFixture()
	out := Render(RenderInput{Findings: fs, Coverage: Coverage{Covered: []string{"k8s", "pg"}, NotCovered: []string{"mon"}}, StopReason: "token ceiling",
		Dropped: []Dropped{{Kind: DroppedFinding}, {Kind: DroppedProposal}}})

	for _, f := range fs {
		for _, want := range []string{f.Claim, f.SubjectNodeID, string(f.Label), f.Derivation.EvidenceLine, f.EvidenceSessionID} {
			if !strings.Contains(out, want) {
				t.Errorf("rendered turn lacks %q", want)
			}
		}
		if f.Proposal != nil {
			for _, want := range []string{f.Proposal.Base, f.Proposal.ExpectedEffect, f.Proposal.Limits, ProposalStatusUnverified, string(f.Proposal.Shape)} {
				if !strings.Contains(out, want) {
					t.Errorf("rendered proposal lacks %q", want)
				}
			}
		}
	}
	findingsIdx := strings.Index(out, "## Findings")
	admissionsIdx := strings.Index(out, "## Proposed admissions")
	if findingsIdx < 0 || admissionsIdx < 0 || admissionsIdx < findingsIdx {
		t.Fatalf("sections out of order: %q", out)
	}
	ranked, admissions := out[findingsIdx:admissionsIdx], out[admissionsIdx:]
	if strings.Contains(ranked, "k-gu") || strings.Contains(ranked, "redis-unknown") {
		t.Error("graph update rendered in the ranked list")
	}
	if !strings.Contains(admissions, "redis-unknown") || !strings.Contains(admissions, "not executable") {
		t.Error("graph update missing from the admissions list, or not marked not executable")
	}
	for _, want := range []string{"### P0", "### P1", "### P2", "### P3", "No P1 findings.", "No P3 findings."} {
		if !strings.Contains(ranked, want) {
			t.Errorf("ranked list lacks %q", want)
		}
	}
	if strings.Index(ranked, "database not accepting") > strings.Index(ranked, "single replica") {
		t.Error("P0 finding rendered after a P2 finding")
	}
	if !strings.Contains(out, "did not cover: mon") || !strings.Contains(out, "not a clean bill") || !strings.Contains(out, "token ceiling") {
		t.Error("coverage and stop reason not rendered")
	}
	if !strings.Contains(out, "Not emitted: 1 candidate finding(s) and 1 proposal(s)") {
		t.Error("dropped count not rendered")
	}
	if !strings.Contains(out, "3 finding(s) on 2 component(s): 1 new, 1 standing, 1 resolved, 0 unknown.") {
		t.Errorf("summary line wrong:\n%s", out)
	}
}

func TestRender_EmptyRunIsLegible(t *testing.T) {
	out := Render(RenderInput{Coverage: Coverage{Covered: []string{"k8s"}}})
	if !strings.Contains(out, "0 finding(s)") || !strings.Contains(out, "## Proposed admissions\n\nNone.") {
		t.Errorf("empty render:\n%s", out)
	}
	if strings.Contains(out, "### Not covered") || strings.Contains(out, "Not emitted") {
		t.Error("empty render claims limits it has no input for")
	}
}

func TestRender_Deterministic(t *testing.T) {
	a := Render(RenderInput{Findings: renderFixture()})
	rev := renderFixture()
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	if b := Render(RenderInput{Findings: rev}); a != b {
		t.Error("render order depends on input order")
	}
}
