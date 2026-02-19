package coreagent

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
)

// Agent is the Core Agent that maintains infrastructure knowledge
type Agent struct {
	services  *core.Services
	llm       llm.LLMAdapter
	tools     *tools.Registry
	executor  *tools.Executor
	refresher *Refresher
	discovery *Engine
	logger    *slog.Logger
	metrics   *observability.Metrics
	stopCh    chan struct{}
}

// New creates a new Core Agent
func New(services *core.Services, llmAdapter llm.LLMAdapter, metrics *observability.Metrics) *Agent {
	metrics = observability.EnsureMetrics(metrics)
	logger := slog.With("component", "core-agent")

	// Create Core Agent tools (graph manipulation)
	toolRegistry := tools.NewRegistry()
	registerCoreAgentTools(toolRegistry, services, logger)

	executor := tools.NewExecutor(toolRegistry, metrics)

	return &Agent{
		services:  services,
		llm:       llmAdapter,
		tools:     toolRegistry,
		executor:  executor,
		refresher: NewRefresher(services, llmAdapter, logger, metrics),
		discovery: NewEngine(services, llmAdapter, logger, metrics),
		logger:    logger,
		metrics:   metrics,
		stopCh:    make(chan struct{}),
	}
}

// Start begins background operations
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("starting core agent")

	// Start background refresh
	if err := a.refresher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start refresher: %w", err)
	}

	a.logger.Info("core agent started")
	return nil
}

// Stop gracefully shuts down the agent
func (a *Agent) Stop(ctx context.Context) error {
	a.logger.Info("stopping core agent")

	close(a.stopCh)

	// Stop background refresh
	if err := a.refresher.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop refresher: %w", err)
	}

	a.logger.Info("core agent stopped")
	return nil
}

// ProcessOnboarding handles infrastructure discovery during onboarding
func (a *Agent) ProcessOnboarding(ctx context.Context, input string) error {
	a.logger.Info("processing onboarding input", "input_length", len(input))
	return a.discovery.ProcessInput(ctx, input)
}

// TriggerRefresh manually triggers a full refresh cycle
func (a *Agent) TriggerRefresh(ctx context.Context) error {
	a.logger.Info("manual refresh triggered")
	return a.refresher.refresh(ctx)
}

// TriggerRefreshSource manually triggers refresh for a specific source
func (a *Agent) TriggerRefreshSource(ctx context.Context, sourceID string) error {
	a.logger.Info("manual source refresh triggered", "source_id", sourceID)
	source, err := a.services.Store.Sources.Get(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("get source: %w", err)
	}
	if source == nil {
		return fmt.Errorf("%w: %s", store.ErrSourceNotFound, sourceID)
	}
	return a.refresher.refreshSource(ctx, source)
}

// ExecuteTool executes a Core Agent tool call
func (a *Agent) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (any, error) {
	a.logger.Debug("executing core agent tool", "tool", toolName, "args", args)
	return a.executor.Execute(ctx, toolName, args)
}

// GetAvailableTools returns the list of tools available to the Core Agent
func (a *Agent) GetAvailableTools() []tools.Tool {
	return a.tools.GetAll()
}

// registerCoreAgentTools registers the tools available to the Core Agent
func registerCoreAgentTools(registry *tools.Registry, services *core.Services, logger *slog.Logger) {
	// Register core agent tools
	registry.Register(NewGraphAddNodeTool(services, logger))
	registry.Register(NewGraphAddEdgeTool(services, logger))
	registry.Register(NewGraphUpdateNodeTool(services, logger))
	registry.Register(NewRegisterSourceTool(services, logger))
	registry.Register(NewSaveOnboardingFactTool(services, logger))

	logger.Info("registered core agent tools", "count", len(registry.GetAll()))
}

// GraphAddNodeTool adds nodes to the knowledge graph
type GraphAddNodeTool struct {
	services *core.Services
	logger   *slog.Logger
}

func NewGraphAddNodeTool(services *core.Services, logger *slog.Logger) *GraphAddNodeTool {
	return &GraphAddNodeTool{
		services: services,
		logger:   logger.With("tool", "graph_add_node"),
	}
}

func (t *GraphAddNodeTool) Name() string {
	return "graph_add_node"
}

func (t *GraphAddNodeTool) Description() string {
	return "Add a new infrastructure node to the knowledge graph"
}

func (t *GraphAddNodeTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"node_id": {
				Type:        "string",
				Description: "Unique identifier for the node",
			},
			"node_type": {
				Type:        "string",
				Description: "Type of infrastructure node (k8s_pod, k8s_service, git_repo, etc.)",
			},
			"metadata": {
				Type:        "object",
				Description: "Key-value pairs of node metadata",
			},
		},
		Required: []string{"node_id", "node_type"},
	}
}

