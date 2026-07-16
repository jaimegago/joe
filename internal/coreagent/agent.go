package coreagent

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/componentgov"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
)

// ToolExecutor is the minimal interface the Core Agent uses to run tool
// calls. Both *tools.Executor and *DurableExecutor satisfy it. Phase 1
// Change 9 introduced the interface so cmd/joe/server.go can swap
// in the §D5 durable wrapper without touching this file's construction
// path.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]any) (any, error)
}

// Agent is the Core Agent that maintains infrastructure knowledge
type Agent struct {
	services  *core.Services
	llm       llm.LLMAdapter
	tools     *tools.Registry
	executor  ToolExecutor
	refresher *Refresher
	discovery *Engine
	logger    *slog.Logger
	metrics   *observability.Metrics
}

// SetToolExecutor swaps the underlying tool executor. cmd/joe/server.go
// uses this after wiring the §D5 durable wrapper around the base executor.
// Calling with nil is a no-op (defensive — never deliberately disable the
// executor at runtime).
func (a *Agent) SetToolExecutor(e ToolExecutor) {
	if e == nil {
		return
	}
	a.executor = e
}

// ToolExecutor returns the current executor. Used by cmd/joe/server.go
// to compose the §D5 durable wrapper around whatever was wired at New
// time.
func (a *Agent) ToolExecutor() ToolExecutor {
	return a.executor
}

// SetRefreshAccessor wires the guarded refresh accessor onto the background
// refresher (A001-COREGOV CC-05). cmd/joe/server.go calls this once at boot,
// before Start, with an accessor built over the SAME promote-aware policy engine
// CC-04 armed, so the autonomous refresh resolves every component's adapter
// through the access guard under the agent:core principal at ActionRead.
func (a *Agent) SetRefreshAccessor(accessor *access.Accessor) {
	a.refresher.SetAccessor(accessor)
}

