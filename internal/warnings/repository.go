package warnings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Repository is the §E warnings interface — deliberately append-only.
//
// The method set is exactly { RaiseWarning, ListWarnings, MarkReviewed }.
// No Update*, no Delete*, no queue/state methods. This is the named
// structural guard for §E2 / R9: the warnings surface is not a queue
// with state, not something Joe acts on, not self-escalating. Adding any
// method here is a structural change that fails the reflect-based
// interface-shape test in repository_test.go.
type Repository interface {
	// RaiseWarning persists a new warning. Append-only: there is no
	// corresponding update path for the (raised_at, signal_reference,
	// body, source_investigation_session_id) tuple.
	RaiseWarning(ctx context.Context, w Warning) (*Warning, error)

	// ListWarnings returns all warnings ordered by raised_at ascending.
	// Phase 1 does not filter or paginate — the surface is small by
	// design.
	ListWarnings(ctx context.Context) ([]Warning, error)

	// MarkReviewed is the ONE allowed mutation: stamp a warning as
	// human-reviewed. Calling it again on an already-reviewed row is a
	// no-op (review is idempotent).
	MarkReviewed(ctx context.Context, id, reviewedByPrincipal string, reviewedAt time.Time) error
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

func (r *SQLRepository) RaiseWarning(ctx context.Context, w Warning) (*Warning, error) {
	if w.ID == "" {
		return nil, fmt.Errorf("raise warning: id required")
	}
	if w.RaisedAt.IsZero() {
		w.RaisedAt = time.Now().UTC()
	}
	var sourceSession any
	if w.SourceInvestigationSessionID != nil {
		sourceSession = *w.SourceInvestigationSessionID
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO joe_warnings
			(id, raised_at, signal_reference, body,
			 source_investigation_session_id, reviewed_at, reviewed_by_principal)
		VALUES (?, ?, ?, ?, ?, NULL, NULL)`),
		w.ID, w.RaisedAt.Format(time.RFC3339), w.SignalReference, w.Body, sourceSession)
	if err != nil {
		return nil, fmt.Errorf("raise warning: %w", err)
	}
	return &w, nil
}

func (r *SQLRepository) ListWarnings(ctx context.Context) ([]Warning, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, raised_at, signal_reference, body,
		       source_investigation_session_id, reviewed_at, reviewed_by_principal
		FROM joe_warnings ORDER BY raised_at`)
	if err != nil {
		return nil, fmt.Errorf("list warnings: %w", err)
	}
	defer rows.Close()

	var out []Warning
	for rows.Next() {
		w, err := scanWarning(rows.Scan)
		if err != nil {
			return nil, err
		}
		if w != nil {
			out = append(out, *w)
		}
	}
	return out, rows.Err()
}

func (r *SQLRepository) MarkReviewed(ctx context.Context, id, reviewedByPrincipal string, reviewedAt time.Time) error {
	if reviewedAt.IsZero() {
		reviewedAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE joe_warnings
		SET reviewed_at = ?, reviewed_by_principal = ?
		WHERE id = ? AND reviewed_at IS NULL`),
		reviewedAt.Format(time.RFC3339), reviewedByPrincipal, id)
	if err != nil {
		return fmt.Errorf("mark warning reviewed: %w", err)
	}
	return nil
}

func scanWarning(scan func(...any) error) (*Warning, error) {
	var (
		w                   Warning
		raisedAtStr         string
		sourceSession       sql.NullString
		reviewedAt          sql.NullString
		reviewedByPrincipal sql.NullString
	)
	err := scan(&w.ID, &raisedAtStr, &w.SignalReference, &w.Body,
		&sourceSession, &reviewedAt, &reviewedByPrincipal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan warning: %w", err)
	}
	w.RaisedAt, _ = time.Parse(time.RFC3339, raisedAtStr)
	if sourceSession.Valid {
		w.SourceInvestigationSessionID = &sourceSession.String
	}
	if reviewedAt.Valid {
		t, _ := time.Parse(time.RFC3339, reviewedAt.String)
		w.ReviewedAt = &t
	}
	if reviewedByPrincipal.Valid {
		w.ReviewedByPrincipal = &reviewedByPrincipal.String
	}
	return &w, nil
}
