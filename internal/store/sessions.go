package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/observability"
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
	db      *sql.DB
	metrics *observability.Metrics
}

func (r *sqlSessionRepository) Create(ctx context.Context, session *Session) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sessions.create", time.Since(start), err) }()

	query := `INSERT INTO sessions (id, started_at) VALUES (?, ?)`
	session.StartedAt = time.Now()
	_, err = r.db.ExecContext(ctx, query, session.ID, session.StartedAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *sqlSessionRepository) Get(ctx context.Context, id string) (session *Session, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sessions.get", time.Since(start), err) }()

	query := `SELECT id, started_at, ended_at, summary, metadata FROM sessions WHERE id = ?`
	var s Session
	var endedAt sql.NullString
	var summary, metadata sql.NullString

	err = r.db.QueryRowContext(ctx, query, id).Scan(
		&s.ID, &s.StartedAt, &endedAt, &summary, &metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}

	if endedAt.Valid {
		s.EndedAt = parseTimeOrWarn(endedAt.String, "sessions.ended_at")
	}
	if summary.Valid {
		s.Summary = summary.String
	}
	if metadata.Valid {
		s.Metadata = json.RawMessage(metadata.String)
	}

	return &s, nil
}

func (r *sqlSessionRepository) End(ctx context.Context, id string, summary string, metadata json.RawMessage) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sessions.end", time.Since(start), err) }()

	query := `UPDATE sessions SET ended_at = ?, summary = ?, metadata = ? WHERE id = ?`
	_, err = r.db.ExecContext(ctx, query, time.Now(), summary, metadata, id)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

func (r *sqlSessionRepository) AddMessage(ctx context.Context, msg *SessionMessage) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sessions.add_message", time.Since(start), err) }()

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

func (r *sqlSessionRepository) GetMessages(ctx context.Context, sessionID string) (messages []*SessionMessage, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sessions.get_messages", time.Since(start), err) }()

	query := `
		SELECT id, session_id, role, content, tool_name, tool_args, created_at
		FROM session_messages WHERE session_id = ? ORDER BY id
	`
	rows, err := r.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

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

func (r *sqlSessionRepository) ListRecent(ctx context.Context, limit int) (sessions []*Session, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sessions.list_recent", time.Since(start), err) }()

	query := `
		SELECT s.id, s.started_at, s.ended_at, s.summary, s.metadata,
		       COUNT(m.id) AS message_count
		FROM sessions s
		LEFT JOIN session_messages m ON m.session_id = s.id
		GROUP BY s.id
		ORDER BY s.started_at DESC LIMIT ?
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s Session
		var endedAt, summary, metadata sql.NullString
		if err := rows.Scan(&s.ID, &s.StartedAt, &endedAt, &summary, &metadata, &s.MessageCount); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		if endedAt.Valid {
			s.EndedAt = parseTimeOrWarn(endedAt.String, "sessions.ended_at")
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
