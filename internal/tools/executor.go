package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/safety"
)

// ErrAllToolsFailed is returned when all tools in a batch fail
var ErrAllToolsFailed = errors.New("all tools in batch failed")

// Executor executes tool calls from the LLM with safety policy enforcement.
// Before every tool.Execute():
//  1. Classify the tool's action (Read/Mutate)
//  2. Check the write floor (D-0018) — FIRST among the denials, so its reason
//     outranks an RBAC scope violation (D-0019 decision 9 precedence)
//  3. Check zone scope (component_id must be in allowed set, if configured)
//  4. Check namespace scope (namespace must be in allowed set, if configured)
//  5. Check safety policy (mutations require authorization)
//  6. Notify before execution (mutate only — blocking, cancellable)
//  7. Execute the tool
//  8. Notify after execution (mutate)
type Executor struct {
	registry          *Registry
	metrics           *observability.Metrics
	policy            *safety.SafetyPolicy
	notifier          safety.ActionNotifier
	allowedComponents map[string]struct{} // nil = no restriction; non-nil = only these component_ids permitted
	allowedNamespaces map[string]struct{} // nil = no restriction; non-nil = only these namespaces permitted
	scopeZoneNames    string              // human-readable zone names for error messages (e.g. "zone-a (frontend)")
	sourceZoneMap     map[string]string   // component_id → zone name (all zones, for identifying target zone in violations)
	namespaceZoneMap  map[string]string   // namespace → zone name (all zones, for identifying target zone in violations)
	floor             safety.WriteFloor   // boot-resolved write floor; zero value (down) = unrestricted
}

