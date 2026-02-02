package agent

import (
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

func TestNewSession(t *testing.T) {
	session := NewSession()

	if session == nil {
		t.Fatal("NewSession() returned nil")
	}

	if session.Messages == nil {
		t.Fatal("NewSession() Messages is nil")
	}

	if len(session.Messages) != 0 {
		t.Errorf("NewSession() has %d messages, want 0", len(session.Messages))
	}
}

func TestSession_AddMessage(t *testing.T) {
	session := NewSession()

	msg := llm.Message{
		Role:    "user",
		Content: "Hello",
	}

	session.AddMessage(msg)

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
	session := NewSession()

	messages := []llm.Message{
		{Role: "user", Content: "Message 1"},
		{Role: "assistant", Content: "Message 2"},
		{Role: "user", Content: "Message 3"},
	}

	session.AddMessages(messages)

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
	session := NewSession()
	session.AddMessages([]llm.Message{})

	if len(session.Messages) != 0 {
		t.Errorf("AddMessages() with empty slice resulted in %d messages, want 0", len(session.Messages))
	}
}

func TestSession_AddMultipleTimes(t *testing.T) {
	session := NewSession()

	session.AddMessage(llm.Message{Role: "user", Content: "First"})
	session.AddMessage(llm.Message{Role: "assistant", Content: "Second"})
	session.AddMessages([]llm.Message{
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

func TestSession_Clear(t *testing.T) {
	session := NewSession()

	// Add some messages
	session.AddMessage(llm.Message{Role: "user", Content: "Message 1"})
	session.AddMessage(llm.Message{Role: "assistant", Content: "Message 2"})

	if len(session.Messages) != 2 {
		t.Fatalf("Session has %d messages before clear, want 2", len(session.Messages))
	}

	// Clear the session
	session.Clear()

	if len(session.Messages) != 0 {
		t.Errorf("Clear() resulted in %d messages, want 0", len(session.Messages))
	}

	// Verify we can add messages after clearing
	session.AddMessage(llm.Message{Role: "user", Content: "New message"})

	if len(session.Messages) != 1 {
		t.Errorf("Session has %d messages after clear and add, want 1", len(session.Messages))
	}
}
