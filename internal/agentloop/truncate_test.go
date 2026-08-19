package agentloop

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/tools"
)

// markerCountRe extracts the elided character count from the truncation marker.
var markerCountRe = regexp.MustCompile(`\[\.\.\. (\d+) characters omitted`)

// TestTruncateContent_UnderCapUnchanged: content estimated within the cap is
// returned verbatim with didTruncate=false.
func TestTruncateContent_UnderCapUnchanged(t *testing.T) {
	content := strings.Repeat("x", 40) // ceil(40/4) = 10 tokens
	out, did := TruncateContent(content, 100)
	if did {
		t.Fatalf("under-cap content reported truncated")
	}
	if out != content {
		t.Fatalf("under-cap content mutated: got %d chars, want %d", len(out), len(content))
	}
}

// TestTruncateContent_OverCapHeadTailMarker: over-cap content ends under the
// cap, keeps a head and a tail, carries the marker with the correct character
// count, and splits roughly 60/40 head-heavy.
func TestTruncateContent_OverCapHeadTailMarker(t *testing.T) {
	// Distinguishable regions: 5000 'A' then 5000 'B'. With a small cap the
	// kept head stays within the A-run and the kept tail within the B-run.
	content := strings.Repeat("A", 5000) + strings.Repeat("B", 5000)
	const tokenCap = 200 // charCap 800
	out, did := TruncateContent(content, tokenCap)
	if !did {
		t.Fatal("over-cap content not truncated")
	}

	// Result fits under the cap (same chars/4 estimate the cap is denominated in).
	if got := EstimateMessageTokens(llm.Message{Content: out}); got > tokenCap {
		t.Errorf("truncated estimate %d tokens exceeds cap %d", got, tokenCap)
	}

	// Head and tail preserved and unmixed (head all 'A', tail all 'B').
	markerStart := strings.Index(out, "\n\n[... ")
	if markerStart < 0 {
		t.Fatalf("marker not found in output")
	}
	head := out[:markerStart]
	if head == "" || strings.Trim(head, "A") != "" {
		t.Errorf("head not a pure prefix of the 'A' run: %q", head)
	}
	markerEnd := strings.Index(out, " ...]\n\n")
	if markerEnd < 0 {
		t.Fatal("marker terminator not found")
	}
	tail := out[markerEnd+len(" ...]\n\n"):]
	if tail == "" || strings.Trim(tail, "B") != "" {
		t.Errorf("tail not a pure suffix of the 'B' run: %q", tail)
	}

	// Marker names the correct elided count: head + tail + elided == original.
	m := markerCountRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatal("elided-count marker not found")
	}
	elided, _ := strconv.Atoi(m[1])
	if got := len(head) + len(tail) + elided; got != len(content) {
		t.Errorf("head(%d)+tail(%d)+elided(%d) = %d, want %d",
			len(head), len(tail), elided, got, len(content))
	}

	// 60/40 split holds approximately.
	kept := len(head) + len(tail)
	ratio := float64(len(head)) / float64(kept)
	if ratio < 0.55 || ratio > 0.65 {
		t.Errorf("head ratio %.3f outside ~0.6 (head=%d kept=%d)", ratio, len(head), kept)
	}
}

// TestTruncationLimit_FloorApplies: when the budget fraction falls below the
// 2000-token floor, the floor is used; a generous budget uses the fraction;
// a non-positive budget disables truncation (limit 0).
func TestTruncationLimit_FloorApplies(t *testing.T) {
	// Tiny budget: 25% of 1000 = 250 < 2000 → floored to 2000.
	s := &Session{TokenBudget: 1000}
	if got := s.truncationLimit(toolResultBudgetFraction); got != minTruncationTokenFloor {
		t.Errorf("tiny-budget tool-result limit = %d, want floor %d", got, minTruncationTokenFloor)
	}
	// Generous budget: 25% of 100000 = 25000 > floor → fraction wins.
	s.TokenBudget = 100000
	if got := s.truncationLimit(toolResultBudgetFraction); got != 25000 {
		t.Errorf("generous tool-result limit = %d, want 25000", got)
	}
	if got := s.truncationLimit(userMessageBudgetFraction); got != 50000 {
		t.Errorf("generous user-message limit = %d, want 50000", got)
	}
	// No budget: truncation disabled.
	s.TokenBudget = 0
	if got := s.truncationLimit(toolResultBudgetFraction); got != 0 {
		t.Errorf("zero-budget limit = %d, want 0 (disabled)", got)
	}
}

// hugeResultTool returns a tool result whose JSON content is huge, to drive
// ingestion truncation of tool results. It registers under a list_components
// name so the default executor policy permits it and the real (large) result —
// not a safety-denied error string — reaches ingestion.
type hugeResultTool struct{ payload string }

func (t *hugeResultTool) Name() string        { return "list_components" }
func (t *hugeResultTool) Description() string { return "returns a large payload" }
func (t *hugeResultTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{Type: "object", Properties: map[string]llm.Property{}}
}
func (t *hugeResultTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return map[string]string{"data": t.payload}, nil
}

// newHugeResultAgent builds an agent whose single tool returns a large payload,
// with a mock LLM that calls the tool twice on the first turn then finishes.
func newHugeResultAgent(t *testing.T, payload string) (*Agent, *Session) {
	t.Helper()
	registry := tools.NewRegistry()
	registry.Register(&hugeResultTool{payload: payload})
	executor := tools.NewExecutor(registry, nil)
	mock := &mockLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "list_components", Args: map[string]any{}},
			{ID: "c2", Name: "list_components", Args: map[string]any{}},
		}},
		{Content: "done"},
	}}
	agent := NewAgent(mock, executor, registry, llm.StaticSystem("sys"))
	session := NewSession(nil)
	// Small budget so the floor governs: 2000-token cap → 8000 char cap.
	session.TokenBudget = 1000
	return agent, session
}

