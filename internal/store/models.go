package store

import (
	"encoding/json"
	"time"
)

// Component represents a registered infrastructure source.
type Component struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Config     json.RawMessage `json:"config"`
	Status     string          `json:"status"`
	LastSyncAt *time.Time      `json:"last_sync_at,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// Session represents a conversation session.
type Session struct {
	ID           string          `json:"id"`
	StartedAt    time.Time       `json:"started_at"`
	EndedAt      *time.Time      `json:"ended_at,omitempty"`
	Summary      string          `json:"summary,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	MessageCount int             `json:"message_count"`
}

// SessionMessage represents a message in a session.
type SessionMessage struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolArgs  json.RawMessage `json:"tool_args,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}