func (t *GraphAddNodeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	nodeID, ok := args["node_id"].(string)
	if !ok || nodeID == "" {
		return nil, fmt.Errorf("node_id is required and must be a string")
	}

	nodeType, ok := args["node_type"].(string)
	if !ok || nodeType == "" {
		return nil, fmt.Errorf("node_type is required and must be a string")
	}

	metadata, _ := args["metadata"].(map[string]any)
	if metadata == nil {
		metadata = make(map[string]any)
	}

	node := graph.Node{
		ID:       nodeID,
		Type:     nodeType,
		Metadata: metadata,
	}

	err := t.services.Graph.AddNode(ctx, node)
	if err != nil {
		t.logger.Error("failed to add node to graph", "error", err, "node_id", nodeID)
		return nil, fmt.Errorf("failed to add node: %w", err)
	}

	t.logger.Info("added node to graph", "node_id", nodeID, "node_type", nodeType)
	return fmt.Sprintf("Added node %s (type: %s) to graph", nodeID, nodeType), nil
}

// GraphAddEdgeTool adds edges to the knowledge graph
type GraphAddEdgeTool struct {
	services *core.Services
	logger   *slog.Logger
}

func NewGraphAddEdgeTool(services *core.Services, logger *slog.Logger) *GraphAddEdgeTool {
	return &GraphAddEdgeTool{
		services: services,
		logger:   logger.With("tool", "graph_add_edge"),
	}
}

func (t *GraphAddEdgeTool) Name() string {
	return "graph_add_edge"
}

func (t *GraphAddEdgeTool) Description() string {
	return "Create a relationship edge between two infrastructure nodes"
}

func (t *GraphAddEdgeTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"from_node": {
				Type:        "string",
				Description: "Source node ID",
			},
			"to_node": {
				Type:        "string",
				Description: "Target node ID",
			},
			"relationship": {
				Type:        "string",
				Description: "Type of relationship (depends_on, runs_on, connects_to, metrics_in, logs_in, traces_in, alerts_in, paged_via, dashboard_in, is_k8s_node, etc.)",
			},
		},
		Required: []string{"from_node", "to_node", "relationship"},
	}
}

func (t *GraphAddEdgeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	fromNode, ok := args["from_node"].(string)
	if !ok || fromNode == "" {
		return nil, fmt.Errorf("from_node is required and must be a string")
	}

	toNode, ok := args["to_node"].(string)
	if !ok || toNode == "" {
		return nil, fmt.Errorf("to_node is required and must be a string")
	}

	relationship, ok := args["relationship"].(string)
	if !ok || relationship == "" {
		return nil, fmt.Errorf("relationship is required and must be a string")
	}

	edge := graph.Edge{
		From:     fromNode,
		To:       toNode,
		Relation: relationship,
		SourceID: "",
	}

	err := t.services.Graph.AddEdge(ctx, edge)
	if err != nil {
		t.logger.Error("failed to add edge to graph", "error", err, "from", fromNode, "to", toNode)
		return nil, fmt.Errorf("failed to add edge: %w", err)
	}

	t.logger.Info("added edge to graph", "from", fromNode, "to", toNode, "relationship", relationship)
	return fmt.Sprintf("Added edge %s --[%s]--> %s", fromNode, relationship, toNode), nil
}

// GraphUpdateNodeTool updates existing nodes in the knowledge graph
type GraphUpdateNodeTool struct {
	services *core.Services
	logger   *slog.Logger
}

func NewGraphUpdateNodeTool(services *core.Services, logger *slog.Logger) *GraphUpdateNodeTool {
	return &GraphUpdateNodeTool{
		services: services,
		logger:   logger.With("tool", "graph_update_node"),
	}
}

func (t *GraphUpdateNodeTool) Name() string {
	return "graph_update_node"
}

func (t *GraphUpdateNodeTool) Description() string {
	return "Update metadata for an existing infrastructure node"
}

func (t *GraphUpdateNodeTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"node_id": {
				Type:        "string",
				Description: "Node ID to update",
			},
			"metadata": {
				Type:        "object",
				Description: "Key-value pairs to update (merges with existing)",
			},
		},
		Required: []string{"node_id", "metadata"},
	}
}

func (t *GraphUpdateNodeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	nodeID, ok := args["node_id"].(string)
	if !ok || nodeID == "" {
		return nil, fmt.Errorf("node_id is required and must be a string")
	}

	metadata, ok := args["metadata"].(map[string]any)
	if !ok || metadata == nil {
		return nil, fmt.Errorf("metadata is required and must be an object")
	}

	// Get the existing node
	existingNode, err := t.services.Graph.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", nodeID, err)
	}

	// Merge metadata
	for key, value := range metadata {
		existingNode.Metadata[key] = value
	}

	// AddNode performs an upsert, so this updates the existing node
	err = t.services.Graph.AddNode(ctx, *existingNode)
	if err != nil {
		t.logger.Error("failed to update node in graph", "error", err, "node_id", nodeID)
		return nil, fmt.Errorf("failed to update node: %w", err)
	}

	t.logger.Info("updated node in graph", "node_id", nodeID)
	return fmt.Sprintf("Updated node %s metadata", nodeID), nil
}

