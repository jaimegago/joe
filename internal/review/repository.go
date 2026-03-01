package review

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrDuplicateEvent is returned by Enqueue when the event_id already exists.
var ErrDuplicateEvent = errors.New("review: duplicate event")

// ErrNotFound is returned when a job is not found.
var ErrNotFound = errors.New("review: job not found")

// Repository defines persistence for ReviewJobs.
type Repository interface {
	// Enqueue inserts a new job. Returns ErrDuplicateEvent if event_id already exists.
	Enqueue(ctx context.Context, job *ReviewJob) error
	Get(ctx context.Context, id string) (*ReviewJob, error)
	GetByEventID(ctx context.Context, eventID string) (*ReviewJob, error)
	List(ctx context.Context, f Filter) ([]*ReviewJob, error)
	UpdateStatus(ctx context.Context, id string, status JobStatus, extra statusExtra) error
}

// Filter scopes a List call.
type Filter struct {
	Platform Platform
	Status   JobStatus
	Limit    int
}

// statusExtra carries optional fields set alongside a status transition.
type statusExtra struct {
	StartedAt  *time.Time
	FinishedAt *time.Time
	ReviewBody string
	Error      string
}

// sqlRepository is the SQLite-backed Repository implementation.
type sqlRepository struct {
	db *sql.DB
}

// NewRepository creates a new SQL-backed review Repository.
func NewRepository(db *sql.DB) Repository {
	return &sqlRepository{db: db}
}

func (r *sqlRepository) Enqueue(ctx context.Context, job *ReviewJob) error {
	res, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO review_jobs
		  (id, event_id, platform, source_id, owner, repo, pr_number, head_sha,
		   status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.EventID, string(job.Platform), job.SourceID,
		job.Owner, job.Repo, job.PRNumber, job.HeadSHA,
		string(JobStatusPending), job.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert review_job: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("review_job rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateEvent
	}
	return nil
}

func (r *sqlRepository) Get(ctx context.Context, id string) (*ReviewJob, error) {
	jobs, err := r.query(ctx, "WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return jobs[0], nil
}

func (r *sqlRepository) GetByEventID(ctx context.Context, eventID string) (*ReviewJob, error) {
	jobs, err := r.query(ctx, "WHERE event_id = ?", eventID)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("%w: event_id %s", ErrNotFound, eventID)
	}
	return jobs[0], nil
}

func (r *sqlRepository) List(ctx context.Context, f Filter) ([]*ReviewJob, error) {
	var conds []string
	var args []any
	if f.Platform != "" {
		conds = append(conds, "platform = ?")
		args = append(args, string(f.Platform))
	}
	if f.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, string(f.Status))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	limit := 50
	if f.Limit > 0 {
		limit = f.Limit
	}
	where += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)
	return r.query(ctx, where, args...)
}

func (r *sqlRepository) UpdateStatus(ctx context.Context, id string, status JobStatus, extra statusExtra) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE review_jobs SET
		  status=?, started_at=?, finished_at=?, review_body=?, error=?
		WHERE id=?`,
		string(status),
		nullTime(extra.StartedAt),
		nullTime(extra.FinishedAt),
		nullStr(extra.ReviewBody),
		nullStr(extra.Error),
		id,
	)
	if err != nil {
		return fmt.Errorf("update review_job status: %w", err)
	}
	return nil
}

func (r *sqlRepository) query(ctx context.Context, where string, args ...any) ([]*ReviewJob, error) {
	q := `SELECT id, event_id, platform, source_id, owner, repo, pr_number, head_sha,
		status, review_body, error, created_at, started_at, finished_at
		FROM review_jobs ` + where

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query review_jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*ReviewJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func scanJob(rows *sql.Rows) (*ReviewJob, error) {
	var j ReviewJob
	var (
		platform, status            string
		reviewBody, errStr          sql.NullString
		createdAtStr                string
		startedAtStr, finishedAtStr sql.NullString
	)
	if err := rows.Scan(
		&j.ID, &j.EventID, &platform, &j.SourceID,
		&j.Owner, &j.Repo, &j.PRNumber, &j.HeadSHA,
		&status, &reviewBody, &errStr,
		&createdAtStr, &startedAtStr, &finishedAtStr,
	); err != nil {
		return nil, fmt.Errorf("scan review_job: %w", err)
	}
	j.Platform = Platform(platform)
	j.Status = JobStatus(status)
	if reviewBody.Valid {
		j.ReviewBody = reviewBody.String
	}
	if errStr.Valid {
		j.Error = errStr.String
	}
	if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
		j.CreatedAt = t
	}
	if startedAtStr.Valid {
		if t, err := time.Parse(time.RFC3339, startedAtStr.String); err == nil {
			j.StartedAt = &t
		}
	}
	if finishedAtStr.Valid {
		if t, err := time.Parse(time.RFC3339, finishedAtStr.String); err == nil {
			j.FinishedAt = &t
		}
	}
	return &j, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
