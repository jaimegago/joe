package runfindings

import (
	"testing"
	"time"
)

func mk(key, component string, label Label) Finding {
	return Finding{ID: "id-" + key, Key: key, RunID: "prev", ScopeKey: "s", SessionID: "sess-prev",
		Type: TypeIssue, SubjectNodeID: key, SubjectComponentID: component, Claim: key, Band: BandP2,
		Label: label, EvidenceSessionID: "sess-prev", EvidenceMessageID: "m-" + key,
		EstablishedAt: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)}
}

func TestLabelAgainstPrevious_FourLabels(t *testing.T) {
	previous := []Finding{
		mk("a", "k8s", LabelNew),      // re-found → standing
		mk("b", "k8s", LabelStanding), // not re-found, covered → resolved
		mk("c", "pg", LabelUnknown),   // not re-found, not covered → unknown again
		mk("d", "k8s", LabelResolved), // already resolved last run → not carried
		mk("e", "pg", LabelNew),       // not re-found, not covered → unknown
	}
	current := []Finding{
		{Key: "a", SubjectComponentID: "k8s"},
		{Key: "f", SubjectComponentID: "k8s"},
	}
	for i := range current {
		current[i].RunID, current[i].SessionID, current[i].ID = "cur", "sess-cur", "id-cur-"+current[i].Key
	}
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	out := LabelAgainstPrevious(current, previous, LabelInput{
		Covered: SetOf("k8s"), RunID: "cur", ScopeKey: "s", SessionID: "sess-cur", Now: now,
	})
	got := map[string]Finding{}
	for _, f := range out {
		got[f.Key] = f
	}
	want := map[string]Label{"a": LabelStanding, "f": LabelNew, "b": LabelResolved, "c": LabelUnknown, "e": LabelUnknown}
	if len(got) != len(want) {
		t.Fatalf("got keys %v, want %v", keys(got), want)
	}
	for k, l := range want {
		if got[k].Label != l {
			t.Errorf("%s: label %s, want %s", k, got[k].Label, l)
		}
		if got[k].RunID != "cur" || got[k].SessionID != "sess-cur" {
			t.Errorf("%s: run/session = %s/%s, want cur/sess-cur", k, got[k].RunID, got[k].SessionID)
		}
	}
	if _, carried := got["d"]; carried {
		t.Error("a previously resolved finding was carried forward")
	}
	// A carried row keeps the previous evidence pointer and gets a new row id.
	b := got["b"]
	if b.EvidenceSessionID != "sess-prev" || b.EvidenceMessageID != "m-b" || b.ID == "id-b" || !b.CreatedAt.Equal(now) {
		t.Errorf("carried b = %+v", b)
	}
	// A current finding is never changed by the previous one: labelling only.
	a := got["a"]
	if a.Band != "" || a.EvidenceSessionID != "" {
		t.Errorf("current a took fields from previous: %+v", a)
	}
}

func TestLabelAgainstPrevious_NoPreviousRunMeansEverythingIsNew(t *testing.T) {
	out := LabelAgainstPrevious([]Finding{{Key: "x"}, {Key: "y"}}, nil, LabelInput{Covered: SetOf()})
	for _, f := range out {
		if f.Label != LabelNew {
			t.Errorf("%s labelled %s, want new", f.Key, f.Label)
		}
	}
}

func keys(m map[string]Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
