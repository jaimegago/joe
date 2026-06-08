package coreagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// fakeCache implements store.CacheRepository for testing
type fakeCache struct {
	entries map[string]*store.JoeFileCache
}

func newFakeCache() *fakeCache {
	return &fakeCache{entries: make(map[string]*store.JoeFileCache)}
}

func (c *fakeCache) Get(ctx context.Context, filePath string) (*store.JoeFileCache, error) {
	entry, ok := c.entries[filePath]
	if !ok {
		return nil, nil
	}
	return entry, nil
}

func (c *fakeCache) Set(ctx context.Context, cache *store.JoeFileCache) error {
	c.entries[cache.FilePath] = cache
	return nil
}

func (c *fakeCache) Delete(ctx context.Context, filePath string) error {
	delete(c.entries, filePath)
	return nil
}

func (c *fakeCache) DeleteAll(ctx context.Context) error {
	c.entries = make(map[string]*store.JoeFileCache)
	return nil
}

// fakeLLM implements llm.LLMAdapter for testing
type fakeLLM struct {
	chatCalls   int
	returnCalls []llm.ToolCall
}

func (f *fakeLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.chatCalls++
	return &llm.ChatResponse{
		Content:   "Interpreted .joe/ file",
		ToolCalls: f.returnCalls,
		Usage: llm.TokenUsage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	}, nil
}

func (f *fakeLLM) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, nil
}

// fakeGitAdapterWithContent extends fakeGitAdapter with content
type fakeGitAdapterWithContent struct {
	files       []git.FileInfo
	fileContent map[string]string
}

func (f *fakeGitAdapterWithContent) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeGitAdapterWithContent) Disconnect() error                                  { return nil }
func (f *fakeGitAdapterWithContent) Status() adapters.Status {
	return adapters.Status{Connected: true, Message: "connected"}
}

func (f *fakeGitAdapterWithContent) ReadFile(ctx context.Context, path string) (string, error) {
	content, ok := f.fileContent[path]
	if !ok {
		return "", nil
	}
	return content, nil
}

func (f *fakeGitAdapterWithContent) ListFiles(ctx context.Context, dir string) ([]git.FileInfo, error) {
	if dir == ".joe" {
		return f.files, nil
	}
	return nil, nil
}

func (f *fakeGitAdapterWithContent) Log(ctx context.Context, limit int) ([]git.CommitInfo, error) {
	return nil, nil
}

func (f *fakeGitAdapterWithContent) Diff(ctx context.Context, from, to string) (string, error) {
	return "", nil
}

func TestJoeFileService_CacheHit(t *testing.T) {
	cache := newFakeCache()
	fakeLLM := &fakeLLM{
		returnCalls: []llm.ToolCall{
			{ID: "1", Name: "graph_add_node", Args: map[string]any{"node_id": "service/payment", "node_type": "service"}},
		},
	}
	service := NewJoeFileService(cache, fakeLLM, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)

	fileContent := "joe_version: \"1.0\"\nrepo:\n  name: payment-service"
	hash := computeContentHash(fileContent)

	// Pre-populate cache
	toolCallsJSON, _ := json.Marshal([]llm.ToolCall{
		{ID: "cached", Name: "graph_add_node", Args: map[string]any{"node_id": "service/payment", "node_type": "service"}},
	})
	cache.Set(context.Background(), &store.JoeFileCache{
		FilePath:    ".joe/manifest.yaml",
		ContentHash: hash,
		ParsedData:  json.RawMessage(fileContent),
		ToolCalls:   toolCallsJSON,
		ProcessedAt: time.Now(),
		ParsedAt:    time.Now(),
	})

	gitAdapter := &fakeGitAdapterWithContent{
		files: []git.FileInfo{
			{Path: ".joe/manifest.yaml", IsDir: false},
		},
		fileContent: map[string]string{
			".joe/manifest.yaml": fileContent,
		},
	}

	// Process files - should hit cache
	toolCalls, err := service.ProcessJoeFiles(context.Background(), gitAdapter, "src-1")
	if err != nil {
		t.Fatalf("ProcessJoeFiles error: %v", err)
	}

	// Verify LLM was NOT called
	if fakeLLM.chatCalls != 0 {
		t.Errorf("LLM chat calls = %d, want 0 (cache hit)", fakeLLM.chatCalls)
	}

	// Verify tool calls returned
	if len(toolCalls) != 1 {
		t.Errorf("tool calls count = %d, want 1", len(toolCalls))
	}

	if len(toolCalls) > 0 && toolCalls[0].ID != "cached" {
		t.Errorf("tool call ID = %s, want cached", toolCalls[0].ID)
	}
}