// NewExecutor creates a new tool executor. If policy is nil, DefaultPolicy is
// used (most restrictive). If notifier is nil, NoopNotifier is used.
func NewExecutor(registry *Registry, metrics *observability.Metrics, opts ...ExecutorOption) *Executor {
	e := &Executor{
		registry: registry,
		metrics:  observability.EnsureMetrics(metrics),
		policy:   safety.DefaultPolicy(),
		notifier: &safety.NoopNotifier{},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ExecutorOption configures optional Executor settings.
type ExecutorOption func(*Executor)

// WithPolicy sets the safety policy for the executor.
func WithPolicy(p *safety.SafetyPolicy) ExecutorOption {
	return func(e *Executor) {
		if p != nil {
			e.policy = p
		}
	}
}

// WithNotifier sets the action notifier for the executor.
func WithNotifier(n safety.ActionNotifier) ExecutorOption {
	return func(e *Executor) {
		if n != nil {
			e.notifier = n
		}
	}
}

// WithAllowedComponents restricts the executor to only permit tool calls that
// target one of the given source IDs. Tools that don't use component_id are
// unaffected. When the set is empty, ALL source-scoped tool calls are denied
// (the caller has no zone access). When nil (the default), no restriction
// is applied.
func WithAllowedComponents(sourceIDs []string) ExecutorOption {
	return func(e *Executor) {
		m := make(map[string]struct{}, len(sourceIDs))
		for _, id := range sourceIDs {
			m[id] = struct{}{}
		}
		e.allowedComponents = m
	}
}

// WithAllowedNamespaces restricts the executor to only permit Kubernetes tool
// calls that target one of the given namespaces. Tools that don't use a
// namespace arg are unaffected. When the set is empty, ALL namespace-scoped
// K8s tool calls are denied. When nil (the default), no restriction is applied.
func WithAllowedNamespaces(namespaces []string) ExecutorOption {
	return func(e *Executor) {
		m := make(map[string]struct{}, len(namespaces))
		for _, ns := range namespaces {
			m[ns] = struct{}{}
		}
		e.allowedNamespaces = m
	}
}

// WithWriteFloor injects the boot-resolved write floor (D-0018). When the floor
// is up, the executor denies every Mutate with a single reason-carrying error.
// The zero value (down) is the default, so executors with no floor injected run
// unrestricted; production wires the resolved floor at both construction sites
// (the Core Agent and the per-request chat executor). The floor is a read-only
// value copied into the executor — there is no setter to lower it at runtime.
func WithWriteFloor(f safety.WriteFloor) ExecutorOption {
	return func(e *Executor) {
		e.floor = f
	}
}

// WithScopeZoneNames sets the human-readable zone names included in scope
// violation error messages so the LLM can articulate zone boundaries.
func WithScopeZoneNames(names string) ExecutorOption {
	return func(e *Executor) {
		e.scopeZoneNames = names
	}
}

// WithComponentZoneMap provides a mapping of component_id → zone name for ALL zones
// (not just authorized ones). This allows violation errors to identify which
// zone the target resource belongs to.
func WithComponentZoneMap(m map[string]string) ExecutorOption {
	return func(e *Executor) {
		e.sourceZoneMap = m
	}
}

// WithNamespaceZoneMap provides a mapping of namespace → zone name for ALL
// zones. This allows namespace violation errors to identify which zone the
// target namespace belongs to.
func WithNamespaceZoneMap(m map[string]string) ExecutorOption {
	return func(e *Executor) {
		e.namespaceZoneMap = m
	}
}

// ZoneViolationError is returned when a tool targets a source outside the
// caller's authorized zones.
type ZoneViolationError struct {
	ToolName       string
	ComponentID    string
	ZoneNames      string // human-readable authorized zone context for the LLM
	TargetZoneName string // zone the target source belongs to (empty if unknown)
}

func (e *ZoneViolationError) Error() string {
	targetInfo := ""
	if e.TargetZoneName != "" {
		targetInfo = fmt.Sprintf(" Source %q belongs to zone %q.", e.ComponentID, e.TargetZoneName)
	}
	if e.ZoneNames != "" {
		return prompts.ZoneViolationMessage(e.ToolName, e.ZoneNames, targetInfo)
	}
	return prompts.ZoneViolationFallback(e.ToolName, e.ComponentID)
}

// NamespaceViolationError is returned when a tool targets a Kubernetes
// namespace outside the caller's authorized scope.
type NamespaceViolationError struct {
	ToolName          string
	Namespace         string
	AllowedNamespaces []string
	ZoneNames         string // human-readable authorized zone context for the LLM
	TargetZoneName    string // zone the target namespace belongs to (empty if unknown)
}

func (e *NamespaceViolationError) Error() string {
	targetInfo := ""
	if e.TargetZoneName != "" {
		targetInfo = fmt.Sprintf(" Namespace %q belongs to zone %q.", e.Namespace, e.TargetZoneName)
	}
	if e.ZoneNames != "" {
		return prompts.NamespaceViolationMessage(e.Namespace, e.ZoneNames, e.AllowedNamespaces, targetInfo)
	}
	return prompts.NamespaceViolationFallback(e.Namespace, e.AllowedNamespaces)
}

// Execute executes a single tool call with safety enforcement.
func (e *Executor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	start := time.Now()

	// Step 1: Look up tool in registry
	tool, err := e.registry.Get(name)
	if err != nil {
		e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
		return nil, fmt.Errorf("failed to get tool %s: %w", name, err)
	}

	// Step 2: Classify the tool's action.
	classification := safety.ClassifyTool(name)

	// Step 3: Write floor (D-0018) — checked FIRST among the denials so its
	// reason outranks an RBAC scope violation (D-0019 decision 9: precedence is
	// floor > incident > RBAC, ordered by resolvability depth — the floor is the
	// reason the user can least readily fix). A boot-resolved, runtime-immutable
	// floor denies every managed-system mutation (the Mutate set; with the Record
	// band gone this is exactly "is Mutate"). One branch, one error — the reason
	// (observation or safe_mode) rides out as data for the api layer to present
	// distinctly. The floor value was sealed at boot and is never re-derived from
	// disk here.
	//
	// Note: enforcement short-circuits at the first failing check, so only the
	// floor error (not a zone/namespace violation) ever exists for a Mutate that
	// trips both. The incident-gate half of the precedence sits one layer up, in
	// captaingate, which checks the same floor before its §C gate.
	if e.floor.Up() && classification.Class == safety.ActionMutate {
		err := &safety.WriteFloorError{Reason: e.floor.Reason()}
		e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
		return nil, err
	}

	// Step 4: Zone scope check — block calls targeting components outside authorized zones
	if e.allowedComponents != nil {
		if sourceID, ok := args["component_id"].(string); ok && sourceID != "" {
			if _, allowed := e.allowedComponents[sourceID]; !allowed {
				targetZone := ""
				if e.sourceZoneMap != nil {
					targetZone = e.sourceZoneMap[sourceID]
				}
				err := &ZoneViolationError{ToolName: name, ComponentID: sourceID, ZoneNames: e.scopeZoneNames, TargetZoneName: targetZone}
				e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
				return nil, err
			}
		}
	}

	// Step 4b: Namespace scope check — block K8s tool calls targeting namespaces outside authorized scope
	if e.allowedNamespaces != nil {
		if ns, ok := args["namespace"].(string); ok && ns != "" {
			if _, allowed := e.allowedNamespaces[ns]; !allowed {
				allowed := make([]string, 0, len(e.allowedNamespaces))
				for ns := range e.allowedNamespaces {
					allowed = append(allowed, ns)
				}
				targetZone := ""
				if e.namespaceZoneMap != nil {
					targetZone = e.namespaceZoneMap[ns]
				}
				err := &NamespaceViolationError{
					ToolName:          name,
					Namespace:         ns,
					AllowedNamespaces: allowed,
					ZoneNames:         e.scopeZoneNames,
					TargetZoneName:    targetZone,
				}
				e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
				return nil, err
			}
		}
	}

	if err := safety.CheckAccess(name, e.policy); err != nil {
		e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
		return nil, err
	}

	// Step 4: Pre-execution notification (mutate only — blocking, cancellable)
	if classification.Class == safety.ActionMutate {
		info := safety.ActionInfo{
			ToolName:    name,
			Class:       classification.Class,
			Description: classification.Description,
			Args:        args,
		}
		if err := e.notifier.NotifyBefore(ctx, info); err != nil {
			e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
			return nil, fmt.Errorf("action cancelled: %w", err)
		}
	}

	// Step 5: Execute the tool
	result, err := tool.Execute(ctx, args)

	// Step 6: Post-execution notification (mutate only — the former "tier >=
	// Record" set; with the Record band vacant this is the identical set).
	if classification.Class == safety.ActionMutate {
		info := safety.ActionInfo{
			ToolName:    name,
			Class:       classification.Class,
			Description: classification.Description,
			Args:        args,
		}
		e.notifier.NotifyAfter(ctx, info, result, err)
	}

	if err != nil {
		e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
		return nil, fmt.Errorf("failed to execute tool %s: %w", name, err)
	}

	e.metrics.RecordToolExecution(ctx, name, time.Since(start), nil)
	return result, nil
}

// ExecuteBatch executes multiple tool calls with safety enforcement.
// Returns results for all tools (successful or not) and an error only if ALL tools failed.
// Individual tool errors are stored in each ToolCallResult.Error field.
// This allows partial success - the caller can inspect individual results.
func (e *Executor) ExecuteBatch(ctx context.Context, calls []ToolCallRequest) ([]ToolCallResult, error) {
	if len(calls) == 0 {
		return nil, nil
	}

	start := time.Now()

	results := make([]ToolCallResult, len(calls))
	errorCount := 0

	for i, call := range calls {
		result, err := e.Execute(ctx, call.Name, call.Args)
		results[i] = ToolCallResult{
			ID:     call.ID,
			Name:   call.Name,
			Result: result,
			Error:  err,
		}
		if err != nil {
			errorCount++
		}
	}

	// Return error only if ALL tools failed
	if errorCount == len(calls) {
		e.metrics.RecordToolBatch(ctx, len(calls), errorCount, time.Since(start))
		return results, fmt.Errorf("%w: %d tool(s) failed", ErrAllToolsFailed, errorCount)
	}

	e.metrics.RecordToolBatch(ctx, len(calls), errorCount, time.Since(start))

	return results, nil
}

// ResultsToMessages converts tool call results to LLM messages
// This formats the results in a way that can be appended to the conversation history
func (e *Executor) ResultsToMessages(results []ToolCallResult) []llm.Message {
	messages := make([]llm.Message, len(results))

	for i, result := range results {
		messages[i] = ResultToMessage(result)
	}

	return messages
}

// ResultToMessage converts a single tool call result to an LLM message
func ResultToMessage(result ToolCallResult) llm.Message {
	var content string
	isError := result.Error != nil

	if isError {
		content = fmt.Sprintf("Error executing tool: %v", result.Error)
	} else {
		// Format the result as JSON for the LLM
		jsonBytes, err := json.Marshal(result.Result)
		if err != nil {
			content = fmt.Sprintf("Error marshaling result: %v", err)
		} else {
			content = string(jsonBytes)
		}
	}

	return llm.Message{
		Role:         "user",
		Content:      content,
		ToolResultID: result.ID,
		ToolName:     result.Name,
		IsError:      isError,
	}
}

// ToolCallRequest represents a request to execute a tool
type ToolCallRequest struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolCallResult represents the result of executing a tool
type ToolCallResult struct {
	ID     string
	Name   string
	Result any
	Error  error
}
