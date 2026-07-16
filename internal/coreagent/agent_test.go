package coreagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// mockLLMAdapter for testing
type mockLLMAdapter struct{}

func (m *mockLLMAdapter) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Content: "Mock response",
		Usage:   llm.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func (m *mockLLMAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func TestNewCoreAgent(t *testing.T) {
	// Create in-memory database for testing
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer sqlStore.Close()

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "info"},
		Refresh: config.RefreshConfig{IntervalMinutes: 1},
	}

	// Create mock services
	adapterRegistry := adapters.NewRegistry()
	services := core.New(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), adapterRegistry, nil)
	defer services.Close()

	// Create mock LLM adapter
	llmAdapter := &mockLLMAdapter{}

	// Create Core Agent
	agent := New(services, llmAdapter, nil)
	if agent == nil {
		t.Fatal("New() returned nil agent")
	}

	// Verify agent has the expected components
	if len(agent.GetAvailableTools()) == 0 {
		t.Error("Core Agent should have registered tools")
	}

	// Test that tools are registered correctly. graph_add_node, graph_add_edge
	// and graph_update_node were dropped from this set by session
	// iac-graph-ingestion (D-0110) — they are parked out of the registry, and
	// their absence is pinned by TestGraphWriteToolsAreParked.
	expectedTools := map[string]bool{
		"register_component":   true,
		"save_onboarding_fact": true,
	}

	tools := agent.GetAvailableTools()
	for _, tool := range tools {
		if expectedTools[tool.Name()] {
			delete(expectedTools, tool.Name())
		}
	}

	if len(expectedTools) > 0 {
		t.Errorf("Missing expected tools: %v", expectedTools)
	}
}

func TestCoreAgentStartStop(t *testing.T) {
	// Create in-memory database for testing
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer sqlStore.Close()

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "info"},
		Refresh: config.RefreshConfig{IntervalMinutes: 1},
	}

	// Create mock services
	adapterRegistry := adapters.NewRegistry()
	services := core.New(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), adapterRegistry, nil)
	defer services.Close()

	// Create mock LLM adapter
	llmAdapter := &mockLLMAdapter{}

	// Create and start Core Agent. Wire a permit-all refresh accessor first:
	// since A001-COREGOV CC-08, Start refuses to boot the background refresh
	// without an accessor (fail-closed), so tests that start the agent must
	// wire the guarded seam exactly as production boot does.
	agent := New(services, llmAdapter, nil)
	agent.SetRefreshAccessor(permitAllAccessor(services))
	ctx := context.Background()

	if err := agent.Start(ctx); err != nil {
		t.Fatalf("failed to start Core Agent: %v", err)
	}

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop the agent
	if err := agent.Stop(ctx); err != nil {
		t.Fatalf("failed to stop Core Agent: %v", err)
	}
}

func TestCoreAgentProcessOnboarding(t *testing.T) {
	// Create in-memory database for testing
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer sqlStore.Close()

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "info"},
		Refresh: config.RefreshConfig{IntervalMinutes: 1},
	}

	// Create mock services
	adapterRegistry := adapters.NewRegistry()
	services := core.New(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), adapterRegistry, nil)
	defer services.Close()

	// Create mock LLM adapter
	llmAdapter := &mockLLMAdapter{}

	// Create Core Agent
	agent := New(services, llmAdapter, nil)
	ctx := context.Background()

	// Test onboarding processing
	testInput := "I have a Kubernetes cluster with nginx pods"
	if err := agent.ProcessOnboarding(ctx, testInput); err != nil {
		t.Fatalf("failed to process onboarding input: %v", err)
	}

	// Verify that the fact was stored
	facts, err := sqlStore.Facts.GetByType(ctx, "user_input")
	if err != nil {
		t.Fatalf("failed to retrieve facts: %v", err)
	}

	if len(facts) == 0 {
		t.Error("expected onboarding fact to be stored")
	} else if facts[0].Content != testInput {
		t.Errorf("expected fact content %q, got %q", testInput, facts[0].Content)
	}
}

// makeTestServices creates a fully migrated in-memory services for unit tests.
func makeTestServices(t *testing.T) *core.Services {
	t.Helper()
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := &config.Config{
		Logging: config.LoggingConfig{Level: "info"},
		Refresh: config.RefreshConfig{IntervalMinutes: 1},
	}
	reg := adapters.NewRegistry()
	svc := core.New(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), reg, nil)
	t.Cleanup(func() { svc.Close() })
	return svc
}

