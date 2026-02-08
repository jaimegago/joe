package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CacheRepository defines operations on .joe/ file cache.
type CacheRepository interface {
	Get(ctx context.Context, filePath string) (*JoeFileCache, error)
	Set(ctx context.Context, cache *JoeFileCache) error
	Delete(ctx context.Context, filePath string) error
	DeleteAll(ctx context.Context) error
}

type sqlCacheRepository struct {
	db *sql.DB
}

func (r *sqlCacheRepository) Get(ctx context.Context, filePath string) (*JoeFileCache, error) {
	query := `SELECT file_path, content_hash, parsed_data, parsed_at FROM joe_file_cache WHERE file_path = ?`
	var c JoeFileCache
	err := r.db.QueryRowContext(ctx, query, filePath).Scan(
		&c.FilePath, &c.ContentHash, &c.ParsedData, &c.ParsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query cache: %w", err)
	}
	return &c, nil
}

func (r *sqlCacheRepository) Set(ctx context.Context, cache *JoeFileCache) error {
	query := `
		INSERT OR REPLACE INTO joe_file_cache (file_path, content_hash, parsed_data, parsed_at)
		VALUES (?, ?, ?, ?)
	`
	cache.ParsedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, cache.FilePath, cache.ContentHash, cache.ParsedData, cache.ParsedAt)
	if err != nil {
		return fmt.Errorf("set cache: %w", err)
	}
	return nil
}

func (r *sqlCacheRepository) Delete(ctx context.Context, filePath string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM joe_file_cache WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("delete cache entry: %w", err)
	}
	return nil
}

func (r *sqlCacheRepository) DeleteAll(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM joe_file_cache")
	if err != nil {
		return fmt.Errorf("delete all cache: %w", err)
	}
	return nil
}
