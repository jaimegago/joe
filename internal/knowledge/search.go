package knowledge

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// Search performs semantic search over all knowledge entries using cosine
// similarity between the query embedding and stored entry embeddings.
// Entries without embeddings are skipped.
func (s *Service) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if s.embedder == nil {
		return nil, fmt.Errorf("no embedder configured; cannot perform semantic search")
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}

	// Embed the query.
	queryVec, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Load all entries.
	entries, err := s.repo.ListEntries(ctx, EntryFilter{})
	if err != nil {
		return nil, fmt.Errorf("list entries for search: %w", err)
	}

	// Build tier filter set for O(1) lookup.
	tierSet := make(map[Tier]bool, len(req.TierFilter))
	for _, t := range req.TierFilter {
		tierSet[t] = true
	}

	minConf := req.MinConfidence
	if minConf == 0 {
		minConf = 0 // allow all unless explicitly set
	}

	var results []SearchResult
	for _, e := range entries {
		if len(e.Embedding) == 0 {
			continue
		}
		if len(tierSet) > 0 && !tierSet[e.Tier] {
			continue
		}
		if e.Confidence < minConf {
			continue
		}
		sim := cosineSimilarity(queryVec, e.Embedding)
		results = append(results, SearchResult{Entry: *e, Similarity: sim})
	}

	// Sort descending by similarity.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if len(results) > req.TopK {
		results = results[:req.TopK]
	}
	return results, nil
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 for zero-length or mismatched vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}