// ── Tool Name / Description / Parameters ────────────────────────────────────

func TestToolNamesAndDescriptions(t *testing.T) {
	svc := makeTestServices(t)
	logger := slog.Default()

	type namedTool interface {
		Name() string
		Description() string
	}
	tools := []struct {
		tool namedTool
		name string
	}{
		{NewGraphAddNodeTool(svc, logger), "graph_add_node"},
		{NewGraphAddEdgeTool(svc, logger), "graph_add_edge"},
		{NewGraphUpdateNodeTool(svc, logger), "graph_update_node"},
		{NewRegisterComponentTool(svc, logger), "register_component"},
		{NewSaveOnboardingFactTool(svc, logger), "save_onboarding_fact"},
	}
	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tool.Name(); got != tt.name {
				t.Errorf("Name() = %q, want %q", got, tt.name)
			}
			if tt.tool.Description() == "" {
				t.Error("Description() should not be empty")
			}
		})
	}
}

func TestToolParameters(t *testing.T) {
	svc := makeTestServices(t)
	logger := slog.Default()

	tools := []interface {
		Parameters() llm.ParameterSchema
	}{
		NewGraphAddNodeTool(svc, logger),
		NewGraphAddEdgeTool(svc, logger),
		NewGraphUpdateNodeTool(svc, logger),
		NewRegisterComponentTool(svc, logger),
		NewSaveOnboardingFactTool(svc, logger),
	}
	for _, tool := range tools {
		p := tool.Parameters()
		if p.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", p.Type)
		}
		if len(p.Properties) == 0 {
			t.Error("Parameters() should have at least one property")
		}
	}
}

// ── GraphAddNodeTool.Execute ─────────────────────────────────────────────────

