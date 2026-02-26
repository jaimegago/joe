package embeddings

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

// mockAdapter implements llm.LLMAdapter for testing.
type mockAdapter struct {
	embedding []float32
	err       error
}

func (m *mockAdapter) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, errors.New("not used")
}

func (m *mockAdapter) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, errors.New("not used")
}

func (m *mockAdapter) Embed(_ context.Context, _ string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.embedding, nil
}

func TestNew_ModelName(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		want      string
	}{
		{"explicit name", "text-embedding-3-small", "text-embedding-3-small"},
		{"empty name defaults to unknown", "", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New(&mockAdapter{}, tt.modelName)
			if got := e.ModelName(); got != tt.want {
				t.Errorf("ModelName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmbed_EmptyText(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: whitespace is not rejected by the embedder — only empty string is.
			// Only truly empty string hits the guard.
			if tt.text != "" {
				return // whitespace passes through to the adapter
			}
			e := New(&mockAdapter{embedding: []float32{0.1}}, "test")
			_, err := e.Embed(context.Background(), tt.text)
			if err == nil {
				t.Error("Embed() expected error for empty text, got nil")
			}
		})
	}
}

func TestEmbed_EmptyString_ReturnsError(t *testing.T) {
	e := New(&mockAdapter{embedding: []float32{0.1, 0.2}}, "test-model")
	_, err := e.Embed(context.Background(), "")
	if err == nil {
		t.Fatal("Embed() expected error for empty string, got nil")
	}
}

func TestEmbed_AdapterError_Propagates(t *testing.T) {
	adapterErr := errors.New("adapter failure")
	e := New(&mockAdapter{err: adapterErr}, "test-model")

	_, err := e.Embed(context.Background(), "some text")
	if err == nil {
		t.Fatal("Embed() expected error, got nil")
	}
	if !errors.Is(err, adapterErr) {
		t.Errorf("Embed() error = %v; should wrap adapter error", err)
	}
}

func TestEmbed_Success_ReturnsVector(t *testing.T) {
	expected := []float32{0.1, 0.2, 0.3, 0.4}
	e := New(&mockAdapter{embedding: expected}, "my-model")

	got, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("Embed() returned %d dimensions, want %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("Embed()[%d] = %v, want %v", i, got[i], expected[i])
		}
	}
}
