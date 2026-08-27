package agentloop

import (
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
)

// An evaluator deciding whether an answer named a cause, or dismissed a signal,
// otherwise has only the prose — and deciding it from prose measures vocabulary
// rather than reasoning. These tests pin what joe-pm
// threads/declared-diagnostic-conclusion.md ordered: a declared conclusion on
// the turn that ends in words, and an absence that stays an absence.

// TestSplitConclusion covers the parse in isolation.
func TestSplitConclusion(t *testing.T) {
	cases := []struct {
		name          string
		content       string
		wantRootCause string
		wantDiscarded []DiscardedSignal
		wantDeclared  bool
		wantContent   string
	}{
		{
			name:          "nothing declared",
			content:       "the cause is a bad readiness probe",
			wantRootCause: "",
			wantDeclared:  false,
			wantContent:   "the cause is a bad readiness probe",
		},
		{
			name:          "root cause alone",
			content:       "user-service is being OOM killed.\n\nROOT-CAUSE: a memory leak in user-service exhausting its limit",
			wantRootCause: "a memory leak in user-service exhausting its limit",
			wantDeclared:  true,
			wantContent:   "user-service is being OOM killed.",
		},
		{
			name:          "root cause and two discards",
			content:       "Full write-up above.\nROOT-CAUSE: OOM kill from a memory leak\nDISCARDED: node-1 CPU at 97% | the pegged CPU is the batch job's, and user-service is not CPU throttled\nDISCARDED: batch-processor-x9k2 | a co-tenant, not in the restart path",
			wantRootCause: "OOM kill from a memory leak",
			wantDiscarded: []DiscardedSignal{
				{Signal: "node-1 CPU at 97%", Rationale: "the pegged CPU is the batch job's, and user-service is not CPU throttled"},
				{Signal: "batch-processor-x9k2", Rationale: "a co-tenant, not in the restart path"},
			},
			wantDeclared: true,
			wantContent:  "Full write-up above.",
		},
		{
			// Declared and empty is a real answer — "I ruled nothing out" —
			// and Declared is what separates it from having declared nothing.
			name:          "declared with nothing discarded",
			content:       "ROOT-CAUSE: the init container failed",
			wantRootCause: "the init container failed",
			wantDiscarded: nil,
			wantDeclared:  true,
			wantContent:   "",
		},
		{
			// A signal named with no reason is an incomplete entry, not an
			// absent one. It is recorded so a consumer requiring a rationale
			// can see that it is missing.
			name:          "discard with no separator keeps the signal and no rationale",
			content:       "DISCARDED: the CPU spike",
			wantDiscarded: []DiscardedSignal{{Signal: "the CPU spike", Rationale: ""}},
			wantDeclared:  true,
			wantContent:   "",
		},
		{
			// A field takes the last declaration; a list accumulates. The two
			// rules differ because a field and a list are different things.
			name:          "last root cause wins, discards accumulate",
			content:       "ROOT-CAUSE: first guess\nDISCARDED: a | one\nROOT-CAUSE: the refined claim\nDISCARDED: b | two",
			wantRootCause: "the refined claim",
			wantDiscarded: []DiscardedSignal{{Signal: "a", Rationale: "one"}, {Signal: "b", Rationale: "two"}},
			wantDeclared:  true,
			wantContent:   "",
		},
		{
			name:          "emphasis and case are tolerated",
			content:       "**Root-Cause:** `an OOM kill`\n*DISCARDED: cpu | unrelated*",
			wantRootCause: "an OOM kill",
			wantDiscarded: []DiscardedSignal{{Signal: "cpu", Rationale: "unrelated"}},
			wantDeclared:  true,
			wantContent:   "",
		},
		{
			// An empty marker line is plumbing with nothing behind it. It is
			// stripped, and it neither manufactures a commitment nor erases
			// one made earlier.
			name:          "empty markers declare nothing and erase nothing",
			content:       "ROOT-CAUSE: a real claim\nROOT-CAUSE:\nDISCARDED:\nprose",
			wantRootCause: "a real claim",
			wantDiscarded: nil,
			wantDeclared:  true,
			wantContent:   "prose",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, content := SplitConclusion(tc.content)
			if got.RootCause != tc.wantRootCause {
				t.Errorf("RootCause = %q, want %q", got.RootCause, tc.wantRootCause)
			}
			if len(got.Discarded) != len(tc.wantDiscarded) {
				t.Fatalf("Discarded = %+v, want %+v", got.Discarded, tc.wantDiscarded)
			}
			for i, want := range tc.wantDiscarded {
				if got.Discarded[i] != want {
					t.Errorf("Discarded[%d] = %+v, want %+v", i, got.Discarded[i], want)
				}
			}
			if got.Declared() != tc.wantDeclared {
				t.Errorf("Declared() = %v, want %v", got.Declared(), tc.wantDeclared)
			}
			if content != tc.wantContent {
				t.Errorf("content = %q, want %q", content, tc.wantContent)
			}
		})
	}
}

