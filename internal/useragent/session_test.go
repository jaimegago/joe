package useragent

import (
	"context"
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

func TestNewSession(t *testing.T) {
	session := NewSession(nil)

	if session == nil {
		t.Fatal("NewSession(nil) returned nil")
	}

	if session.Messages == nil {
		t.Fatal("NewSession(nil) Messages is nil")
	}

	if len(session.Messages) != 0 {
		t.Errorf("NewSession(nil) has %d messages, want 0", len(session.Messages))
	}
}

func TestSession_AddMessage(t *testing.T) {
	session := NewSession(nil)

	msg := llm.Message{
		Role:    "user",
		Content: "Hello",
	}

	session.AddMessage(context.Background(), msg)

	if len(session.Messages) != 1 {
		t.Errorf("AddMessage() resulted in %d messages, want 1", len(session.Messages))
	}

	if session.Messages[0].Role != "user" {
		t.Errorf("Message role = %s, want user", session.Messages[0].Role)
	}

	if session.Messages[0].Content != "Hello" {
		t.Errorf("Message content = %s, want Hello", session.Messages[0].Content)
	}
}

func TestSession_AddMessages(t *testing.T) {
	session := NewSession(nil)

	messages := []llm.Message{
		{Role: "user", Content: "Message 1"},
		{Role: "assistant", Content: "Message 2"},
		{Role: "user", Content: "Message 3"},
	}

	session.AddMessages(context.Background(), messages)

	if len(session.Messages) != 3 {
		t.Errorf("AddMessages() resulted in %d messages, want 3", len(session.Messages))
	}

	for i, expected := range messages {
		if session.Messages[i].Role != expected.Role {
			t.Errorf("Message[%d] role = %s, want %s", i, session.Messages[i].Role, expected.Role)
		}
		if session.Messages[i].Content != expected.Content {
			t.Errorf("Message[%d] content = %s, want %s", i, session.Messages[i].Content, expected.Content)
		}
	}
}

func TestSession_AddMessages_Empty(t *testing.T) {
	session := NewSession(nil)
	session.AddMessages(context.Background(), []llm.Message{})

	if len(session.Messages) != 0 {
		t.Errorf("AddMessages() with empty slice resulted in %d messages, want 0", len(session.Messages))
	}
}

func TestSession_AddMultipleTimes(t *testing.T) {
	session := NewSession(nil)

	session.AddMessage(context.Background(), llm.Message{Role: "user", Content: "First"})
	session.AddMessage(context.Background(), llm.Message{Role: "assistant", Content: "Second"})
	session.AddMessages(context.Background(), []llm.Message{
		{Role: "user", Content: "Third"},
		{Role: "assistant", Content: "Fourth"},
	})

	if len(session.Messages) != 4 {
		t.Errorf("Session has %d messages, want 4", len(session.Messages))
	}

	expected := []string{"First", "Second", "Third", "Fourth"}
	for i, exp := range expected {
		if session.Messages[i].Content != exp {
			t.Errorf("Message[%d] content = %s, want %s", i, session.Messages[i].Content, exp)
		}
	}
}

func TestSession_Close(t *testing.T) {
	session := NewSession(nil)
	// Close should not panic
	session.Close()
}

func TestSession_AddMessage_Pruning(t *testing.T) {
	session := NewSession(nil)
	session.MaxMessages = 20

	// Add enough to trigger pruning (>20)
	for i := 0; i < 25; i++ {
		session.AddMessage(context.Background(), llm.Message{Role: "user", Content: fmt.Sprintf("msg%d", i)})
	}

	// Sliding window: keeps last 20 messages
	if len(session.Messages) != 20 {
		t.Errorf("session has %d messages after pruning, want 20", len(session.Messages))
	}
	// The most recent messages should be kept
	if session.Messages[len(session.Messages)-1].Content != "msg24" {
		t.Errorf("last message = %q, want msg24", session.Messages[len(session.Messages)-1].Content)
	}
	// Oldest kept message should be msg5 (25-20=5)
	if session.Messages[0].Content != "msg5" {
		t.Errorf("first message = %q, want msg5", session.Messages[0].Content)
	}
}

func TestSession_AddMessage_PruningSmallMax(t *testing.T) {
	session := NewSession(nil)
	session.MaxMessages = 10

	for i := 0; i < 15; i++ {
		session.AddMessage(context.Background(), llm.Message{Role: "user", Content: fmt.Sprintf("msg%d", i)})
	}

	// Sliding window: keeps last 10 messages
	if len(session.Messages) != 10 {
		t.Errorf("session has %d messages, want 10", len(session.Messages))
	}
	if session.Messages[0].Content != "msg5" {
		t.Errorf("first message = %q, want msg5", session.Messages[0].Content)
	}
}

func TestSession_AddMessages_Pruning(t *testing.T) {
	session := NewSession(nil)
	session.MaxMessages = 10

	// Add 5 initial messages
	for i := 0; i < 5; i++ {
		session.AddMessage(context.Background(), llm.Message{Role: "user", Content: fmt.Sprintf("msg%d", i)})
	}

	// Bulk-add 8 more (total 13, exceeds 10)
	bulk := make([]llm.Message, 8)
	for i := 0; i < 8; i++ {
		bulk[i] = llm.Message{Role: "user", Content: fmt.Sprintf("bulk%d", i)}
	}
	session.AddMessages(context.Background(), bulk)

	// Sliding window: keeps last 10
	if len(session.Messages) != 10 {
		t.Errorf("session has %d messages after bulk add, want 10", len(session.Messages))
	}
	// Last message should be bulk7
	if session.Messages[len(session.Messages)-1].Content != "bulk7" {
		t.Errorf("last message = %q, want bulk7", session.Messages[len(session.Messages)-1].Content)
	}
}

func TestSession_Clear(t *testing.T) {
	session := NewSession(nil)

	// Add some messages
	session.AddMessage(context.Background(), llm.Message{Role: "user", Content: "Message 1"})
	session.AddMessage(context.Background(), llm.Message{Role: "assistant", Content: "Message 2"})

	if len(session.Messages) != 2 {
		t.Fatalf("Session has %d messages before clear, want 2", len(session.Messages))
	}

	// Clear the session
	session.Clear()

	if len(session.Messages) != 0 {
		t.Errorf("Clear() resulted in %d messages, want 0", len(session.Messages))
	}

	// Verify we can add messages after clearing
	session.AddMessage(context.Background(), llm.Message{Role: "user", Content: "New message"})

	if len(session.Messages) != 1 {
		t.Errorf("Session has %d messages after clear and add, want 1", len(session.Messages))
	}
}