func TestGraphAddNodeTool_Execute(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewGraphAddNodeTool(svc, slog.Default())
	ctx := context.Background()

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"success", map[string]any{"node_id": "n1", "node_type": "k8s_pod"}, false},
		{"success with metadata", map[string]any{"node_id": "n2", "node_type": "test", "metadata": map[string]any{"env": "prod"}}, false},
		{"missing node_id", map[string]any{"node_type": "k8s_pod"}, true},
		{"missing node_type", map[string]any{"node_id": "n3"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ── GraphAddEdgeTool.Execute ─────────────────────────────────────────────────

func TestGraphAddEdgeTool_Execute(t *testing.T) {
	svc := makeTestServices(t)
	ctx := context.Background()

	// Add prerequisite nodes so FK constraints are satisfied.
	for _, id := range []string{"from-node", "to-node"} {
		if err := svc.Graph.AddNode(ctx, graph.Node{ID: id, Type: "test", Metadata: map[string]any{}}); err != nil {
			t.Fatalf("AddNode(%s): %v", id, err)
		}
	}

	tool := NewGraphAddEdgeTool(svc, slog.Default())

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"success", map[string]any{"from_node": "from-node", "to_node": "to-node", "relationship": "depends_on"}, false},
		{"missing from_node", map[string]any{"to_node": "to-node", "relationship": "depends_on"}, true},
		{"missing to_node", map[string]any{"from_node": "from-node", "relationship": "depends_on"}, true},
		{"missing relationship", map[string]any{"from_node": "from-node", "to_node": "to-node"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ── GraphUpdateNodeTool.Execute ──────────────────────────────────────────────

func TestGraphUpdateNodeTool_Execute(t *testing.T) {
	svc := makeTestServices(t)
	ctx := context.Background()
	tool := NewGraphUpdateNodeTool(svc, slog.Default())

	t.Run("missing node_id", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"metadata": map[string]any{"k": "v"}})
		if err == nil {
			t.Error("expected error for missing node_id")
		}
	})

	t.Run("missing metadata", func(t *testing.T) {
		_, err := tool.Execute(ctx, map[string]any{"node_id": "n1"})
		if err == nil {
			t.Error("expected error for missing metadata")
		}
	})

	t.Run("success", func(t *testing.T) {
		// Add node first, then update it.
		if err := svc.Graph.AddNode(ctx, graph.Node{ID: "upd-node", Type: "test", Metadata: map[string]any{}}); err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		_, err := tool.Execute(ctx, map[string]any{
			"node_id":  "upd-node",
			"metadata": map[string]any{"version": "v2"},
		})
		if err != nil {
			t.Errorf("Execute() error = %v", err)
		}
	})
}

// ── RegisterComponentTool.Execute ───────────────────────────────────────────────

func TestRegisterComponentTool_Execute(t *testing.T) {
	svc := makeTestServices(t)
	ctx := context.Background()
	tool := NewRegisterComponentTool(svc, slog.Default())

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			"success",
			map[string]any{"name": "my-cluster", "type": "kubernetes", "config": map[string]any{}},
			false,
		},
		{"missing name", map[string]any{"type": "kubernetes", "config": map[string]any{}}, true},
		{"missing type", map[string]any{"name": "x", "config": map[string]any{}}, true},
		{"invalid type", map[string]any{"name": "x", "type": "badtype", "config": map[string]any{}}, true},
		// Config is optional at registration (D-0029): an absent config is
		// normalized to an empty JSON object and the component lands inert.
		{"absent config lands inert", map[string]any{"name": "x", "type": "kubernetes"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ── SaveOnboardingFactTool.Execute ───────────────────────────────────────────

func TestSaveOnboardingFactTool_Execute(t *testing.T) {
	svc := makeTestServices(t)
	ctx := context.Background()
	tool := NewSaveOnboardingFactTool(svc, slog.Default())

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"success", map[string]any{"fact_type": "architecture", "description": "uses microservices"}, false},
		{"missing fact_type", map[string]any{"description": "info"}, true},
		{"missing description", map[string]any{"fact_type": "arch"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ── Agent methods ────────────────────────────────────────────────────────────

func TestTriggerRefresh(t *testing.T) {
	svc := makeTestServices(t)
	agent := New(svc, &mockLLMAdapter{}, nil)
	if err := agent.TriggerRefresh(context.Background()); err != nil {
		t.Errorf("TriggerRefresh() error = %v", err)
	}
}

func TestTriggerRefreshComponent_NotFound(t *testing.T) {
	svc := makeTestServices(t)
	agent := New(svc, &mockLLMAdapter{}, nil)
	err := agent.TriggerRefreshComponent(context.Background(), "does-not-exist")
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestTriggerRefreshComponent_NoAdapter(t *testing.T) {
	svc := makeTestServices(t)
	ctx := context.Background()

	// Insert a source without registering an adapter.
	src := &store.Component{
		ID:     "src-no-adapter",
		Name:   "test",
		Type:   store.ComponentTypeKubernetes,
		Config: json.RawMessage(`{}`),
	}
	if err := svc.Store.Components.Create(ctx, src); err != nil {
		t.Fatalf("create source: %v", err)
	}

	agent := New(svc, &mockLLMAdapter{}, nil)
	// Wire a permit-all accessor so the guarded resolve runs and surfaces the
	// real "adapter not found" error (CC-08: a nil accessor would instead
	// fail-closed to a skip-quietly denial and the test would see no error).
	agent.SetRefreshAccessor(permitAllAccessor(svc))
	// refreshComponent will error (adapter not found) but TriggerRefreshComponent returns it.
	err := agent.TriggerRefreshComponent(ctx, src.ID)
	if err == nil {
		t.Error("expected error when no adapter registered for source")
	}
}

// TestExecuteTool covers the executor dispatch path. It formerly drove
// graph_add_node; session iac-graph-ingestion (D-0110) parked that tool out of
// the registry, so this repoints at save_onboarding_fact — a still-registered
// tool — to keep exercising dispatch rather than the parked tool.
func TestExecuteTool(t *testing.T) {
	svc := makeTestServices(t)
	agent := New(svc, &mockLLMAdapter{}, nil)
	ctx := context.Background()

	result, err := agent.ExecuteTool(ctx, "save_onboarding_fact", map[string]any{
		"fact_type":   "test",
		"description": "exec-tool dispatch probe",
	})
	if err != nil {
		t.Errorf("ExecuteTool() error = %v", err)
	}
	if result == nil {
		t.Error("ExecuteTool() returned nil result")
	}
}

// ── Refresher internals ──────────────────────────────────────────────────────

func TestRefresher_Refresh_EmptyStore(t *testing.T) {
	svc := makeTestServices(t)
	r := NewRefresher(svc, &mockLLMAdapter{}, slog.Default(), nil)
	if err := r.refresh(context.Background()); err != nil {
		t.Errorf("refresh() error = %v", err)
	}
}
