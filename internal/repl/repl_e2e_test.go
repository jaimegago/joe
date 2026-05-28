package repl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
	_ "modernc.org/sqlite"
)

// e2eLLM calls a local (delegated) tool on the first turn, then echoes the
// tool's result into a final answer on the second turn.
type e2eLLM struct {
	mu       sync.Mutex
	calls    int
	toolName string
}

func (l *e2eLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.calls == 1 {
		// Extract the file path from the user's "read <path>" message so the
		// delegated read_file targets the real temp file on the CLI side.
		path := ""
		for _, m := range req.Messages {
			if m.Role == "user" && strings.HasPrefix(m.Content, "read ") {
				path = strings.TrimPrefix(m.Content, "read ")
			}
		}
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{ID: "tc1", Name: l.toolName, Args: map[string]any{"path": path}}},
		}, nil
	}
	answer := "no tool result seen"
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "E2E-FILE-CONTENT") {
			answer = "file said: E2E-FILE-CONTENT"
		}
	}
	return &llm.ChatResponse{Content: answer}, nil
}

func (l *e2eLLM) ChatStream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (l *e2eLLM) Embed(context.Context, string) ([]float32, error) { return []float32{0.1}, nil }

// TestE2E_ThinClientStreamsToolUsingConversation wires the real thin REPL to a
// real joe-core api.Server and proves the whole Phase 2 path: REPL → joe-core's
// single agentic loop → a local tool delegated back to and executed in the CLI
// process → result returned to the loop → final answer rendered by the REPL.
func TestE2E_ThinClientStreamsToolUsingConversation(t *testing.T) {
	// A real local file the delegated read_file will read on the CLI side.
	fp := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(fp, []byte("E2E-FILE-CONTENT"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	// Stand up a real joe-core API server backed by a scripted LLM.
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	services := &core.Services{
		Config:   &config.Config{Server: config.ServerConfig{Address: "localhost:7777"}},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: adapters.NewRegistry(),
		LLM:      &e2eLLM{toolName: "read_file"},
	}
	mux := http.NewServeMux()
	api.New(services).RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Build the real thin REPL pointed at that server, with a real local
	// registry/executor (which actually reads the file on this machine).
	registry := tools.NewLocalRegistry(safety.DefaultPolicy())
	executor := tools.NewExecutor(registry, nil)
	r := New(client.New(ts.URL), testREPLConfig(), executor, registry)

	// Drive one turn. The scripted LLM reads the path from this message and
	// asks read_file (delegated) to read it on the CLI side.
	out := captureStdout(t, func() {
		if err := r.streamTurn(context.Background(), "read "+fp); err != nil {
			t.Fatalf("streamTurn: %v", err)
		}
	})

	if !strings.Contains(out, "·") {
		t.Errorf("expected local tool activity indicator in output, got %q", out)
	}
	if !strings.Contains(out, "file said: E2E-FILE-CONTENT") {
		t.Errorf("expected final answer echoing the delegated tool result, got %q", out)
	}
}
