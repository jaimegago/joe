package knowledge

import (
	"context"
	"math"
	"testing"
)

// mockEmbedder returns a fixed vector for testing.
type mockEmbedder struct {
	vecs map[string][]float32
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := m.vecs[text]; ok {
		return v, nil
	}
	// Return a default unit vector.
	return []float32{1, 0, 0}, nil
}
func (m *mockEmbedder) ModelName() string { return "mock" }

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical vectors", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal vectors", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"opposite vectors", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"zero a", []float32{0, 0}, []float32{1, 0}, 0.0},
		{"mismatched len", []float32{1, 0}, []float32{1, 0, 0}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(got-tt.want)) > 1e-6 {
				t.Errorf("cosineSimilarity = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSemanticSearch(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, nil)

	embedder := &mockEmbedder{
		vecs: map[string][]float32{
			"payment timeout fix":  {1, 0, 0},
			"runbook content":      {1, 0, 0}, // identical → similarity = 1
			"unrelated doc":        {0, 0, 1},
			"slightly related doc": {0.9, 0.1, 0},
		},
	}

	svc := NewService(repo, embedder)
	ctx := context.Background()

	entries := []*Entry{
		{Tier: TierCurated, Type: EntryTypeRunbook, Title: "Runbook", Content: "runbook content"},
		{Tier: TierCurated, Type: EntryTypeDoc, Title: "Unrelated", Content: "unrelated doc"},
		{Tier: TierSynced, Type: EntryTypeDoc, Title: "Slightly related", Content: "slightly related doc"},
	}
	for _, e := range entries {
		if err := svc.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	results, err := svc.Search(ctx, SearchRequest{Query: "payment timeout fix", TopK: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search: expected results, got none")
	}
	// First result should be highest similarity.
	if results[0].Entry.Title != "Runbook" {
		t.Errorf("top result = %q, want 'Runbook'", results[0].Entry.Title)
	}
	// Results should be sorted descending by similarity.
	for i := 1; i < len(results); i++ {
		if results[i].Similarity > results[i-1].Similarity {
			t.Errorf("results[%d].Similarity (%v) > results[%d].Similarity (%v), not sorted",
				i, results[i].Similarity, i-1, results[i-1].Similarity)
		}
	}
}

func TestSemanticSearchNoEmbedder(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, nil)
	svc := NewService(repo, nil) // no embedder

	_, err := svc.Search(context.Background(), SearchRequest{Query: "anything"})
	if err == nil {
		t.Error("Search without embedder should return error")
	}
}

func TestSemanticSearchTierFilter(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, nil)
	embedder := &mockEmbedder{}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	entries := []*Entry{
		{Tier: TierCurated, Type: EntryTypeDoc, Title: "Curated", Content: "doc a"},
		{Tier: TierDerived, Type: EntryTypeInsight, Title: "Derived", Content: "doc b"},
	}
	for _, e := range entries {
		if err := svc.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	results, err := svc.Search(ctx, SearchRequest{
		Query:      "query",
		TopK:       10,
		TierFilter: []Tier{TierCurated},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Entry.Tier != TierCurated {
			t.Errorf("expected only curated results, got %q", r.Entry.Tier)
		}
	}
}
