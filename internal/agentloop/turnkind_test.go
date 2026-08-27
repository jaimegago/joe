package agentloop

import (
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/tools"
)

// A turn that ends in words rather than an action is invisible to anything
// keyed on actions: the run evidence records which tools ran and nothing about
// what the prose was for. These tests pin the two things joe-pm
// threads/terminal-turn-kind.md ordered — a declared kind on every terminal
// turn, and one bounded gate keyed on it.

func newKindAgent(t *testing.T, m *mockLLM) (*Agent, *Session) {
	t.Helper()
	registry := tools.NewRegistry()
	registry.Register(newEchoTool())
	agent := NewAgent(m, tools.NewExecutor(registry, nil), registry, llm.StaticSystem("system"))
	session := NewSession(nil)
	t.Cleanup(session.Close)
	return agent, session
}

// probeDone is the reply the unfulfilled-tool-intent probe gets from a model
// that is genuinely finished. Every terminal turn in these tests is probed, so
// each one needs its DONE alongside.
func probeDone() *llm.ChatResponse { return &llm.ChatResponse{Content: "DONE"} }

// TestSplitTurnKind covers the parse in isolation: the vocabulary is closed at
// three, an absent or unrecognised marker is not a declaration, and the marker
// never survives into the prose.
func TestSplitTurnKind(t *testing.T) {
	cases := []struct {
		name         string
		content      string
		wantKind     TurnKind
		wantDeclared bool
		wantContent  string
	}{
		{
			name:         "answer declared on its own last line",
			content:      "the cause is a bad readiness probe\n\nTURN-KIND: answer",
			wantKind:     TurnKindAnswer,
			wantDeclared: true,
			wantContent:  "the cause is a bad readiness probe",
		},
		{
			name:         "question declared",
			content:      "which cluster do you mean?\nTURN-KIND: question",
			wantKind:     TurnKindQuestion,
			wantDeclared: true,
			wantContent:  "which cluster do you mean?",
		},
		{
			name:         "refusal declared",
			content:      "that would delete the volume\nTURN-KIND: refusal",
			wantKind:     TurnKindRefusal,
			wantDeclared: true,
			wantContent:  "that would delete the volume",
		},
		{
			// Models wrap a lone instructed line in the emphasis they use for
			// every other standalone line. Rejecting that would report a model
			// that declares as one that never does.
			name:         "emphasis and case are tolerated",
			content:      "done\n**Turn-Kind: QUESTION**",
			wantKind:     TurnKindQuestion,
			wantDeclared: true,
			wantContent:  "done",
		},
		{
			// Told to put a line last, models still append a pleasantry after
			// it. Anchoring strictly to the final line would drop the marker.
			name:         "marker is found above trailing prose",
			content:      "which namespace?\nTURN-KIND: question\n\nHappy to keep digging.",
			wantKind:     TurnKindQuestion,
			wantDeclared: true,
			wantContent:  "which namespace?\n\nHappy to keep digging.",
		},
		{
			name:         "no marker defaults to answer, undeclared",
			content:      "the cause is X",
			wantKind:     TurnKindAnswer,
			wantDeclared: false,
			wantContent:  "the cause is X",
		},
		{
			// The vocabulary is CLOSED. A fourth value is not a declaration,
			// and must not reach a consumer as one.
			name:         "value outside the vocabulary is not a declaration",
			content:      "hmm\nTURN-KIND: clarification",
			wantKind:     TurnKindAnswer,
			wantDeclared: false,
			wantContent:  "hmm\nTURN-KIND: clarification",
		},
		{
			name:         "empty content",
			content:      "",
			wantKind:     TurnKindAnswer,
			wantDeclared: false,
			wantContent:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, declared, content := SplitTurnKind(tc.content)
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", kind, tc.wantKind)
			}
			if declared != tc.wantDeclared {
				t.Errorf("declared = %v, want %v", declared, tc.wantDeclared)
			}
			if content != tc.wantContent {
				t.Errorf("content = %q, want %q", content, tc.wantContent)
			}
		})
	}
}

// TestParseTurnKind_VocabularyIsClosed pins the closure directly: exactly three
// values parse, and nothing else does.
func TestParseTurnKind_VocabularyIsClosed(t *testing.T) {
	for _, in := range []string{"answer", "question", "refusal", "ANSWER", " Question "} {
		if _, ok := ParseTurnKind(in); !ok {
			t.Errorf("ParseTurnKind(%q) rejected a value in the vocabulary", in)
		}
	}
	for _, in := range []string{"", "unknown", "undeclared", "clarification", "ask", "answers"} {
		if k, ok := ParseTurnKind(in); ok {
			t.Errorf("ParseTurnKind(%q) accepted %q; the vocabulary is closed at three", in, k)
		}
	}
}

