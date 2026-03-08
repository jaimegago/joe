package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/uid"
)

// Embedder generates an embedding vector for a piece of text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	ModelName() string
}

// Service provides knowledge store operations with business rules enforced.
type Service struct {
	repo     Repository
	embedder Embedder // optional; if nil, entries are stored without embeddings
	logger   *slog.Logger
}

// NewService creates a new KnowledgeService.
// embedder may be nil — entries will be stored without vector embeddings until one is set.
func NewService(repo Repository, embedder Embedder) *Service {
	return &Service{
		repo:     repo,
		embedder: embedder,
		logger:   slog.Default(),
	}
}

// Create creates a new knowledge entry.
// It computes the content hash, generates an embedding (if an embedder is configured),
// and persists the entry.
func (s *Service) Create(ctx context.Context, e *Entry) error {
	if e.ID == "" {
		e.ID = uid.New()
	}
	e.ContentHash = hashContent(e.Content)
	if e.Confidence == 0 {
		e.Confidence = 1.0
	}

	if err := s.embedAndAttach(ctx, e); err != nil {
		// Non-fatal: store without embedding, can be generated later.
		s.logger.Warn("failed to embed knowledge entry, storing without embedding",
			"id", e.ID, "error", err)
	}

	return s.repo.CreateEntry(ctx, e)
}

// Get returns a knowledge entry by ID.
func (s *Service) Get(ctx context.Context, id string) (*Entry, error) {
	return s.repo.GetEntry(ctx, id)
}

// Update updates a knowledge entry.
// Tier 1 (curated) entries are immutable — returns an error.
func (s *Service) Update(ctx context.Context, e *Entry) error {
	existing, err := s.repo.GetEntry(ctx, e.ID)
	if err != nil {
		return err
	}
	if existing.Tier == TierCurated {
		return fmt.Errorf("tier 1 (curated) entries are immutable: %s", e.ID)
	}

	newHash := hashContent(e.Content)
	if newHash != existing.ContentHash {
		// Content changed: re-embed.
		e.ContentHash = newHash
		if err := s.embedAndAttach(ctx, e); err != nil {
			s.logger.Warn("failed to re-embed updated entry", "id", e.ID, "error", err)
		}
	} else {
		// Preserve existing embedding.
		e.Embedding = existing.Embedding
		e.EmbeddingModel = existing.EmbeddingModel
		e.EmbeddingAt = existing.EmbeddingAt
		e.ContentHash = existing.ContentHash
	}

	return s.repo.UpdateEntry(ctx, e)
}

// Delete deletes a knowledge entry.
// Tier 1 (curated) entries are immutable — returns an error.
func (s *Service) Delete(ctx context.Context, id string) error {
	existing, err := s.repo.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	if existing.Tier == TierCurated {
		return fmt.Errorf("tier 1 (curated) entries are immutable: %s", id)
	}
	return s.repo.DeleteEntry(ctx, id)
}

// List returns entries matching the given filter.
func (s *Service) List(ctx context.Context, f EntryFilter) ([]*Entry, error) {
	return s.repo.ListEntries(ctx, f)
}

// UpsertSynced creates or updates a Tier 2 entry from an external source.
// If an entry with matching source_type+source_id exists, it is updated only
// when the content hash differs (avoiding unnecessary re-embeds and writes).
func (s *Service) UpsertSynced(ctx context.Context, e *Entry) error {
	e.Tier = TierSynced
	e.ContentHash = hashContent(e.Content)

	existing, _ := s.findBySource(ctx, e.SourceType, e.SourceID)
	if existing != nil {
		if existing.ContentHash == e.ContentHash {
			// No change; just bump last_synced_at.
			now := time.Now().UTC()
			existing.LastSyncedAt = &now
			return s.repo.UpdateEntry(ctx, existing)
		}
		e.ID = existing.ID
		e.CreatedAt = existing.CreatedAt
		e.CreatedBy = existing.CreatedBy
		if err := s.embedAndAttach(ctx, e); err != nil {
			s.logger.Warn("failed to embed synced entry", "id", e.ID, "error", err)
		}
		now := time.Now().UTC()
		e.LastSyncedAt = &now
		return s.repo.UpdateEntry(ctx, e)
	}

	// New entry.
	if e.ID == "" {
		e.ID = uid.New()
	}
	if err := s.embedAndAttach(ctx, e); err != nil {
		s.logger.Warn("failed to embed synced entry, storing without embedding",
			"id", e.ID, "error", err)
	}
	now := time.Now().UTC()
	e.LastSyncedAt = &now
	return s.repo.CreateEntry(ctx, e)
}

// CreateSource registers a new external knowledge source.
func (s *Service) CreateSource(ctx context.Context, src *KnowledgeSource) error {
	if src.ID == "" {
		src.ID = uid.New()
	}
	if src.Status == "" {
		src.Status = "active"
	}
	if src.SyncIntervalMinutes == 0 {
		src.SyncIntervalMinutes = 60
	}
	return s.repo.CreateSource(ctx, src)
}

// GetSource returns a knowledge source by ID.
func (s *Service) GetSource(ctx context.Context, id string) (*KnowledgeSource, error) {
	return s.repo.GetSource(ctx, id)
}

// ListSources returns all registered knowledge sources.
func (s *Service) ListSources(ctx context.Context) ([]*KnowledgeSource, error) {
	return s.repo.ListSources(ctx)
}

// UpdateSourceSyncStatus records a completed sync attempt.
func (s *Service) UpdateSourceSyncStatus(ctx context.Context, id string, lastSyncAt time.Time, lastErr string) error {
	return s.repo.UpdateSourceSyncStatus(ctx, id, lastSyncAt, lastErr)
}

// DeleteSource removes a knowledge source.
func (s *Service) DeleteSource(ctx context.Context, id string) error {
	return s.repo.DeleteSource(ctx, id)
}

// EmbedAll generates embeddings for all entries that are missing one.
// This is useful for backfilling after changing the embedding model.
func (s *Service) EmbedAll(ctx context.Context) error {
	if s.embedder == nil {
		return fmt.Errorf("no embedder configured")
	}
	entries, err := s.repo.ListEntries(ctx, EntryFilter{})
	if err != nil {
		return fmt.Errorf("list entries for embed: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if len(e.Embedding) > 0 && e.EmbeddingModel == s.embedder.ModelName() {
			continue // already embedded by this model
		}
		if err := s.embedAndAttach(ctx, e); err != nil {
			errs = append(errs, fmt.Errorf("embed entry %s: %w", e.ID, err))
			continue
		}
		if err := s.repo.UpdateEntry(ctx, e); err != nil {
			errs = append(errs, fmt.Errorf("update entry %s after embed: %w", e.ID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("embed all encountered %d errors: %v", len(errs), errs[0])
	}
	return nil
}

// --- private helpers ---

func (s *Service) embedAndAttach(ctx context.Context, e *Entry) error {
	if s.embedder == nil {
		return nil
	}
	vec, err := s.embedder.Embed(ctx, e.Content)
	if err != nil {
		return err
	}
	e.Embedding = vec
	e.EmbeddingModel = s.embedder.ModelName()
	now := time.Now().UTC()
	e.EmbeddingAt = &now
	return nil
}

func (s *Service) findBySource(ctx context.Context, srcType SourceType, srcID string) (*Entry, error) {
	entries, err := s.repo.ListEntries(ctx, EntryFilter{SourceType: srcType, SourceID: srcID})
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	return entries[0], nil
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum)
}
