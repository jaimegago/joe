package findings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Repository is the durable interface for findings — §A4 annotation
// semantic only. Deliberately minimal: post, list for a target, list all.
// No accept/reject, no merge, no workflow.
type Repository interface {
	PostFinding(ctx context.Context, f Finding) (*Finding, error)
	GetFinding(ctx context.Context, id string) (*Finding, error)
	ListFindings(ctx context.Context) ([]Finding, error)
	ListFindingsForTarget(ctx context.Context, targetSessionID string) ([]Finding, error)
}

// SQLRepository implements Repository on top of *sql.DB.
type SQLRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository constructs a SQLRepository.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

func (r *SQLRepository) PostFinding(ctx context.Context, f Finding) (*Finding, error) {
	if f.ID == "" {
		return nil, fmt.Errorf("post finding: id required")
	}
	if f.SourceSessionID == "" || f.TargetSessionID == "" {
		return nil, fmt.Errorf("post finding: source_session_id and target_session_id required")
	}
	if f.PostedAt.IsZero() {
		f.PostedAt = time.Now().UTC()
	}
	var referenced any
	if f.ReferencedInvestigationSessionID != nil {
		referenced = *f.ReferencedInvestigationSessionID
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO findings
			(id, source_session_id, target_session_id, author_principal, body,
			 posted_at, referenced_investigation_session_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`),
		f.ID, f.SourceSessionID, f.TargetSessionID, f.AuthorPrincipal, f.Body,
		f.PostedAt.Format(time.RFC3339), referenced)
	if err != nil {
		return nil, fmt.Errorf("post finding: %w", err)
	}
	return &f, nil
}

func (r *SQLRepository) GetFinding(ctx context.Context, id string) (*Finding, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, source_session_id, target_session_id, author_principal,
		       body, posted_at, referenced_investigation_session_id
		FROM findings WHERE id = ?`), id)
	return scanFinding(row.Scan)
}

func (r *SQLRepository) ListFindings(ctx context.Context) ([]Finding, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, source_session_id, target_session_id, author_principal,
		       body, posted_at, referenced_investigation_session_id
		FROM findings ORDER BY posted_at`)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	return scanFindingRows(rows)
}

func (r *SQLRepository) ListFindingsForTarget(ctx context.Context, targetSessionID string) ([]Finding, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, source_session_id, target_session_id, author_principal,
		       body, posted_at, referenced_investigation_session_id
		FROM findings WHERE target_session_id = ? ORDER BY posted_at`), targetSessionID)
	if err != nil {
		return nil, fmt.Errorf("list findings for target: %w", err)
	}
	defer rows.Close()
	return scanFindingRows(rows)
}

func scanFinding(scan func(...any) error) (*Finding, error) {
	var (
		f           Finding
		postedAtStr string
		referenced  sql.NullString
	)
	err := scan(&f.ID, &f.SourceSessionID, &f.TargetSessionID, &f.AuthorPrincipal,
		&f.Body, &postedAtStr, &referenced)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan finding: %w", err)
	}
	f.PostedAt, _ = time.Parse(time.RFC3339, postedAtStr)
	if referenced.Valid {
		f.ReferencedInvestigationSessionID = &referenced.String
	}
	return &f, nil
}

func scanFindingRows(rows *sql.Rows) ([]Finding, error) {
	var out []Finding
	for rows.Next() {
		f, err := scanFinding(rows.Scan)
		if err != nil {
			return nil, err
		}
		if f != nil {
			out = append(out, *f)
		}
	}
	return out, rows.Err()
}