// TestIngestion_ToolResultsTruncated: two oversized tool results enter history
// truncated with the marker and the counter reports 2.
func TestIngestion_ToolResultsTruncated(t *testing.T) {
	payload := strings.Repeat("Z", 40000) // ~10000 tokens, well over the 2000 cap
	agent, session := newHugeResultAgent(t, payload)

	if _, err := agent.Run(context.Background(), session, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := session.ToolResultsTruncated(); got != 2 {
		t.Errorf("ToolResultsTruncated = %d, want 2", got)
	}
	// Each tool-result message in history carries the marker and is under cap.
	limit := session.truncationLimit(toolResultBudgetFraction)
	found := 0
	for _, msg := range session.Messages {
		if msg.ToolResultID == "" {
			continue
		}
		found++
		if !strings.Contains(msg.Content, "characters omitted to fit the context budget") {
			t.Errorf("tool-result %s missing truncation marker", msg.ToolResultID)
		}
		if got := EstimateMessageTokens(msg); got > limit {
			t.Errorf("tool-result %s estimate %d exceeds cap %d", msg.ToolResultID, got, limit)
		}
	}
	if found != 2 {
		t.Errorf("found %d tool-result messages, want 2", found)
	}
}

// TestIngestion_UserMessageTruncatedAndProtected: an oversized user message
// enters truncated, the turn proceeds, the counter flips, and the most-recent
// user-message protection still finds the truncated form.
func TestIngestion_UserMessageTruncatedAndProtected(t *testing.T) {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, nil)
	mock := &mockLLM{responses: []*llm.ChatResponse{{Content: "done"}}}
	agent := NewAgent(mock, executor, registry, llm.StaticSystem("sys"))
	session := NewSession(nil)
	session.TokenBudget = 1000 // 50% floor → 2000-token cap → 8000 char cap

	huge := strings.Repeat("Q", 40000)
	answer, err := agent.Run(context.Background(), session, huge)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "done" {
		t.Fatalf("turn did not proceed: answer = %q", answer)
	}
	if !session.UserMessageTruncated() {
		t.Error("UserMessageTruncated = false, want true")
	}

	// The genuine user message (index 0) is the truncated form and is still
	// recognised as the most-recent user message to protect.
	if idx := session.lastUserMessageIndex(); idx != 0 {
		t.Errorf("lastUserMessageIndex = %d, want 0", idx)
	}
	user := session.Messages[0]
	if user.Role != "user" || user.ToolResultID != "" {
		t.Fatalf("message 0 is not a genuine user message: %+v", user)
	}
	if !strings.Contains(user.Content, "characters omitted to fit the context budget") {
		t.Error("user message missing truncation marker")
	}
	if len(user.Content) >= len(huge) {
		t.Errorf("user message not shortened: %d >= %d", len(user.Content), len(huge))
	}
}

// TestIngestion_NoReTruncationInHistory: a message already truncated in history
// is not re-truncated on a later iteration (truncation is ingestion-only).
func TestIngestion_NoReTruncationInHistory(t *testing.T) {
	payload := strings.Repeat("Z", 40000)
	registry := tools.NewRegistry()
	registry.Register(&hugeResultTool{payload: payload})
	executor := tools.NewExecutor(registry, nil)
	// Two tool-calling turns then a final answer: the first turn's truncated
	// result sits in history across the second iteration.
	mock := &mockLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "list_components", Args: map[string]any{}}}},
		{ToolCalls: []llm.ToolCall{{ID: "c2", Name: "list_components", Args: map[string]any{}}}},
		{Content: "done"},
	}}
	agent := NewAgent(mock, executor, registry, llm.StaticSystem("sys"))
	session := NewSession(nil)
	session.TokenBudget = 1000

	if _, err := agent.Run(context.Background(), session, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Exactly two results truncated (one per turn) — the first is not counted
	// again when the second iteration runs.
	if got := session.ToolResultsTruncated(); got != 2 {
		t.Errorf("ToolResultsTruncated = %d, want 2 (no re-truncation)", got)
	}
	// The first truncated result still carries exactly one marker (not nested).
	for _, msg := range session.Messages {
		if msg.ToolResultID == "" {
			continue
		}
		if n := strings.Count(msg.Content, "characters omitted to fit the context budget"); n != 1 {
			t.Errorf("tool-result %s has %d markers, want 1", msg.ToolResultID, n)
		}
	}
}

// TestIngestion_CleanTurnReportsZero: a turn with no oversized messages leaves
// both counters at their zero values.
func TestIngestion_CleanTurnReportsZero(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(newEchoTool())
	executor := tools.NewExecutor(registry, nil)
	mock := &mockLLM{responses: []*llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Args: map[string]any{"message": "hi"}}}},
		{Content: "done"},
	}}
	agent := NewAgent(mock, executor, registry, llm.StaticSystem("sys"))
	session := NewSession(nil)
	session.TokenBudget = 100000

	if _, err := agent.Run(context.Background(), session, "small message"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if session.ToolResultsTruncated() != 0 {
		t.Errorf("ToolResultsTruncated = %d, want 0", session.ToolResultsTruncated())
	}
	if session.UserMessageTruncated() {
		t.Error("UserMessageTruncated = true, want false")
	}
}