// TestRun_TerminalTurnAlwaysCarriesAKind is the acceptance bar that every
// terminal turn carries one — the declared case and the defaulted case both.
// A kind emitted only for the interesting cases is a kind whose absence is
// ambiguous between "not that shape" and "not emitted".
func TestRun_TerminalTurnAlwaysCarriesAKind(t *testing.T) {
	t.Run("declared", func(t *testing.T) {
		m := &mockLLM{responses: []*llm.ChatResponse{
			{Content: "I will not drain that node.\nTURN-KIND: refusal"},
			probeDone(),
		}}
		agent, session := newKindAgent(t, m)
		answer, err := agent.Run(context.Background(), session, "drain node-1")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if session.TerminalTurnKind() != TurnKindRefusal {
			t.Errorf("kind = %q, want %q", session.TerminalTurnKind(), TurnKindRefusal)
		}
		if !session.TurnKindDeclared() {
			t.Error("TurnKindDeclared() = false, want true — the model emitted the marker")
		}
		// The marker is joe's plumbing and must not reach the operator.
		if strings.Contains(answer, "TURN-KIND") {
			t.Errorf("answer leaked the marker: %q", answer)
		}
		for _, msg := range session.Messages {
			if strings.Contains(msg.Content, "TURN-KIND") {
				t.Errorf("history leaked the marker: %q", msg.Content)
			}
		}
	})

	t.Run("undeclared still carries one", func(t *testing.T) {
		m := &mockLLM{responses: []*llm.ChatResponse{
			{Content: "the cause is a bad readiness probe"},
			probeDone(),
		}}
		agent, session := newKindAgent(t, m)
		if _, err := agent.Run(context.Background(), session, "why is checkout down?"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if session.TerminalTurnKind() != TurnKindAnswer {
			t.Errorf("kind = %q, want the %q default", session.TerminalTurnKind(), TurnKindAnswer)
		}
		if session.TurnKindDeclared() {
			t.Error("TurnKindDeclared() = true, want false — nothing was declared")
		}
	})
}

// TestGate_ZeroActionQuestionReenters is the invariant: joe does not return a
// `question` terminal turn on a session in which it has taken no actions. The
// model must be sent back to look first.
func TestGate_ZeroActionQuestionReenters(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		// Iteration 1: a question, having run nothing at all.
		{Content: "Which cluster should I look at?\nTURN-KIND: question"},
		// Its probe: genuinely finished, so the probe recovers no call and the
		// gate is what has to catch this.
		probeDone(),
		// The re-entered turn: the model looks.
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Args: map[string]any{"message": "hi"}}}},
		// Iteration 3: a real answer, grounded in the tool result.
		{Content: "the cause is X\nTURN-KIND: answer"},
		probeDone(),
	}}
	agent, session := newKindAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "why is checkout timing out?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "the cause is X" {
		t.Errorf("answer = %q, want the investigation to have continued to %q", answer, "the cause is X")
	}
	if session.ZeroActionQuestionGate() != ZeroActionQuestionGateHeld {
		t.Errorf("gate = %q, want %q", session.ZeroActionQuestionGate(), ZeroActionQuestionGateHeld)
	}
	if session.TerminalTurnKind() != TurnKindAnswer {
		t.Errorf("terminal kind = %q, want %q", session.TerminalTurnKind(), TurnKindAnswer)
	}

	// The re-entry must be visible to the model: its own question, then the
	// instruction. A re-entered turn that cannot see what it asked asks again.
	var sawQuestion, sawReentry bool
	for _, msg := range session.Messages {
		if msg.Role == "assistant" && msg.Content == "Which cluster should I look at?" {
			sawQuestion = true
		}
		if msg.Role == "user" && msg.Content == prompts.ZeroActionQuestionReentry {
			sawReentry = true
		}
	}
	if !sawQuestion {
		t.Error("the gated question was not preserved in history")
	}
	if !sawReentry {
		t.Error("the re-entry instruction was not appended to history")
	}
	// The tool actually ran.
	if session.ActionsTaken() != 1 {
		t.Errorf("ActionsTaken() = %d, want 1", session.ActionsTaken())
	}
}

