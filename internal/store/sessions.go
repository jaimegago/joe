package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SessionRepository defines operations on sessions.
type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	End(ctx context.Context, id string, summary string, metadata json.RawMessage) error
	AddMessage(ctx context.Context, msg *SessionMessage) error
	GetMessages(ctx context.Context, sessionID string) ([]*SessionMessage, error)
	ListRecent(ctx context.Context, limit int) ([]*Session, error)
}

type sqlSessionRepository struct {
	db *sql.DB
}

func (r *sqlSessionRepository) Create(ctx context.Context, session *Session) error {
	query := `INSERT INTO sessions (id, started_at) VALUES (?, ?)`
	session.StartedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, session.ID, session.StartedAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *sqlSessionRepository) Get(ctx context.Context, id string) (*Session, error) {
	query := `SELECT id, started_at, ended_at, summary, metadata FROM sessions WHERE id = ?`
	var s Session
	var endedAt sql.NullString
	var summary, metadata sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&s.ID, &s.StartedAt, &endedAt, &summary, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}

	if endedAt.Valid {
		t, _ := time.Parse(time.RFC3339, endedAt.String)
		s.EndedAt = &t
	}
	if summary.Valid {
		s.Summary = summary.String
	}
	if metadata.Valid {
		s.Metadata = json.RawMessage(metadata.String)
	}

	return &s, nil
}

func (r *sqlSessionRepository) End(ctx context.Context, id string, summary string, metadata json.RawMessage) error {
	query := `UPDATE sessions SET ended_at = ?, summary = ?, metadata = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, time.Now(), summary, metadata, id)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

func (r *sqlSessionRepository) AddMessage(ctx context.Context, msg *SessionMessage) error {
	query := `
		INSERT INTO session_messages (session_id, role, content, tool_name, tool_args, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	msg.CreatedAt = time.Now()
	result, err := r.db.ExecContext(ctx, query,
		msg.SessionID, msg.Role, msg.Content, msg.ToolName, msg.ToolArgs, msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	msg.ID, _ = result.LastInsertId()
	return nil
}

func (r *sqlSessionRepository) GetMessages(ctx context.Context, sessionID string) ([]*SessionMessage, error) {
	query := `
		SELECT id, session_id, role, content, tool_name, tool_args, created_at
		FROM session_messages WHERE session_id = ? ORDER BY id
	`
	rows, err := r.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []*SessionMessage
	for rows.Next() {
		var m SessionMessage
		var toolName, toolArgs sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &toolName, &toolArgs, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if toolName.Valid {
			m.ToolName = toolName.String
		}
		if toolArgs.Valid {
			m.ToolArgs = json.RawMessage(toolArgs.String)
		}
		messages = append(messages, &m)
	}

	return messages, rows.Err()
}

func (r *sqlSessionRepository) ListRecent(ctx context.Context, limit int) ([]*Session, error) {
	query := `
		SELECT id, started_at, ended_at, summary, metadata
		FROM sessions ORDER BY started_at DESC LIMIT ?
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		var endedAt, summary, metadata sql.NullString
		if err := rows.Scan(&s.ID, &s.StartedAt, &endedAt, &summary, &metadata); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		if endedAt.Valid {
			t, _ := time.Parse(time.RFC3339, endedAt.String)
			s.EndedAt = &t
		}
		if summary.Valid {
			s.Summary = summary.String
		}
		if metadata.Valid {
			s.Metadata = json.RawMessage(metadata.String)
		}
		sessions = append(sessions, &s)
	}

	return sessions, rows.Err()
}
