package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/observability"
)

// ErrComponentNotFound is returned when a source cannot be found by ID.
var ErrComponentNotFound = errors.New("source not found")

// ComponentRepository defines operations on components.
type ComponentRepository interface {
	Create(ctx context.Context, source *Component) error
	Get(ctx context.Context, id string) (*Component, error)
	List(ctx context.Context) ([]*Component, error)
	ListByType(ctx context.Context, sourceType string) ([]*Component, error)
	Update(ctx context.Context, source *Component) error
	UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) error
	Delete(ctx context.Context, id string) error
}

type sqlComponentRepository struct {
	db      *sql.DB
	driver  string
	metrics *observability.Metrics
}

func (r *sqlComponentRepository) Create(ctx context.Context, source *Component) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "components.create", time.Since(start), err) }()

	query := Rebind(r.driver, `
		INSERT INTO components (id, type, name, config, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
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

func (r *sqlComponentRepository) Get(ctx context.Context, id string) (source *Component, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "components.get", time.Since(start), err) }()

	query := Rebind(r.driver, `
		SELECT id, type, name, config, status, last_sync_at, last_error, created_at, updated_at
		FROM components WHERE id = ?
	`)
	var s Component
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
		s.LastSyncAt = parseTimeOrWarn(lastSyncAt.String, "components.last_sync_at")
	}
	if lastError.Valid {
		s.LastError = lastError.String
	}

	return &s, nil
}

func (r *sqlComponentRepository) List(ctx context.Context) (components []*Component, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "components.list", time.Since(start), err) }()

	query := `
		SELECT id, type, name, config, status, last_sync_at, last_error, created_at, updated_at
		FROM components ORDER BY name
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query components: %w", err)
	}
	defer rows.Close()

	return scanComponents(rows)
}

func (r *sqlComponentRepository) ListByType(ctx context.Context, sourceType string) (components []*Component, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "components.list_by_type", time.Since(start), err) }()

	query := Rebind(r.driver, `
		SELECT id, type, name, config, status, last_sync_at, last_error, created_at, updated_at
		FROM components WHERE type = ? ORDER BY name
	`)
	rows, err := r.db.QueryContext(ctx, query, sourceType)
	if err != nil {
		return nil, fmt.Errorf("query components by type: %w", err)
	}
	defer rows.Close()

	return scanComponents(rows)
}

func scanComponents(rows *sql.Rows) ([]*Component, error) {
	var components []*Component
	for rows.Next() {
		var s Component
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
			s.LastSyncAt = parseTimeOrWarn(lastSyncAt.String, "components.last_sync_at")
		}
		if lastError.Valid {
			s.LastError = lastError.String
		}
		components = append(components, &s)
	}
	return components, rows.Err()
}

func (r *sqlComponentRepository) Update(ctx context.Context, source *Component) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "components.update", time.Since(start), err) }()

	query := Rebind(r.driver, `
		UPDATE components
		SET type = ?, name = ?, config = ?, status = ?, updated_at = ?
		WHERE id = ?
	`)
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

func (r *sqlComponentRepository) UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "components.update_sync_status", time.Since(start), err) }()

	query := Rebind(r.driver, `
		UPDATE components
		SET last_sync_at = ?, last_error = ?, status = ?, updated_at = ?
		WHERE id = ?
	`)
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

func (r *sqlComponentRepository) Delete(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "components.delete", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx, Rebind(r.driver, "DELETE FROM components WHERE id = ?"), id)
	if err != nil {
		return fmt.Errorf("delete source: %w", err)
	}
	return nil
}
