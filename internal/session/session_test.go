package session_test

import (
	"testing"
	"time"

	. "github.com/jaimegago/joe/internal/session"
)

func TestManager_CreateAndGetSession(t *testing.T) {
	mgr := NewManager()
	id := "test-session"
	s := mgr.Create(id)
	if s == nil {
		t.Fatal("Create returned nil")
	}
	if s.ID != id {
		t.Errorf("Session ID = %s, want %s", s.ID, id)
	}
	if got := mgr.Get(id); got != s {
		t.Errorf("Get did not return created session")
	}
}

func TestManager_DeleteSession(t *testing.T) {
	mgr := NewManager()
	id := "delete-me"
	mgr.Create(id)
	mgr.Delete(id)
	if got := mgr.Get(id); got != nil {
		t.Errorf("Session not deleted")
	}
}

func TestSession_AddMessage(t *testing.T) {
	s := &Session{}
	s.AddMessage("user", "hello")
	if len(s.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(s.Messages))
	}
	msg := s.Messages[0]
	if msg.Role != "user" || msg.Content != "hello" {
		t.Errorf("Message = %+v, want user/hello", msg)
	}
}

func TestManager_MultipleSessions(t *testing.T) {
	mgr := NewManager()
	ids := []string{"a", "b", "c"}
	for _, id := range ids {
		mgr.Create(id)
	}
	for _, id := range ids {
		if mgr.Get(id) == nil {
			t.Errorf("Session %s not found", id)
		}
	}
}

func TestSession_StartedAt(t *testing.T) {
	mgr := NewManager()
	s := mgr.Create("timing")
	if time.Since(s.StartedAt) > time.Second {
		t.Errorf("StartedAt too old: %v", s.StartedAt)
	}
}

func TestSession_ContextIsolation(t *testing.T) {
	mgr := NewManager()
	s1 := mgr.Create("s1")
	s2 := mgr.Create("s2")
	s1.Context["foo"] = 42
	if _, ok := s2.Context["foo"]; ok {
		t.Error("Context leaked between sessions")
	}
}

func TestSession_AddMessage_Appends(t *testing.T) {
	s := &Session{}
	msgs := []struct {
		role, content string
	}{
		{"user", "hi"},
		{"assistant", "hello"},
	}
	for _, m := range msgs {
		s.AddMessage(m.role, m.content)
	}
	if len(s.Messages) != len(msgs) {
		t.Fatalf("Expected %d messages, got %d", len(msgs), len(s.Messages))
	}
	for i, m := range msgs {
		if s.Messages[i].Role != m.role || s.Messages[i].Content != m.content {
			t.Errorf("Message[%d] = %+v, want %s/%s", i, s.Messages[i], m.role, m.content)
		}
	}
}

func TestManager_Get_NotFound(t *testing.T) {
	mgr := NewManager()
	if got := mgr.Get("nope"); got != nil {
		t.Error("Expected nil for missing session")
	}
}

func TestSession_ZeroValue(t *testing.T) {
	var s Session
	if s.ID != "" {
		t.Errorf("Zero Session ID = %q, want empty", s.ID)
	}
	if s.Messages != nil && len(s.Messages) != 0 {
		t.Errorf("Zero Session Messages should be nil or empty, got: %#v", s.Messages)
	}
	if s.Context != nil {
		t.Error("Zero Session Context should be nil")
	}
}
