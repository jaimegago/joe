package store

import (
	"encoding/json"
	"time"
)

// Source represents a registered infrastructure source.
type Source struct {
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
	ID        string          `json:"id"`
	StartedAt time.Time       `json:"started_at"`
	EndedAt   *time.Time      `json:"ended_at,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
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

// Clarification represents a question from Core Agent to humans.
type Clarification struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Context         json.RawMessage `json:"context"`
	Question        string          `json:"question"`
	Options         []string        `json:"options,omitempty"`
	Status          string          `json:"status"`
	Answer          string          `json:"answer,omitempty"`
	AnsweredBy      string          `json:"answered_by,omitempty"`
	AnsweredAt      *time.Time      `json:"answered_at,omitempty"`
	GraphOperations json.RawMessage `json:"graph_operations,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	NotifiedAt      *time.Time      `json:"notified_at,omitempty"`
}

// ClarificationType constants.
const (
	ClarificationNewService       = "new_service"
	ClarificationEdgeConfirm      = "edge_confirm"
	ClarificationAmbiguousJoeFile = "ambiguous_joe_file"
	ClarificationNewSource        = "new_source"
	ClarificationServicePurpose   = "service_purpose"
)

// ClarificationStatus constants.
const (
	ClarificationPending   = "pending"
	ClarificationAnswered  = "answered"
	ClarificationDismissed = "dismissed"
)

// JoeFileCache represents cached .joe/ file parsing.
type JoeFileCache struct {
	FilePath    string          `json:"file_path"`
	ContentHash string          `json:"content_hash"`
	ParsedData  json.RawMessage `json:"parsed_data"`
	ParsedAt    time.Time       `json:"parsed_at"`
}

// OnboardingFact represents user-provided context.
type OnboardingFact struct {
	ID        int64     `json:"id"`
	FactType  string    `json:"fact_type"`
	Subject   string    `json:"subject"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	SourceID  string    `json:"source_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