// New creates a new Core Agent
func New(services *core.Services, llmAdapter llm.LLMAdapter, metrics *observability.Metrics) *Agent {
	metrics = observability.EnsureMetrics(metrics)
	logger := slog.With("component", "core-agent")

	// Create Core Agent tools (graph manipulation)
	toolRegistry := tools.NewRegistry()
	registerCoreAgentTools(toolRegistry, services, logger)

	// Inject the boot-resolved write floor (D-0018) so the Core Agent's own tool
	// executor denies managed-system mutations whenever the floor is up. Routing
	// the autonomous graph-refresh path (graphdelta → store) through this executor
	// seam is deferred to a dedicated follow-up (D-0022 Task 2: non-trivial — the
	// seam is tool-name/args-shaped while the refresh is typed-store-shaped). The
	// agent:core principal now exists (A001-COREGOV CC-02): cmd/joe/server.go
	// stamps rbac.AgentCorePrincipal() onto the context handed to Start, so it is
	// carried on the refresh context end-to-end. It is carried for identity/audit
	// only at this point — the read is not yet floored (CC-03) and no grant is
	// wired (CC-04), so refresh behavior is unchanged. The floor still governs
	// the LLM tool calls this executor runs today.
	executor := tools.NewExecutor(toolRegistry, metrics, tools.WithWriteFloor(services.WriteFloor))

	return &Agent{
		services:  services,
		llm:       llmAdapter,
		tools:     toolRegistry,
		executor:  executor,
		refresher: NewRefresher(services, llmAdapter, logger, metrics),
		discovery: NewEngine(services, llmAdapter, logger, metrics),
		logger:    logger,
		metrics:   metrics,
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

// TriggerRefreshComponent manually triggers refresh for a specific component
func (a *Agent) TriggerRefreshComponent(ctx context.Context, sourceID string) error {
	a.logger.Info("manual component refresh triggered", "component_id", sourceID)
	source, err := a.services.Store.Components.Get(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("get component: %w", err)
	}
	if source == nil {
		return fmt.Errorf("%w: %s", store.ErrComponentNotFound, sourceID)
	}
	return a.refresher.refreshComponent(ctx, source)
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
	registry.Register(NewRegisterComponentTool(services, logger))
	registry.Register(NewSaveOnboardingFactTool(services, logger))
	// Parked for launch (session iac-graph-ingestion, D-0110), following the
	// same D-0081/D-0109 pattern as save_knowledge_entry below: the registration
	// is the only thing removed. The implementations, their ActionRead classifier
	// rows (internal/safety/tier.go), and their parameter schemas are retained —
	// re-enabling is restoring these three call sites.
	//
	// Why park: these three are the onboarding-era LLM-shaped graph writers. They
	// call services.Graph.AddNode/AddEdge/UpdateNode DIRECTLY, bypassing the
	// delta-reconcile seam (LoadGraphStateForComponent -> BuildGraphDelta ->
	// ApplyGraphDelta) that every *_refresh.go writes through, so nothing
	// reconciles or removes what they add. Being Read-classed they pass the write
	// floor unconditionally, observation mode included. Like save_knowledge_entry
	// they were already unreachable in fact — nothing drives this registry
	// (Agent.ExecuteTool/GetAvailableTools have no production callers, and
	// coreagent runs no LLM loop) — but nothing SAID so. Left registered, the day
	// an autonomous loop is wired here it would silently acquire the ability to
	// write LLM-inferred nodes and edges into the infrastructure graph, which
	// D-0110 makes a deterministic-only structure. Parking makes the absence
	// deliberate and test-pinned rather than incidental.
	//   registry.Register(NewGraphAddNodeTool(services, logger))
	//   registry.Register(NewGraphAddEdgeTool(services, logger))
	//   registry.Register(NewGraphUpdateNodeTool(services, logger))
	//
	// Parked for launch (session knowledge-store-maturation), following the
	// D-0081 pattern: the registration is the only thing removed. The tool
	// implementation, its ActionRead classifier row, and its NeedsDurability
	// declaration (internal/safety/tier.go) are all retained — re-enabling is
	// restoring this one call site.
	//
	// Why park rather than leave it: it was already unreachable in fact (nothing
	// drives this registry — Agent.ExecuteTool/GetAvailableTools have no callers,
	// and coreagent runs no LLM loop), but nothing SAID so. Left registered, the
	// day an autonomous loop is wired here it would silently acquire a
	// knowledge-writing tool that is Read-classed — and so passes the write floor
	// unconditionally, in observation mode included — with no audit row, no
	// principal stamping, and no admin gate. Parking makes the absence
	// deliberate and test-pinned rather than incidental.
	//   registry.Register(NewSaveKnowledgeEntryTool(services, logger))

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
		From:        fromNode,
		To:          toNode,
		Relation:    relationship,
		ComponentID: "",
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

// RegisterComponentTool registers new infrastructure components
type RegisterComponentTool struct {
	services *core.Services
	logger   *slog.Logger
}

func NewRegisterComponentTool(services *core.Services, logger *slog.Logger) *RegisterComponentTool {
	return &RegisterComponentTool{
		services: services,
		logger:   logger.With("tool", "register_component"),
	}
}

func (t *RegisterComponentTool) Name() string {
	return "register_component"
}

func (t *RegisterComponentTool) Description() string {
	return "Register a new infrastructure source (K8s cluster, Git repo, etc.)"
}

func (t *RegisterComponentTool) Parameters() llm.ParameterSchema {
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
		Required: []string{"name", "type"},
	}
}

func (t *RegisterComponentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required and must be a string")
	}

	sourceType, ok := args["type"].(string)
	if !ok || sourceType == "" {
		return nil, fmt.Errorf("type is required and must be a string")
	}
	if !store.IsValidComponentType(sourceType) {
		return nil, fmt.Errorf("unsupported component type %q (allowed: %s)", sourceType, strings.Join(store.AllowedComponentTypes(), ", "))
	}

	// Config is optional at registration (D-0029): a config-less registration
	// lands inert. Accept an absent or null config as empty; reject only a
	// present-but-non-object config.
	var configBytes json.RawMessage
	if raw, present := args["config"]; present && raw != nil {
		config, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config must be an object")
		}
		b, err := json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}
		configBytes = b
	}

	// Credential-less by construction (A003 Stream G): an autonomous discovery
	// write records type + name + routing config only. Reuse the SAME rejection
	// rule the HTTP create path uses so the LLM cannot supply a credential the
	// operator surface would refuse. Credentials enter only at promotion.
	if err := componentgov.RejectCredentialFields(configBytes); err != nil {
		return nil, fmt.Errorf("config must be credential-less: %w", err)
	}

	// Default an absent or empty config to an empty JSON object at the SAME
	// shared seam the HTTP create path uses, before encryption, so the two
	// registration surfaces cannot drift and a config-less registration persists.
	configBytes = componentgov.NormalizeRegistrationConfig(configBytes)

	randBytes := make([]byte, 8)
	if _, err := crypto_rand.Read(randBytes); err != nil {
		return nil, fmt.Errorf("failed to generate component ID: %w", err)
	}
	source := &store.Component{
		ID:     fmt.Sprintf("%s-%x", sourceType, randBytes),
		Name:   name,
		Type:   sourceType,
		Config: configBytes,
	}

	// Land the component AND its audit row in one fail-closed transaction.
	// register_component stays ActionRead (recording a discovered component to
	// Joe's own store is not a managed-system mutation), but an autonomous "Joe
	// registered a component it discovered" action still warrants a durable
	// record — actor is the Core Agent principal (agent:core).
	actor, _ := rbac.AgentCorePrincipal()
	if err := t.registerWithAudit(ctx, actor, source); err != nil {
		t.logger.Error("failed to register component", "error", err, "name", name)
		return nil, fmt.Errorf("failed to register component: %w", err)
	}

	t.logger.Info("registered new component", "id", source.ID, "name", name, "type", sourceType)
	return fmt.Sprintf("Registered component %s (id: %s, type: %s)", name, source.ID, sourceType), nil
}

