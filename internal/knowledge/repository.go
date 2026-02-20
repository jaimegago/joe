package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/observability"
)

// Repository defines storage operations for knowledge entries and sources.
type Repository interface {
	// Entry operations
	CreateEntry(ctx context.Context, e *Entry) error
	GetEntry(ctx context.Context, id string) (*Entry, error)
	UpdateEntry(ctx context.Context, e *Entry) error
	DeleteEntry(ctx context.Context, id string) error
	ListEntries(ctx context.Context, f EntryFilter) ([]*Entry, error)

	// Source operations
	CreateSource(ctx context.Context, s *KnowledgeSource) error
	GetSource(ctx context.Context, id string) (*KnowledgeSource, error)
	ListSources(ctx context.Context) ([]*KnowledgeSource, error)
	UpdateSourceSyncStatus(ctx context.Context, id string, lastSyncAt time.Time, lastErr string) error
	DeleteSource(ctx context.Context, id string) error
}

// EntryFilter scopes a ListEntries call.
type EntryFilter struct {
	Tier       Tier
	SourceType SourceType
	SourceID   string
}

// sqlRepository is the SQLite-backed Repository implementation.
type sqlRepository struct {
	db      *sql.DB
	metrics *observability.Metrics
}

// NewRepository creates a new SQL-backed Repository.
func NewRepository(db *sql.DB, metrics *observability.Metrics) Repository {
	return &sqlRepository{db: db, metrics: observability.EnsureMetrics(metrics)}
}

// --- Entry operations ---

func (r *sqlRepository) CreateEntry(ctx context.Context, e *Entry) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.create_entry", time.Since(start), err) }()

	embBlob, err := encodeEmbedding(e.Embedding)
	if err != nil {
		return fmt.Errorf("encode embedding: %w", err)
	}
	nodesJSON, err := encodeStringSlice(e.RelatedNodes)
	if err != nil {
		return fmt.Errorf("encode related_nodes: %w", err)
	}
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO knowledge_entries
		  (id, tier, type, title, content, content_hash,
		   embedding, embedding_model, embedding_at,
		   source_type, source_id, source_url, related_nodes,
		   confidence, created_by, created_at, updated_at, last_synced_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, string(e.Tier), string(e.Type), e.Title, e.Content, e.ContentHash,
		embBlob, nullStr(e.EmbeddingModel), nullTime(e.EmbeddingAt),
		nullStr(string(e.SourceType)), nullStr(e.SourceID), nullStr(e.SourceURL), nodesJSON,
		e.Confidence, nullStr(e.CreatedBy), e.CreatedAt, e.UpdatedAt, nullTime(e.LastSyncedAt),
	)
	if err != nil {
		return fmt.Errorf("insert knowledge entry: %w", err)
	}
	return nil
}

func (r *sqlRepository) GetEntry(ctx context.Context, id string) (*Entry, error) {
	entries, err := r.queryEntries(ctx, "WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("knowledge entry not found: %s", id)
	}
	return entries[0], nil
}

func (r *sqlRepository) UpdateEntry(ctx context.Context, e *Entry) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.update_entry", time.Since(start), err) }()

	embBlob, err := encodeEmbedding(e.Embedding)
	if err != nil {
		return fmt.Errorf("encode embedding: %w", err)
	}
	nodesJSON, err := encodeStringSlice(e.RelatedNodes)
	if err != nil {
		return fmt.Errorf("encode related_nodes: %w", err)
	}
	e.UpdatedAt = time.Now().UTC()

	_, err = r.db.ExecContext(ctx, `
		UPDATE knowledge_entries SET
		  tier=?, type=?, title=?, content=?, content_hash=?,
		  embedding=?, embedding_model=?, embedding_at=?,
		  source_type=?, source_id=?, source_url=?, related_nodes=?,
		  confidence=?, updated_at=?, last_synced_at=?
		WHERE id=?`,
		string(e.Tier), string(e.Type), e.Title, e.Content, e.ContentHash,
		embBlob, nullStr(e.EmbeddingModel), nullTime(e.EmbeddingAt),
		nullStr(string(e.SourceType)), nullStr(e.SourceID), nullStr(e.SourceURL), nodesJSON,
		e.Confidence, e.UpdatedAt, nullTime(e.LastSyncedAt),
		e.ID,
	)
	if err != nil {
		return fmt.Errorf("update knowledge entry: %w", err)
	}
	return nil
}

func (r *sqlRepository) DeleteEntry(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.delete_entry", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx, "DELETE FROM knowledge_entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete knowledge entry: %w", err)
	}
	return nil
}

