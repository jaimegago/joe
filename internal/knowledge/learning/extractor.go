// Package learning extracts reusable insights from completed Joe sessions and
// stores them as Tier 3 (derived) knowledge entries with provenance tracking.
package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// Extractor analyses completed sessions and derives Tier 3 knowledge entries.
type Extractor struct {
	svc    *knowledge.Service
	llm    llm.LLMAdapter
	store  *store.Store
	logger *slog.Logger
}

// New creates a new Extractor.
func New(svc *knowledge.Service, llmAdapter llm.LLMAdapter, store *store.Store) *Extractor {
	return &Extractor{
		svc:    svc,
		llm:    llmAdapter,
		store:  store,
		logger: slog.Default(),
	}
}

// ExtractFromSession analyses a completed session and creates Tier 3 knowledge
// entries for any reusable patterns, failure modes, or best practices observed.
// It is safe to call multiple times; duplicate insights are deduplicated by
// source_type+source_id in the UpsertSynced path.
func (e *Extractor) ExtractFromSession(ctx context.Context, sessionID string) error {
	// Load session messages.
	msgs, err := e.store.Sessions.GetMessages(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load session messages: %w", err)
	}
	if len(msgs) == 0 {
		return nil
	}

	// Build a transcript for the LLM.
	transcript := buildTranscript(msgs)
	if strings.TrimSpace(transcript) == "" {
		return nil
	}

	// Ask the LLM to extract learnings.
	learnings, err := e.extractLearnings(ctx, transcript)
	if err != nil {
		return fmt.Errorf("extract learnings from session %s: %w", sessionID, err)
	}

	// Persist each learning as a Tier 3 entry.
	for _, l := range learnings {
		meta, _ := json.Marshal(map[string]any{
			"session_id":   sessionID,
			"extracted_at": time.Now().UTC().Format(time.RFC3339),
		})
		entry := &knowledge.Entry{
			ID:         uuid.New().String(),
			Tier:       knowledge.TierDerived,
			Type:       knowledge.EntryType(l.Type),
			Title:      l.Title,
			Content:    l.Description,
			SourceType: knowledge.SourceTypeSession,
			SourceID:   sessionID + "/" + sanitizeTitle(l.Title),
			Confidence: l.Confidence,
			Metadata:   json.RawMessage(meta),
		}
		if len(l.RelatedNodes) > 0 {
			entry.RelatedNodes = l.RelatedNodes
		}
		if err := e.svc.UpsertSynced(ctx, entry); err != nil {
			e.logger.Warn("learning extractor: failed to upsert entry",
				"session_id", sessionID, "title", l.Title, "error", err)
		}
	}

	e.logger.Info("learning extractor: extracted learnings",
		"session_id", sessionID, "count", len(learnings))
	return nil
}

// --- LLM extraction ---

type extractedLearning struct {
	Type         string   `json:"type"` // "pattern", "failure_mode", "best_practice", "insight"
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	RelatedNodes []string `json:"related_nodes"` // graph node IDs if identifiable
	Confidence   float64  `json:"confidence"`    // 0-1
}

const extractionSystemPrompt = `You are a knowledge extraction assistant for Joe, an infrastructure copilot.

Analyse the session transcript below and extract reusable knowledge items:
- **pattern**: a recurring behaviour observed ("payment-svc timeouts correlate with high DB pool usage")
- **failure_mode**: a failure or issue resolved ("HPA not scaling because metrics-server was unavailable")
- **best_practice**: a confirmed good approach ("always check PVC binding status before scaling StatefulSets")
- **insight**: general operational insight not fitting the above

Output ONLY a JSON array of objects with fields:
  type (string), title (string, ≤80 chars), description (string, ≤500 chars),
  related_nodes ([]string, graph node IDs if identifiable, else []),
  confidence (float 0-1, how reusable this knowledge is)

Return [] if no reusable knowledge is found. Do not explain or add commentary.`

func (e *Extractor) extractLearnings(ctx context.Context, transcript string) ([]extractedLearning, error) {
	resp, err := e.llm.Chat(ctx, llm.ChatRequest{
		SystemPrompt: extractionSystemPrompt,
		Messages: []llm.Message{
			{
				Role:    "user",
				Content: "Session transcript:\n\n" + transcript,
			},
		},
		MaxTokens: 2048,
	})
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	// Strip markdown code fences if present.
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var learnings []extractedLearning
	if err := json.Unmarshal([]byte(content), &learnings); err != nil {
		return nil, fmt.Errorf("parse learnings JSON: %w (raw: %s)", err, content)
	}
	return learnings, nil
}

// --- helpers ---

func buildTranscript(msgs []*store.SessionMessage) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Role == "tool" || m.ToolName != "" {
			continue // skip raw tool results to keep transcript concise
		}
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func sanitizeTitle(title string) string {
	r := strings.NewReplacer(" ", "-", "/", "-", "\\", "-")
	s := r.Replace(strings.ToLower(title))
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