// TestDeclared_SeparatesEmptyFromAbsent is the distinction the whole design
// rests on: an empty discard list under a declaration is an answer, and the
// same empty list with no declaration is an absence. A consumer that cannot
// separate them scores contract adoption instead of diagnostic accuracy.
func TestDeclared_SeparatesEmptyFromAbsent(t *testing.T) {
	declaredEmpty, _ := SplitConclusion("ROOT-CAUSE: an OOM kill")
	if !declaredEmpty.Declared() || len(declaredEmpty.Discarded) != 0 {
		t.Fatalf("declared-and-empty = %+v, want Declared with no discards", declaredEmpty)
	}

	absent, _ := SplitConclusion("the pods are restarting")
	if absent.Declared() || len(absent.Discarded) != 0 {
		t.Fatalf("absent = %+v, want not Declared with no discards", absent)
	}

	// Both carry an empty list. Only Declared tells them apart.
	if len(declaredEmpty.Discarded) != len(absent.Discarded) {
		t.Fatal("the two cases differ in their list length; the test no longer pins what it claims to")
	}
}

// TestRun_ConclusionReachesTheSessionAndNotTheOperator pins both halves at
// once: the declaration is recorded, and its markers are joe's plumbing — they
// must not reach the operator, the history, or the intent probe's replay.
func TestRun_ConclusionReachesTheSessionAndNotTheOperator(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "user-service is being OOM killed by a memory leak; the node CPU is the batch job's.\n" +
			"ROOT-CAUSE: a memory leak in user-service exhausting its memory limit\n" +
			"DISCARDED: node-1 CPU at 97% | it is the batch workload's, and user-service shows no CPU throttling\n" +
			"TURN-KIND: answer"},
		probeDone(),
	}}
	agent, session := newKindAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "user-service keeps restarting")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := session.TerminalConclusion()
	if !got.Declared() {
		t.Fatal("Declared() = false, want true — the model emitted the markers")
	}
	if got.RootCause != "a memory leak in user-service exhausting its memory limit" {
		t.Errorf("RootCause = %q", got.RootCause)
	}
	if len(got.Discarded) != 1 || got.Discarded[0].Signal != "node-1 CPU at 97%" {
		t.Fatalf("Discarded = %+v", got.Discarded)
	}
	if got.Discarded[0].Rationale == "" {
		t.Error("Rationale is empty; the model stated one")
	}

	for _, marker := range []string{"ROOT-CAUSE", "DISCARDED", "TURN-KIND"} {
		if strings.Contains(answer, marker) {
			t.Errorf("answer leaked %s: %q", marker, answer)
		}
		for _, msg := range session.Messages {
			if strings.Contains(msg.Content, marker) {
				t.Errorf("history leaked %s: %q", marker, msg.Content)
			}
		}
	}
}

// TestRun_UndeclaredConclusionStaysAbsent is decision 2 of the order: an answer
// carrying no declaration is unassessable, never a wrong diagnosis. joe's half
// of that is refusing to invent one.
func TestRun_UndeclaredConclusionStaysAbsent(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "the readiness probe is misconfigured\nTURN-KIND: answer"},
		probeDone(),
	}}
	agent, session := newKindAgent(t, m)
	if _, err := agent.Run(context.Background(), session, "why is checkout down?"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := session.TerminalConclusion()
	if got.Declared() {
		t.Error("Declared() = true, want false — nothing was declared")
	}
	if got.RootCause != "" {
		t.Errorf("RootCause = %q, want empty — joe must not infer one from the prose", got.RootCause)
	}
}

// TestPrompt_InstructsTheDeclaration. The parse is worthless against a model
// that was never told to declare, and the clause is the only thing that tells
// it. This pins the marker spellings the parser matches.
func TestPrompt_InstructsTheDeclaration(t *testing.T) {
	for _, want := range []string{"ROOT-CAUSE:", "DISCARDED:", "DIAGNOSTIC CONCLUSION"} {
		if !strings.Contains(prompts.TaskSystem, want) {
			t.Errorf("TaskSystem does not mention %q", want)
		}
	}
}