// TestGate_FiresAtMostOncePerSession pins the bound. An unbounded re-entry gate
// is a hang, and a hang is a worse failure than the question it was preventing:
// the second zero-action question is RETURNED, and the fact that the gate fired
// and did not hold is recorded rather than silent.
func TestGate_FiresAtMostOncePerSession(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "Which cluster?\nTURN-KIND: question"},
		probeDone(),
		// Re-entered, and the model asks again without looking.
		{Content: "I still need to know which cluster.\nTURN-KIND: question"},
		probeDone(),
	}}
	agent, session := newKindAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "why is checkout timing out?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "I still need to know which cluster." {
		t.Errorf("answer = %q, want the second question returned as it stood", answer)
	}
	if session.TerminalTurnKind() != TurnKindQuestion {
		t.Errorf("terminal kind = %q, want %q", session.TerminalTurnKind(), TurnKindQuestion)
	}
	if session.ActionsTaken() != 0 {
		t.Errorf("ActionsTaken() = %d, want 0", session.ActionsTaken())
	}

	// The bound: exactly one re-entry. The mock holds four responses and would
	// error on a fifth, so a second firing surfaces as a Run error — but assert
	// the count directly rather than relying on that.
	if m.callCount != 4 {
		t.Errorf("callCount = %d, want 4 (two turns, each probed) — the gate fired more than once", m.callCount)
	}

	// Fired and did not hold, recorded.
	if session.ZeroActionQuestionGate() != ZeroActionQuestionGateNotHeld {
		t.Errorf("gate = %q, want %q — a gate that fired and did not hold must not be silent",
			session.ZeroActionQuestionGate(), ZeroActionQuestionGateNotHeld)
	}
}

// TestGate_DoesNotFireAfterAnAction is the other half of the invariant's scope:
// the gate keys on zero actions, so a question asked after joe has looked is a
// question joe is entitled to ask, and must reach the operator untouched.
func TestGate_DoesNotFireAfterAnAction(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Args: map[string]any{"message": "hi"}}}},
		// Having looked, the model asks something only the operator knows.
		{Content: "Both clusters host it — which one is the one you meant?\nTURN-KIND: question"},
		probeDone(),
	}}
	agent, session := newKindAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "why is checkout timing out?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "Both clusters host it — which one is the one you meant?" {
		t.Errorf("answer = %q, want the question returned untouched", answer)
	}
	if session.ZeroActionQuestionGate() != "" {
		t.Errorf("gate = %q, want it not to have fired", session.ZeroActionQuestionGate())
	}
	if session.TerminalTurnKind() != TurnKindQuestion {
		t.Errorf("terminal kind = %q, want %q", session.TerminalTurnKind(), TurnKindQuestion)
	}
}

// TestGate_DoesNotFireOnAnUndeclaredTurn pins the cost of the default. A model
// that emits no marker gets TurnKindAnswer, so the gate does not fire — a
// formatting slip must not spend an extra LLM round trip, and must not re-enter
// a loop on a turn nobody said was a question.
func TestGate_DoesNotFireOnAnUndeclaredTurn(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "Which cluster should I look at?"},
		probeDone(),
	}}
	agent, session := newKindAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "why is checkout timing out?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "Which cluster should I look at?" {
		t.Errorf("answer = %q, want the undeclared turn returned as it stood", answer)
	}
	if session.ZeroActionQuestionGate() != "" {
		t.Errorf("gate = %q, want it not to have fired on an undeclared turn", session.ZeroActionQuestionGate())
	}
	if session.TurnKindDeclared() {
		t.Error("TurnKindDeclared() = true, want false")
	}
}

// TestGate_ObserverSeesTheGatedTurn pins that the re-entry is not invisible.
// The gated turn is a real LLM call that was paid for; a run whose step count
// disagreed with its token spend would hide the question joe declined to
// return.
func TestGate_ObserverSeesTheGatedTurn(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "Which cluster?\nTURN-KIND: question"},
		probeDone(),
		{Content: "the cause is X\nTURN-KIND: answer"},
		probeDone(),
	}}
	registry := tools.NewRegistry()
	registry.Register(newEchoTool())
	obs := &SliceObserver{}
	agent := NewAgent(m, tools.NewExecutor(registry, nil), registry, llm.StaticSystem("system"), WithObserver(obs))
	session := NewSession(nil)
	t.Cleanup(session.Close)

	if _, err := agent.Run(context.Background(), session, "why is checkout timing out?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(obs.Steps) != 2 {
		t.Fatalf("observed %d steps, want 2 — the gated turn is a real iteration", len(obs.Steps))
	}
	if obs.Steps[0].LLMResponse.Content != "Which cluster?" {
		t.Errorf("step 1 content = %q, want the gated question with its marker stripped",
			obs.Steps[0].LLMResponse.Content)
	}
}
