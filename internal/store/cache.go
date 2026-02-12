package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/observability"
)

// CacheRepository defines operations on .joe/ file cache.
type CacheRepository interface {
	Get(ctx context.Context, filePath string) (*JoeFileCache, error)
	Set(ctx context.Context, cache *JoeFileCache) error
	Delete(ctx context.Context, filePath string) error
	DeleteAll(ctx context.Context) error
}

type sqlCacheRepository struct {
	db      *sql.DB
	metrics *observability.Metrics
}

func (r *sqlCacheRepository) Get(ctx context.Context, filePath string) (cache *JoeFileCache, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "cache.get", time.Since(start), err) }()

	query := `SELECT file_path, content_hash, parsed_data, parsed_at FROM joe_file_cache WHERE file_path = ?`
	var c JoeFileCache
	err = r.db.QueryRowContext(ctx, query, filePath).Scan(
		&c.FilePath, &c.ContentHash, &c.ParsedData, &c.ParsedAt,
	)
	if err == sql.ErrNoRows {
		r.metrics.RecordCacheLookup(ctx, "joe_file_cache", false, time.Since(start), nil)
		return nil, nil
	}
	if err != nil {
		r.metrics.RecordCacheLookup(ctx, "joe_file_cache", false, time.Since(start), err)
		return nil, fmt.Errorf("query cache: %w", err)
	}
	r.metrics.RecordCacheLookup(ctx, "joe_file_cache", true, time.Since(start), nil)
	return &c, nil
}

func (r *sqlCacheRepository) Set(ctx context.Context, cache *JoeFileCache) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "cache.set", time.Since(start), err) }()

	query := `
		INSERT OR REPLACE INTO joe_file_cache (file_path, content_hash, parsed_data, parsed_at)
		VALUES (?, ?, ?, ?)
	`
	cache.ParsedAt = time.Now()
	_, err = r.db.ExecContext(ctx, query, cache.FilePath, cache.ContentHash, cache.ParsedData, cache.ParsedAt)
	if err != nil {
		return fmt.Errorf("set cache: %w", err)
	}
	return nil
}

func (r *sqlCacheRepository) Delete(ctx context.Context, filePath string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "cache.delete", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx, "DELETE FROM joe_file_cache WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("delete cache entry: %w", err)
	}
	return nil
}

func (r *sqlCacheRepository) DeleteAll(ctx context.Context) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "cache.delete_all", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx, "DELETE FROM joe_file_cache")
	if err != nil {
		return fmt.Errorf("delete all cache: %w", err)
	}
	return nil
}
