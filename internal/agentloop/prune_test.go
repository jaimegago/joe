package agentloop

import (
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

// tok builds a message whose content is exactly n estimated tokens
// (content length 4*n, so ceil(4n/4)=n). role/extra let callers shape
// genuine-user vs tool-result vs tool-call messages.
func userMsg(tag string, n int) llm.Message {
	return llm.Message{Role: "user", Content: tag + strings.Repeat("x", 4*n-len(tag))}
}
func asstMsg(tag string, n int) llm.Message {
	return llm.Message{Role: "assistant", Content: tag + strings.Repeat("x", 4*n-len(tag))}
}
func toolCallMsg(tag string, n int) llm.Message {
	return llm.Message{Role: "assistant", Content: tag + strings.Repeat("x", 4*n-len(tag)), ToolCalls: []llm.ToolCall{{ID: tag, Name: "t"}}}
}
func toolResultMsg(tag string, n int) llm.Message {
	return llm.Message{Role: "user", Content: tag + strings.Repeat("x", 4*n-len(tag)), ToolResultID: tag, ToolName: "t"}
}

func containsTag(msgs []llm.Message, tag string) bool {
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, tag) {
			return true
		}
	}
	return false
}

// TestPrune_TokenBudget_DropsOldestUntilUnderBudget asserts oldest-first
// dropping that ends under the budget, with the recent user message kept.
func TestPrune_TokenBudget_DropsOldestUntilUnderBudget(t *testing.T) {
	s := NewSession(nil)
	s.TokenBudget = 25
	s.AddMessages(context.Background(), []llm.Message{
		userMsg("m0", 10), asstMsg("m1", 10), userMsg("m2", 10), asstMsg("m3", 10), userMsg("m4", 10),
	})
	if got := EstimateMessagesTokens(s.Messages); got > 25 {
		t.Errorf("after prune estimated tokens = %d, want <= 25", got)
	}
	if containsTag(s.Messages, "m0") || containsTag(s.Messages, "m1") || containsTag(s.Messages, "m2") {
		t.Errorf("oldest messages survived pruning: %v", tags(s.Messages))
	}
	if !containsTag(s.Messages, "m4") {
		t.Error("most recent user message m4 was dropped")
	}
	if !s.HistoryTrimmed() || s.MessagesDropped() != 3 {
		t.Errorf("trimmed=%v dropped=%d, want true/3", s.HistoryTrimmed(), s.MessagesDropped())
	}
}

// TestPrune_MostRecentUserSurvivesOversized asserts the most recent genuine
// user message is kept even when it alone exceeds the budget.
func TestPrune_MostRecentUserSurvivesOversized(t *testing.T) {
	s := NewSession(nil)
	s.TokenBudget = 20
	s.AddMessages(context.Background(), []llm.Message{
		asstMsg("a0", 50), userMsg("big", 100),
	})
	if len(s.Messages) != 1 || !containsTag(s.Messages, "big") {
		t.Fatalf("kept %v, want only the oversized recent user message", tags(s.Messages))
	}
	if EstimateMessagesTokens(s.Messages) <= s.TokenBudget {
		t.Error("test setup invalid: surviving message should exceed budget to prove the guard")
	}
}

// TestPrune_ToolCallResultPairNeverSplit constructs a list where naive
// oldest-first slicing to fit the budget would leave a tool-result as the
// first kept message (orphaned from its dropped tool-call). The pair must be
// dropped together.
func TestPrune_ToolCallResultPairNeverSplit(t *testing.T) {
	s := NewSession(nil)
	s.TokenBudget = 12
	s.AddMessages(context.Background(), []llm.Message{
		userMsg("u0", 5), toolCallMsg("a1", 5), toolResultMsg("r1", 5), userMsg("u2", 5),
	})
	// Hard constraint: the kept slice must never begin with a tool-result.
	if len(s.Messages) > 0 && s.Messages[0].ToolResultID != "" {
		t.Errorf("kept slice begins with an orphaned tool-result: %v", tags(s.Messages))
	}
	// The tool-call and its result must share fate — both gone here.
	if containsTag(s.Messages, "r1") {
		t.Errorf("tool-result r1 kept; its call a1 was dropped (orphan): %v", tags(s.Messages))
	}
	if containsTag(s.Messages, "a1") {
		t.Errorf("tool-call a1 kept but expected dropped with its result: %v", tags(s.Messages))
	}
	if !containsTag(s.Messages, "u2") {
		t.Error("most recent user message u2 dropped")
	}
}

// TestPrune_CountBackstopAppliesAfterTokenBudget asserts the MaxMessages cap
// still trims when the token budget alone would not (many tiny messages).
func TestPrune_CountBackstopAppliesAfterTokenBudget(t *testing.T) {
	s := NewSession(nil)
	s.TokenBudget = 100000 // large: token pruning never triggers
	s.MaxMessages = 5
	msgs := make([]llm.Message, 10)
	for i := range msgs {
		msgs[i] = userMsg(string(rune('a'+i)), 1)
	}
	s.AddMessages(context.Background(), msgs)
	if len(s.Messages) != 5 {
		t.Errorf("count backstop kept %d messages, want 5", len(s.Messages))
	}
	if !containsTag(s.Messages, "j") { // the 10th (index 9) tag is 'j'
		t.Error("most recent message dropped by count backstop")
	}
	if !s.HistoryTrimmed() || s.MessagesDropped() != 5 {
		t.Errorf("trimmed=%v dropped=%d, want true/5", s.HistoryTrimmed(), s.MessagesDropped())
	}
}

// TestPrune_NoPruningLeavesFlagsClear asserts a within-budget session reports
// nothing dropped.
func TestPrune_NoPruningLeavesFlagsClear(t *testing.T) {
	s := NewSession(nil)
	s.TokenBudget = 100000
	s.MaxMessages = 100
	s.AddMessages(context.Background(), []llm.Message{userMsg("u0", 3), asstMsg("a0", 3)})
	if s.HistoryTrimmed() || s.MessagesDropped() != 0 {
		t.Errorf("trimmed=%v dropped=%d, want false/0", s.HistoryTrimmed(), s.MessagesDropped())
	}
}

func tags(msgs []llm.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		if len(m.Content) >= 2 {
			out[i] = m.Content[:2]
		} else {
			out[i] = m.Content
		}
	}
	return out
}
