package runfindings

import (
	"strings"
	"testing"
)

const sampleBlock = "```joe-findings\n" +
	`{"findings": [{"type": "issue", "subject_node_id": "svc-a", "claim": "0 of 1 replicas available", "consequence": "unavailable", "evidence_line": "Available=False"}]}` +
	"\n```"

func TestSplitFindingsBlock_LastBlockWinsAndProseIsKept(t *testing.T) {
	content := "Here is what I found.\n\n" + sampleBlock + "\n\nThanks!"
	block, rest, found := SplitFindingsBlock(content)
	if !found {
		t.Fatal("block not found")
	}
	if !strings.HasPrefix(block, `{"findings"`) {
		t.Errorf("block = %q", block)
	}
	if rest != "Here is what I found.\n\n\n\nThanks!" && !strings.Contains(rest, "Thanks!") {
		t.Errorf("rest = %q", rest)
	}
	if strings.Contains(rest, FindingsFence) {
		t.Errorf("rest still carries the fence: %q", rest)
	}

	// A second block later in the turn wins over the first.
	two := sampleBlock + "\n\n```joe-findings\n{\"findings\": []}\n```"
	block, _, found = SplitFindingsBlock(two)
	if !found || block != `{"findings": []}` {
		t.Errorf("last block did not win: %q", block)
	}
}

func TestSplitFindingsBlock_IgnoresOtherFencesAndUnclosed(t *testing.T) {
	other := "```yaml\nkind: Deployment\n```\n"
	if _, rest, found := SplitFindingsBlock(other); found || rest != other {
		t.Errorf("yaml block mistaken for findings: found=%v rest=%q", found, rest)
	}
	// A findings fence nested inside another block is that block's content.
	nested := "```text\n```joe-findings\n{}\n```\n"
	if _, _, found := SplitFindingsBlock(nested); found {
		t.Error("nested fence mistaken for a findings block")
	}
	unclosed := "```joe-findings\n{\"findings\": []}"
	if _, _, found := SplitFindingsBlock(unclosed); found {
		t.Error("unclosed block accepted")
	}
	if _, _, found := SplitFindingsBlock("no block at all"); found {
		t.Error("plain prose reported a block")
	}
}

func TestParseCandidates(t *testing.T) {
	block, _, _ := SplitFindingsBlock(sampleBlock)
	cands, err := ParseCandidates(block)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].SubjectNodeID != "svc-a" || cands[0].Consequence != "unavailable" {
		t.Errorf("candidates = %+v", cands)
	}
	if _, err := ParseCandidates(`{"findings": []}`); err != nil {
		t.Errorf("empty list rejected: %v", err)
	}
	if _, err := ParseCandidates(`{}`); err == nil {
		t.Error("object without findings array accepted")
	}
	if _, err := ParseCandidates(`not json`); err == nil {
		t.Error("garbage accepted")
	}
}

// TestOutputContract_NamesNoComputedField: the schema the model is handed
// carries no slot for anything code computes.
func TestOutputContract_NamesNoComputedField(t *testing.T) {
	for _, forbidden := range []string{`"band"`, `"severity"`, `"reach"`, `"weight"`, `"label"`, `"key"`} {
		if strings.Contains(OutputContract, forbidden) {
			t.Errorf("OutputContract offers the model a %s field", forbidden)
		}
	}
	if !strings.Contains(OutputContract, FindingsFence) {
		t.Error("OutputContract does not name the fence")
	}
}
