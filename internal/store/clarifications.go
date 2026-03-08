package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/observability"
)

// ErrAlreadyAnswered is returned by Answer when the clarification has already
// been answered or dismissed by another joecored instance.
var ErrAlreadyAnswered = errors.New("clarification: already answered")

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
	db      *sql.DB
	driver  string
	metrics *observability.Metrics
}

func (r *sqlClarificationRepository) Create(ctx context.Context, c *Clarification) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "clarifications.create", time.Since(start), err) }()

	query := Rebind(r.driver, `
		INSERT INTO clarifications (id, type, context, question, options, status, graph_operations, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	c.CreatedAt = time.Now()
	if c.Status == "" {
		c.Status = ClarificationPending
	}

	var options []byte
	if len(c.Options) > 0 {
		var marshalErr error
		options, marshalErr = json.Marshal(c.Options)
		if marshalErr != nil {
			slog.Warn("failed to marshal clarification options", "clarification_id", c.ID, "error", marshalErr)
		}
	}

	_, err = r.db.ExecContext(ctx, query,
		c.ID, c.Type, c.Context, c.Question, options,
		c.Status, c.GraphOperations, c.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert clarification: %w", err)
	}
	return nil
}

func (r *sqlClarificationRepository) Get(ctx context.Context, id string) (clarification *Clarification, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "clarifications.get", time.Since(start), err) }()

	query := Rebind(r.driver, `
		SELECT id, type, context, question, options, status, answer,
		       answered_by, answered_at, graph_operations, created_at, notified_at
		FROM clarifications WHERE id = ?
	`)
	return r.scanOne(r.db.QueryRowContext(ctx, query, id))
}

func (r *sqlClarificationRepository) ListPending(ctx context.Context) (clarifications []*Clarification, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "clarifications.list_pending", time.Since(start), err) }()

	return r.ListByStatus(ctx, ClarificationPending)
}

func (r *sqlClarificationRepository) ListByStatus(ctx context.Context, status string) (clarifications []*Clarification, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "clarifications.list_by_status", time.Since(start), err) }()

	query := Rebind(r.driver, `
		SELECT id, type, context, question, options, status, answer,
		       answered_by, answered_at, graph_operations, created_at, notified_at
		FROM clarifications
		WHERE status = ?
		ORDER BY created_at ASC
	`)
	rows, err := r.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("query clarifications: %w", err)
	}
	defer rows.Close()

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

func (r *sqlClarificationRepository) Answer(ctx context.Context, id, answer, answeredBy string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "clarifications.answer", time.Since(start), err) }()

	// Guard with AND status = 'pending' so that two joecored instances racing
	// to answer the same clarification produce exactly one winner.
	query := Rebind(r.driver, `
		UPDATE clarifications
		SET answer = ?, answered_by = ?, answered_at = ?, status = ?
		WHERE id = ? AND status = ?
	`)
	var res sql.Result
	res, err = r.db.ExecContext(ctx, query, answer, answeredBy, time.Now(), ClarificationAnswered, id, ClarificationPending)
	if err != nil {
		return fmt.Errorf("answer clarification: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("answer clarification rows affected: %w", err)
	}
	if rows == 0 {
		return ErrAlreadyAnswered
	}
	return nil
}

func (r *sqlClarificationRepository) Dismiss(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "clarifications.dismiss", time.Since(start), err) }()

	query := Rebind(r.driver, `UPDATE clarifications SET status = ? WHERE id = ?`)
	_, err = r.db.ExecContext(ctx, query, ClarificationDismissed, id)
	if err != nil {
		return fmt.Errorf("dismiss clarification: %w", err)
	}
	return nil
}

func (r *sqlClarificationRepository) MarkNotified(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "clarifications.mark_notified", time.Since(start), err) }()

	query := Rebind(r.driver, `UPDATE clarifications SET notified_at = ? WHERE id = ?`)
	_, err = r.db.ExecContext(ctx, query, time.Now(), id)
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
	if errors.Is(err, sql.ErrNoRows) {
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
		if err := json.Unmarshal([]byte(options.String), &c.Options); err != nil {
			slog.Warn("failed to unmarshal clarification options", "clarification_id", c.ID, "error", err)
		}
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
		c.AnsweredAt = parseTimeOrWarn(answeredAt.String, "clarifications.answered_at")
	}
	if notifiedAt.Valid {
		c.NotifiedAt = parseTimeOrWarn(notifiedAt.String, "clarifications.notified_at")
	}
}