func TestJoeFileService_CacheMiss(t *testing.T) {
	cache := newFakeCache()
	fakeLLM := &fakeLLM{
		returnCalls: []llm.ToolCall{
			{ID: "llm-1", Name: "graph_add_node", Args: map[string]any{"node_id": "service/payment", "node_type": "service"}},
		},
	}
	service := NewJoeFileService(cache, fakeLLM, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)

	fileContent := "joe_version: \"1.0\"\nrepo:\n  name: payment-service"

	gitAdapter := &fakeGitAdapterWithContent{
		files: []git.FileInfo{
			{Path: ".joe/manifest.yaml", IsDir: false},
		},
		fileContent: map[string]string{
			".joe/manifest.yaml": fileContent,
		},
	}

	// Process files - should miss cache and call LLM
	toolCalls, err := service.ProcessJoeFiles(context.Background(), gitAdapter, "src-1")
	if err != nil {
		t.Fatalf("ProcessJoeFiles error: %v", err)
	}

	// Verify LLM WAS called
	if fakeLLM.chatCalls != 1 {
		t.Errorf("LLM chat calls = %d, want 1 (cache miss)", fakeLLM.chatCalls)
	}

	// Verify tool calls returned
	if len(toolCalls) != 1 {
		t.Errorf("tool calls count = %d, want 1", len(toolCalls))
	}

	if len(toolCalls) > 0 && toolCalls[0].ID != "llm-1" {
		t.Errorf("tool call ID = %s, want llm-1", toolCalls[0].ID)
	}

	// Verify cache was updated
	cached, _ := cache.Get(context.Background(), ".joe/manifest.yaml")
	if cached == nil {
		t.Fatalf("cache entry not found after cache miss")
	}

	if cached.ContentHash != computeContentHash(fileContent) {
		t.Errorf("cached hash mismatch")
	}
}

func TestJoeFileService_HashChange(t *testing.T) {
	cache := newFakeCache()
	fakeLLM := &fakeLLM{
		returnCalls: []llm.ToolCall{
			{ID: "new-1", Name: "graph_add_node", Args: map[string]any{"node_id": "service/payment-v2", "node_type": "service"}},
		},
	}
	service := NewJoeFileService(cache, fakeLLM, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)

	oldContent := "joe_version: \"1.0\"\nrepo:\n  name: payment-service"
	oldHash := computeContentHash(oldContent)

	// Pre-populate cache with old content
	toolCallsJSON, _ := json.Marshal([]llm.ToolCall{
		{ID: "old", Name: "graph_add_node", Args: map[string]any{"node_id": "service/payment", "node_type": "service"}},
	})
	cache.Set(context.Background(), &store.JoeFileCache{
		FilePath:    ".joe/manifest.yaml",
		ContentHash: oldHash,
		ParsedData:  json.RawMessage(oldContent),
		ToolCalls:   toolCallsJSON,
		ProcessedAt: time.Now(),
		ParsedAt:    time.Now(),
	})

	// Process new content with changed hash
	newContent := "joe_version: \"2.0\"\nrepo:\n  name: payment-service-v2"
	gitAdapter := &fakeGitAdapterWithContent{
		files: []git.FileInfo{
			{Path: ".joe/manifest.yaml", IsDir: false},
		},
		fileContent: map[string]string{
			".joe/manifest.yaml": newContent,
		},
	}

	toolCalls, err := service.ProcessJoeFiles(context.Background(), gitAdapter, "src-1")
	if err != nil {
		t.Fatalf("ProcessJoeFiles error: %v", err)
	}

	// Verify LLM WAS called (hash changed)
	if fakeLLM.chatCalls != 1 {
		t.Errorf("LLM chat calls = %d, want 1 (hash changed)", fakeLLM.chatCalls)
	}

	// Verify new tool calls returned
	if len(toolCalls) != 1 {
		t.Errorf("tool calls count = %d, want 1", len(toolCalls))
	}

	if len(toolCalls) > 0 && toolCalls[0].ID != "new-1" {
		t.Errorf("tool call ID = %s, want new-1", toolCalls[0].ID)
	}

	// Verify cache was updated with new hash
	cached, _ := cache.Get(context.Background(), ".joe/manifest.yaml")
	if cached == nil {
		t.Fatalf("cache entry not found after hash change")
	}

	newHash := computeContentHash(newContent)
	if cached.ContentHash != newHash {
		t.Errorf("cached hash = %s, want %s", cached.ContentHash, newHash)
	}
}

