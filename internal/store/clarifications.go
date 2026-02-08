package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ClarificationRepository defines operations on clarifications.
type ClarificationRepository interface {
	Create(ctx context.Context, c *Clarification) error
	Get(ctx context.Context, id string) (*Clarification, error)
	ListPending(ctx context.Context) ([]*Clarification, error)
	ListByStatus(ctx context.Context, status string) ([]*Clarification, error)
	Answer(ctx context.Context, id, answer, answeredBy string) error
	Dismiss(ctx context.Context, id string) error
	MarkNotified(ctx context.Context, id string) error
}

type sqlClarificationRepository struct {
	db *sql.DB
}

func (r *sqlClarificationRepository) Create(ctx context.Context, c *Clarification) error {
	query := `
		INSERT INTO clarifications (id, type, context, question, options, status, graph_operations, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	c.CreatedAt = time.Now()
	if c.Status == "" {
		c.Status = ClarificationPending
	}

	var options []byte
	if len(c.Options) > 0 {
		options, _ = json.Marshal(c.Options)
	}

	_, err := r.db.ExecContext(ctx, query,
		c.ID, c.Type, c.Context, c.Question, options,
		c.Status, c.GraphOperations, c.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert clarification: %w", err)
	}
	return nil
}

func (r *sqlClarificationRepository) Get(ctx context.Context, id string) (*Clarification, error) {
	query := `
		SELECT id, type, context, question, options, status, answer,
		       answered_by, answered_at, graph_operations, created_at, notified_at
		FROM clarifications WHERE id = ?
	`
	return r.scanOne(r.db.QueryRowContext(ctx, query, id))
}

func (r *sqlClarificationRepository) ListPending(ctx context.Context) ([]*Clarification, error) {
	return r.ListByStatus(ctx, ClarificationPending)
}

func (r *sqlClarificationRepository) ListByStatus(ctx context.Context, status string) ([]*Clarification, error) {
	query := `
		SELECT id, type, context, question, options, status, answer,
		       answered_by, answered_at, graph_operations, created_at, notified_at
		FROM clarifications
		WHERE status = ?
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("query clarifications: %w", err)
	}
	defer rows.Close()

	var clarifications []*Clarification
	for rows.Next() {
		var c Clarification
		var options, graphOps sql.NullString
		var answer, answeredBy sql.NullString
		var answeredAt, notifiedAt sql.NullString

		if err := rows.Scan(
			&c.ID, &c.Type, &c.Context, &c.Question, &options, &c.Status,
			&answer, &answeredBy, &answeredAt, &graphOps, &c.CreatedAt, &notifiedAt,
		); err != nil {
			return nil, fmt.Errorf("scan clarification: %w", err)
		}

		populateClarificationNullables(&c, options, graphOps, answer, answeredBy, answeredAt, notifiedAt)
		clarifications = append(clarifications, &c)
	}

	return clarifications, rows.Err()
}

func (r *sqlClarificationRepository) Answer(ctx context.Context, id, answer, answeredBy string) error {
	query := `
		UPDATE clarifications
		SET answer = ?, answered_by = ?, answered_at = ?, status = ?
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, answer, answeredBy, time.Now(), ClarificationAnswered, id)
	if err != nil {
		return fmt.Errorf("answer clarification: %w", err)
	}
	return nil
}

func (r *sqlClarificationRepository) Dismiss(ctx context.Context, id string) error {
	query := `UPDATE clarifications SET status = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, ClarificationDismissed, id)
	if err != nil {
		return fmt.Errorf("dismiss clarification: %w", err)
	}
	return nil
}

func (r *sqlClarificationRepository) MarkNotified(ctx context.Context, id string) error {
	query := `UPDATE clarifications SET notified_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("mark notified: %w", err)
	}
	return nil
}

func (r *sqlClarificationRepository) scanOne(row *sql.Row) (*Clarification, error) {
	var c Clarification
	var options, graphOps sql.NullString
	var answer, answeredBy sql.NullString
	var answeredAt, notifiedAt sql.NullString

	err := row.Scan(
		&c.ID, &c.Type, &c.Context, &c.Question, &options, &c.Status,
		&answer, &answeredBy, &answeredAt, &graphOps, &c.CreatedAt, &notifiedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query clarification: %w", err)
	}

	populateClarificationNullables(&c, options, graphOps, answer, answeredBy, answeredAt, notifiedAt)
	return &c, nil
}

func populateClarificationNullables(c *Clarification, options, graphOps, answer, answeredBy, answeredAt, notifiedAt sql.NullString) {
	if options.Valid {
		json.Unmarshal([]byte(options.String), &c.Options)
	}
	if graphOps.Valid {
		c.GraphOperations = json.RawMessage(graphOps.String)
	}
	if answer.Valid {
		c.Answer = answer.String
	}
	if answeredBy.Valid {
		c.AnsweredBy = answeredBy.String
	}
	if answeredAt.Valid {
		t, _ := time.Parse(time.RFC3339, answeredAt.String)
		c.AnsweredAt = &t
	}
	if notifiedAt.Valid {
		t, _ := time.Parse(time.RFC3339, notifiedAt.String)
		c.NotifiedAt = &t
	}
}