func (r *sqlRepository) ListEntries(ctx context.Context, f EntryFilter) ([]*Entry, error) {
	var conditions []string
	var args []any

	if f.Tier != "" {
		conditions = append(conditions, "tier = ?")
		args = append(args, string(f.Tier))
	}
	if f.SourceType != "" {
		conditions = append(conditions, "source_type = ?")
		args = append(args, string(f.SourceType))
	}
	if f.SourceID != "" {
		conditions = append(conditions, "source_id = ?")
		args = append(args, f.SourceID)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	return r.queryEntries(ctx, where, args...)
}

func (r *sqlRepository) queryEntries(ctx context.Context, where string, args ...any) (entries []*Entry, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.query_entries", time.Since(start), err) }()

	query := `SELECT id, tier, type, title, content, content_hash,
		embedding, embedding_model, embedding_at,
		source_type, source_id, source_url, related_nodes,
		confidence, created_by, created_at, updated_at, last_synced_at
		FROM knowledge_entries ` + where + ` ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query knowledge entries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		e, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// rowScanner abstracts sql.Row and sql.Rows for scanEntry.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (*Entry, error) {
	var e Entry
	var (
		tier, entryType                     string
		sourceType, sourceID, sourceURL     sql.NullString
		embBlob                             []byte
		embModel, createdBy                 sql.NullString
		embAt, lastSyncedAt                 sql.NullString
		nodesJSON                           sql.NullString
	)
	if err := row.Scan(
		&e.ID, &tier, &entryType, &e.Title, &e.Content, &e.ContentHash,
		&embBlob, &embModel, &embAt,
		&sourceType, &sourceID, &sourceURL, &nodesJSON,
		&e.Confidence, &createdBy, &e.CreatedAt, &e.UpdatedAt, &lastSyncedAt,
	); err != nil {
		return nil, fmt.Errorf("scan knowledge entry: %w", err)
	}
	e.Tier = Tier(tier)
	e.Type = EntryType(entryType)
	if sourceType.Valid {
		e.SourceType = SourceType(sourceType.String)
	}
	if sourceID.Valid {
		e.SourceID = sourceID.String
	}
	if sourceURL.Valid {
		e.SourceURL = sourceURL.String
	}
	if embModel.Valid {
		e.EmbeddingModel = embModel.String
	}
	if createdBy.Valid {
		e.CreatedBy = createdBy.String
	}
	if embAt.Valid {
		if t, err2 := time.Parse(time.RFC3339, embAt.String); err2 == nil {
			e.EmbeddingAt = &t
		}
	}
	if lastSyncedAt.Valid {
		if t, err2 := time.Parse(time.RFC3339, lastSyncedAt.String); err2 == nil {
			e.LastSyncedAt = &t
		}
	}
	if len(embBlob) > 0 {
		emb, err := decodeEmbedding(embBlob)
		if err == nil {
			e.Embedding = emb
		}
	}
	if nodesJSON.Valid && nodesJSON.String != "" {
		_ = json.Unmarshal([]byte(nodesJSON.String), &e.RelatedNodes)
	}
	return &e, nil
}

// --- Source operations ---

func (r *sqlRepository) CreateSource(ctx context.Context, s *KnowledgeSource) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.create_source", time.Since(start), err) }()

	s.CreatedAt = time.Now().UTC()
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO knowledge_sources
		  (id, type, name, config, status, sync_interval_minutes, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		s.ID, s.Type, s.Name, string(s.Config), s.Status, s.SyncIntervalMinutes, s.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert knowledge source: %w", err)
	}
	return nil
}

func (r *sqlRepository) GetSource(ctx context.Context, id string) (*KnowledgeSource, error) {
	sources, err := r.listSources(ctx, "WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("knowledge source not found: %s", id)
	}
	return sources[0], nil
}

func (r *sqlRepository) ListSources(ctx context.Context) ([]*KnowledgeSource, error) {
	return r.listSources(ctx, "")
}

func (r *sqlRepository) UpdateSourceSyncStatus(ctx context.Context, id string, lastSyncAt time.Time, lastErr string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.update_source_sync", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx,
		"UPDATE knowledge_sources SET last_sync_at=?, last_error=? WHERE id=?",
		lastSyncAt, lastErr, id,
	)
	if err != nil {
		return fmt.Errorf("update source sync status: %w", err)
	}
	return nil
}

func (r *sqlRepository) DeleteSource(ctx context.Context, id string) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.delete_source", time.Since(start), err) }()

	_, err = r.db.ExecContext(ctx, "DELETE FROM knowledge_sources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete knowledge source: %w", err)
	}
	return nil
}

func (r *sqlRepository) listSources(ctx context.Context, where string, args ...any) (sources []*KnowledgeSource, err error) {
	start := time.Now()
	defer func() { r.metrics.RecordDBOperation(ctx, "knowledge.list_sources", time.Since(start), err) }()

	query := `SELECT id, type, name, config, status, sync_interval_minutes,
		last_sync_at, last_error, created_at
		FROM knowledge_sources ` + where + ` ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list knowledge sources: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s KnowledgeSource
		var lastSyncAt, lastErr sql.NullString
		var cfgStr string
		if err := rows.Scan(
			&s.ID, &s.Type, &s.Name, &cfgStr, &s.Status, &s.SyncIntervalMinutes,
			&lastSyncAt, &lastErr, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan knowledge source: %w", err)
		}
		s.Config = json.RawMessage(cfgStr)
		if lastSyncAt.Valid {
			if t, err2 := time.Parse(time.RFC3339, lastSyncAt.String); err2 == nil {
				s.LastSyncAt = &t
			}
		}
		if lastErr.Valid {
			s.LastError = lastErr.String
		}
		sources = append(sources, &s)
	}
	return sources, rows.Err()
}

// --- helpers ---

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

func encodeStringSlice(ss []string) (string, error) {
	if len(ss) == 0 {
		return "", nil
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// encodeEmbedding serialises a float32 slice to JSON bytes for SQLite BLOB storage.
// Returns nil (NULL) when the slice is empty.
func encodeEmbedding(v []float32) ([]byte, error) {
	if len(v) == 0 {
		return nil, nil
	}
	return json.Marshal(v)
}

// decodeEmbedding deserialises JSON bytes back to a float32 slice.
func decodeEmbedding(b []byte) ([]float32, error) {
	var v []float32
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}
