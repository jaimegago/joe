package session

import (
	"time"

	"github.com/jaimegago/joe/internal/llm"
)

// Session represents a conversation session
type Session struct {
	ID        string
	StartedAt time.Time
	Messages  []llm.Message
	Context   map[string]any
}

// Manager manages conversation sessions
type Manager struct {
	sessions map[string]*Session
}

// NewManager creates a new session manager
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session
func (m *Manager) Create(id string) *Session {
	session := &Session{
		ID:        id,
		StartedAt: time.Now(),
		Messages:  []llm.Message{},
		Context:   make(map[string]any),
	}
	m.sessions[id] = session
	return session
}

// Get retrieves a session by ID
func (m *Manager) Get(id string) *Session {
	return m.sessions[id]
}

// Delete removes a session
func (m *Manager) Delete(id string) {
	delete(m.sessions, id)
}

// AddMessage adds a message to the session
func (s *Session) AddMessage(role, content string) {
	s.Messages = append(s.Messages, llm.Message{
		Role:    role,
		Content: content,
	})
}