// registerWithAudit commits the component insert and its audit row in one
// transaction (fail-closed: an audit failure rolls back the insert). A nil
// audit repository — a unit-test harness that does not exercise the trail —
// skips the row but still commits the insert, the same nil carve-out the HTTP
// governed-registration path uses; production always wires services.Audit.
func (t *RegisterComponentTool) registerWithAudit(ctx context.Context, actor rbac.Principal, source *store.Component) (err error) {
	blob, _ := json.Marshal(audit.Details{
		Target: "component:" + source.ID,
		After:  map[string]string{"type": source.Type, "name": source.Name},
	})
	ev := audit.Event{
		Principal:   string(actor),
		Action:      audit.ActionComponentRegister,
		ComponentID: source.ID,
		Decision:    audit.DecisionAllow,
		Reason:      "component_registration",
		Kind:        audit.KindAdminAccess,
		Context:     string(blob),
	}

	tx, err := t.services.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = t.services.Store.Components.CreateTx(ctx, tx, source); err != nil {
		return err
	}
	if t.services.Audit != nil {
		if err = t.services.Audit.InsertTx(ctx, tx, ev); err != nil {
			return fmt.Errorf("audit insert: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
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

// SaveKnowledgeEntryTool saves a derived-tier knowledge entry.
//
// Action class: ActionRead (internal/safety/tier.go) — per D-0020 Joe's own
// model-maintenance tools are Reads: this writes to Joe's knowledge store, not
// to a managed system. Being Read-classed, it passes the write floor
// unconditionally, observation mode included.
//
// PARKED (session knowledge-store-maturation): not registered on the agent:core
// registry — see registerCoreAgentTools. Pinned by
// TestSaveKnowledgeEntryToolIsParked.
type SaveKnowledgeEntryTool struct {
	services *core.Services
	logger   *slog.Logger
}

func NewSaveKnowledgeEntryTool(services *core.Services, logger *slog.Logger) *SaveKnowledgeEntryTool {
	return &SaveKnowledgeEntryTool{
		services: services,
		logger:   logger.With("tool", "save_knowledge_entry"),
	}
}

func (t *SaveKnowledgeEntryTool) Name() string { return "save_knowledge_entry" }

func (t *SaveKnowledgeEntryTool) Description() string {
	return "Save a derived (Tier 3) knowledge entry — a reusable pattern, insight, or failure mode learned during a session."
}

func (t *SaveKnowledgeEntryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"title": {
				Type:        "string",
				Description: "Short descriptive title (≤80 chars)",
			},
			"content": {
				Type:        "string",
				Description: "Full description of the knowledge item",
			},
			"entry_type": {
				Type:        "string",
				Description: "One of: pattern, failure_mode, best_practice, insight, runbook, doc, fact",
			},
			"session_id": {
				Type:        "string",
				Description: "Session ID this was derived from (for provenance)",
			},
			"confidence": {
				Type:        "number",
				Description: "Confidence score 0-1 (how reusable this is)",
			},
			"related_nodes": {
				Type:        "array",
				Description: "Graph node IDs this knowledge applies to",
				Items:       &llm.Property{Type: "string"},
			},
		},
		Required: []string{"title", "content", "entry_type"},
	}
}

func (t *SaveKnowledgeEntryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.services.Knowledge == nil {
		return nil, fmt.Errorf("knowledge service not available")
	}

	title, _ := args["title"].(string)
	content, _ := args["content"].(string)
	entryType, _ := args["entry_type"].(string)
	sessionID, _ := args["session_id"].(string)

	if title == "" || content == "" || entryType == "" {
		return nil, fmt.Errorf("title, content, and entry_type are required")
	}

	confidence := 0.8
	if c, ok := args["confidence"].(float64); ok && c > 0 {
		confidence = c
	}

	var relatedNodes []string
	if rn, ok := args["related_nodes"].([]any); ok {
		for _, n := range rn {
			if s, ok := n.(string); ok {
				relatedNodes = append(relatedNodes, s)
			}
		}
	}

	metaBytes, _ := json.Marshal(map[string]string{
		"session_id": sessionID,
	})

	entry := &knowledge.Entry{
		Tier:         knowledge.TierDerived,
		Type:         knowledge.EntryType(entryType),
		Title:        title,
		Content:      content,
		SourceType:   knowledge.SourceTypeSession,
		SourceID:     sessionID,
		Confidence:   confidence,
		RelatedNodes: relatedNodes,
		Metadata:     metaBytes,
	}

	if err := t.services.Knowledge.Create(ctx, entry); err != nil {
		t.logger.Error("failed to save knowledge entry", "error", err)
		return nil, fmt.Errorf("save knowledge entry: %w", err)
	}

	t.logger.Info("saved knowledge entry", "id", entry.ID, "title", title, "type", entryType)
	return map[string]any{"id": entry.ID, "title": title, "tier": "derived"}, nil
}
