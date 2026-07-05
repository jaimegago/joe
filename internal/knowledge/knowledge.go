package knowledge

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrEntryNotFound is the sentinel for an entry lookup that matched no row. The
// repository wraps it with the offending id (fmt.Errorf("%w: ...")), so callers
// can distinguish a genuinely missing entry (errors.Is → HTTP 404) from a real
// store failure (→ 500) instead of masking every error as not-found.
var ErrEntryNotFound = errors.New("knowledge entry not found")

// Tier represents the trust level of a knowledge entry.
type Tier string

const (
	// TierCurated is human-owned, highest trust, immutable via API.
	TierCurated Tier = "curated"
	// TierSynced is fetched from an external source (Confluence, Notion), high trust.
	TierSynced Tier = "synced"
	// TierDerived is LLM-extracted from sessions, lower trust, carries confidence score.
	TierDerived Tier = "derived"
)

// EntryType classifies the kind of knowledge stored.
type EntryType string

const (
	EntryTypeRunbook     EntryType = "runbook"
	EntryTypePattern     EntryType = "pattern"
	EntryTypeDoc         EntryType = "doc"
	EntryTypeInsight     EntryType = "insight"
	EntryTypeFact        EntryType = "fact"
	EntryTypeFailureMode EntryType = "failure_mode"
)

// SourceType identifies how the entry was created.
type SourceType string

const (
	SourceTypeHuman      SourceType = "human"
	SourceTypeConfluence SourceType = "confluence"
	SourceTypeNotion     SourceType = "notion"
	SourceTypeSession    SourceType = "session"
)

// Entry is a single piece of knowledge in the store.
type Entry struct {
	ID             string     `json:"id"`
	Tier           Tier       `json:"tier"`
	Type           EntryType  `json:"type"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	ContentHash    string     `json:"content_hash"`
	Embedding      []float32  `json:"embedding,omitempty"`
	EmbeddingModel string     `json:"embedding_model,omitempty"`
	EmbeddingAt    *time.Time `json:"embedding_at,omitempty"`
	SourceType     SourceType `json:"source_type,omitempty"`
	SourceID       string     `json:"source_id,omitempty"`
	SourceURL      string     `json:"source_url,omitempty"`
	RelatedNodes   []string   `json:"related_nodes,omitempty"`
	Confidence     float64    `json:"confidence"`
	CreatedBy      string     `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastSyncedAt   *time.Time `json:"last_synced_at,omitempty"`
	// Metadata holds extra provenance data (e.g. session_id for derived entries).
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// KnowledgeSource is an external sync target (Confluence space, Notion database).
type KnowledgeSource struct {
	ID                  string          `json:"id"`
	Type                string          `json:"type"` // "confluence", "notion"
	Name                string          `json:"name"`
	Config              json.RawMessage `json:"config"` // encrypted at rest
	Status              string          `json:"status"`
	SyncIntervalMinutes int             `json:"sync_interval_minutes"`
	LastSyncAt          *time.Time      `json:"last_sync_at,omitempty"`
	LastError           string          `json:"last_error,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
}

// SearchRequest specifies a semantic search query.
type SearchRequest struct {
	Query         string  `json:"query"`
	TopK          int     `json:"top_k"`
	TierFilter    []Tier  `json:"tier_filter,omitempty"`
	MinConfidence float64 `json:"min_confidence,omitempty"`
}

// SearchResult pairs an entry with its similarity score.
type SearchResult struct {
	Entry      Entry   `json:"entry"`
	Similarity float32 `json:"similarity"`
}