// RegisterSourceTool registers new infrastructure sources
type RegisterSourceTool struct {
	services *core.Services
	logger   *slog.Logger
}

func NewRegisterSourceTool(services *core.Services, logger *slog.Logger) *RegisterSourceTool {
	return &RegisterSourceTool{
		services: services,
		logger:   logger.With("tool", "register_source"),
	}
}

func (t *RegisterSourceTool) Name() string {
	return "register_source"
}

func (t *RegisterSourceTool) Description() string {
	return "Register a new infrastructure source (K8s cluster, Git repo, etc.)"
}

func (t *RegisterSourceTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"name": {
				Type:        "string",
				Description: "Human-readable name for the source",
			},
			"type": {
				Type:        "string",
				Description: "Source type (kubernetes, git, etc.)",
			},
			"config": {
				Type:        "object",
				Description: "Source-specific configuration",
			},
		},
		Required: []string{"name", "type", "config"},
	}
}

func (t *RegisterSourceTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required and must be a string")
	}

	sourceType, ok := args["type"].(string)
	if !ok || sourceType == "" {
		return nil, fmt.Errorf("type is required and must be a string")
	}
	if !store.IsValidSourceType(sourceType) {
		return nil, fmt.Errorf("unsupported source type %q (allowed: %s)", sourceType, strings.Join(store.AllowedSourceTypes(), ", "))
	}

	config, ok := args["config"].(map[string]any)
	if !ok || config == nil {
		return nil, fmt.Errorf("config is required and must be an object")
	}

	// Create source in store
	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	randBytes := make([]byte, 8)
	if _, err := crypto_rand.Read(randBytes); err != nil {
		return nil, fmt.Errorf("failed to generate source ID: %w", err)
	}
	source := &store.Source{
		ID:     fmt.Sprintf("%s-%x", sourceType, randBytes),
		Name:   name,
		Type:   sourceType,
		Config: configBytes,
	}

	err = t.services.Store.Sources.Create(ctx, source)
	if err != nil {
		t.logger.Error("failed to register source", "error", err, "name", name)
		return nil, fmt.Errorf("failed to register source: %w", err)
	}

	t.logger.Info("registered new source", "id", source.ID, "name", name, "type", sourceType)
	return fmt.Sprintf("Registered source %s (id: %s, type: %s)", name, source.ID, sourceType), nil
}

// SaveOnboardingFactTool saves facts discovered during onboarding
type SaveOnboardingFactTool struct {
	services *core.Services
	logger   *slog.Logger
}

func NewSaveOnboardingFactTool(services *core.Services, logger *slog.Logger) *SaveOnboardingFactTool {
	return &SaveOnboardingFactTool{
		services: services,
		logger:   logger.With("tool", "save_onboarding_fact"),
	}
}

func (t *SaveOnboardingFactTool) Name() string {
	return "save_onboarding_fact"
}

func (t *SaveOnboardingFactTool) Description() string {
	return "Save a fact discovered during onboarding"
}

func (t *SaveOnboardingFactTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"fact_type": {
				Type:        "string",
				Description: "Type of fact (architecture, component, dependency, etc.)",
			},
			"description": {
				Type:        "string",
				Description: "Human-readable description of the fact",
			},
			"metadata": {
				Type:        "object",
				Description: "Additional structured data about the fact",
			},
		},
		Required: []string{"fact_type", "description"},
	}
}

func (t *SaveOnboardingFactTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	factType, ok := args["fact_type"].(string)
	if !ok || factType == "" {
		return nil, fmt.Errorf("fact_type is required and must be a string")
	}

	description, ok := args["description"].(string)
	if !ok || description == "" {
		return nil, fmt.Errorf("description is required and must be a string")
	}

	fact := &store.OnboardingFact{
		FactType: factType,
		Subject:  "onboarding",
		Content:  description,
		Source:   "core-agent",
	}

	err := t.services.Store.Facts.Create(ctx, fact)
	if err != nil {
		t.logger.Error("failed to save onboarding fact", "error", err, "type", factType)
		return nil, fmt.Errorf("failed to save fact: %w", err)
	}

	t.logger.Info("saved onboarding fact", "id", fact.ID, "type", factType)
	return fmt.Sprintf("Saved fact: %s", description), nil
}
