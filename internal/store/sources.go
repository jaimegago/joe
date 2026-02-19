package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/observability"
)

// ErrSourceNotFound is returned when a source cannot be found by ID.
var ErrSourceNotFound = errors.New("source not found")

// SourceRepository defines operations on sources.
type SourceRepository interface {
	Create(ctx context.Context, source *Source) error
	Get(ctx context.Context, id string) (*Source, error)
	List(ctx context.Context) ([]*Source, error)
	ListByType(ctx context.Context, sourceType string) ([]*Source, error)
	Update(ctx context.Context, source *Source) error
	UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) error
	Delete(ctx context.Context, id string) error
}

type sqlSourceRepository struct {
	db      *sql.DB
	metrics *observability.Metrics
}

func (r *sqlSourceRepository) Create(ctx context.Context, source *Source) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sources.create", time.Since(start), err) }()

	query := `
		INSERT INTO sources (id, type, name, config, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now()
	source.CreatedAt = now
	source.UpdatedAt = now
	if source.Status == "" {
		source.Status = "active"
	}

	_, err = r.db.ExecContext(ctx, query,
		source.ID, source.Type, source.Name, source.Config,
		source.Status, source.CreatedAt, source.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert source: %w", err)
	}
	return nil
}

func (r *sqlSourceRepository) Get(ctx context.Context, id string) (source *Source, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sources.get", time.Since(start), err) }()

	query := `
		SELECT id, type, name, config, status, last_sync_at, last_error, created_at, updated_at
		FROM sources WHERE id = ?
	`
	var s Source
	var config []byte
	var lastSyncAt, lastError sql.NullString

	err = r.db.QueryRowContext(ctx, query, id).Scan(
		&s.ID, &s.Type, &s.Name, &config, &s.Status,
		&lastSyncAt, &lastError, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query source: %w", err)
	}

	s.Config = config
	if lastSyncAt.Valid {
		s.LastSyncAt = parseTimeOrWarn(lastSyncAt.String, "sources.last_sync_at")
	}
	if lastError.Valid {
		s.LastError = lastError.String
	}

	return &s, nil
}

func (r *sqlSourceRepository) List(ctx context.Context) (sources []*Source, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sources.list", time.Since(start), err) }()

	query := `
		SELECT id, type, name, config, status, last_sync_at, last_error, created_at, updated_at
		FROM sources ORDER BY name
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query sources: %w", err)
	}
	defer rows.Close()

	return scanSources(rows)
}

func (r *sqlSourceRepository) ListByType(ctx context.Context, sourceType string) (sources []*Source, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sources.list_by_type", time.Since(start), err) }()

	query := `
		SELECT id, type, name, config, status, last_sync_at, last_error, created_at, updated_at
		FROM sources WHERE type = ? ORDER BY name
	`
	rows, err := r.db.QueryContext(ctx, query, sourceType)
	if err != nil {
		return nil, fmt.Errorf("query sources by type: %w", err)
	}
	defer rows.Close()

	return scanSources(rows)
}

func scanSources(rows *sql.Rows) ([]*Source, error) {
	var sources []*Source
	for rows.Next() {
		var s Source
		var config []byte
		var lastSyncAt, lastError sql.NullString

		if err := rows.Scan(
			&s.ID, &s.Type, &s.Name, &config, &s.Status,
			&lastSyncAt, &lastError, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}

		s.Config = config
		if lastSyncAt.Valid {
			s.LastSyncAt = parseTimeOrWarn(lastSyncAt.String, "sources.last_sync_at")
		}
		if lastError.Valid {
			s.LastError = lastError.String
		}
		sources = append(sources, &s)
	}
	return sources, rows.Err()
}

func (r *sqlSourceRepository) Update(ctx context.Context, source *Source) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sources.update", time.Since(start), err) }()

	query := `
		UPDATE sources
		SET type = ?, name = ?, config = ?, status = ?, updated_at = ?
		WHERE id = ?
	`
	source.UpdatedAt = time.Now()
	_, err = r.db.ExecContext(ctx, query,
		source.Type, source.Name, source.Config, source.Status,
		source.UpdatedAt, source.ID,
	)
	if err != nil {
		return fmt.Errorf("update source: %w", err)
	}
	return nil
}

func (r *sqlSourceRepository) UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sources.update_sync_status", time.Since(start), err) }()

	query := `
		UPDATE sources
		SET last_sync_at = ?, last_error = ?, status = ?, updated_at = ?
		WHERE id = ?
	`
	status := "active"
	if lastError != "" {
		status = "error"
	}
	_, err = r.db.ExecContext(ctx, query, syncedAt, lastError, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update sync status: %w", err)
	}
	return nil
}

func (r *sqlSourceRepository) Delete(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "sources.delete", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete source: %w", err)
	}
	return nil
}