func TestJoeFileService_NoJoeFiles(t *testing.T) {
	cache := newFakeCache()
	fakeLLM := &fakeLLM{}
	service := NewJoeFileService(cache, fakeLLM, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)

	// No .joe files present
	gitAdapter := &fakeGitAdapterWithContent{
		files:       []git.FileInfo{},
		fileContent: map[string]string{},
	}

	toolCalls, err := service.ProcessJoeFiles(context.Background(), gitAdapter, "src-1")
	if err != nil {
		t.Fatalf("ProcessJoeFiles error: %v", err)
	}

	// Verify no LLM calls
	if fakeLLM.chatCalls != 0 {
		t.Errorf("LLM chat calls = %d, want 0 (no .joe files)", fakeLLM.chatCalls)
	}

	// Verify nil returned (no .joe/ files found, not just empty slice)
	if toolCalls != nil {
		t.Errorf("tool calls = %v, want nil (no .joe files)", toolCalls)
	}
}

func TestJoeFileService_MultipleFiles(t *testing.T) {
	cache := newFakeCache()
	fakeLLM := &fakeLLM{
		returnCalls: []llm.ToolCall{
			{ID: "1", Name: "graph_add_node", Args: map[string]any{"node_id": "service/payment", "node_type": "service"}},
		},
	}
	service := NewJoeFileService(cache, fakeLLM, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)

	manifestContent := "joe_version: \"1.0\"\nrepo:\n  name: payment-service"
	sourcesContent := "components:\n  - type: postgresql\n    reference: main-db"

	gitAdapter := &fakeGitAdapterWithContent{
		files: []git.FileInfo{
			{Path: ".joe/manifest.yaml", IsDir: false},
			{Path: ".joe/components.yaml", IsDir: false},
		},
		fileContent: map[string]string{
			".joe/manifest.yaml":   manifestContent,
			".joe/components.yaml": sourcesContent,
		},
	}

	// Process files - should process both
	toolCalls, err := service.ProcessJoeFiles(context.Background(), gitAdapter, "src-1")
	if err != nil {
		t.Fatalf("ProcessJoeFiles error: %v", err)
	}

	// Verify LLM was called twice (once per file)
	if fakeLLM.chatCalls != 2 {
		t.Errorf("LLM chat calls = %d, want 2 (two .joe files)", fakeLLM.chatCalls)
	}

	// Verify tool calls returned (2 calls from LLM)
	if len(toolCalls) != 2 {
		t.Errorf("tool calls count = %d, want 2", len(toolCalls))
	}

	// Verify both files cached
	cached1, _ := cache.Get(context.Background(), ".joe/manifest.yaml")
	cached2, _ := cache.Get(context.Background(), ".joe/components.yaml")

	if cached1 == nil || cached2 == nil {
		t.Errorf("both files should be in cache")
	}
}
