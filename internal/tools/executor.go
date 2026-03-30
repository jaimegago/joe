package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/safety"
)

// ErrAllToolsFailed is returned when all tools in a batch fail
var ErrAllToolsFailed = errors.New("all tools in batch failed")

// Executor executes tool calls from the LLM with safety policy enforcement.
// Before every tool.Execute():
//  1. Check zone scope (source_id must be in allowed set, if configured)
//  2. Classify the tool's tier (T1/T2/T3)
//  3. Check safety policy (T2/T3 require authorization)
//  4. Notify before execution (T3 only — blocking, cancellable)
//  5. Execute the tool
//  6. Notify after execution (T2 and T3)
type Executor struct {
	registry       *Registry
	metrics        *observability.Metrics
	policy         *safety.SafetyPolicy
	notifier       safety.ActionNotifier
	allowedSources map[string]struct{} // nil = no restriction; non-nil = only these source_ids permitted
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

// WithAllowedSources restricts the executor to only permit tool calls that
// target one of the given source IDs. Tools that don't use source_id are
// unaffected. When the set is empty, ALL source-scoped tool calls are denied
// (the caller has no zone access). When nil (the default), no restriction
// is applied.
func WithAllowedSources(sourceIDs []string) ExecutorOption {
	return func(e *Executor) {
		m := make(map[string]struct{}, len(sourceIDs))
		for _, id := range sourceIDs {
			m[id] = struct{}{}
		}
		e.allowedSources = m
	}
}

// ZoneViolationError is returned when a tool targets a source outside the
// caller's authorized zones.
type ZoneViolationError struct {
	ToolName string
	SourceID string
}

func (e *ZoneViolationError) Error() string {
	return fmt.Sprintf("zone violation: tool %q targets source %q which is outside your authorized zones", e.ToolName, e.SourceID)
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

	// Step 2: Zone scope check — block calls targeting sources outside authorized zones
	if e.allowedSources != nil {
		if sourceID, ok := args["source_id"].(string); ok && sourceID != "" {
			if _, allowed := e.allowedSources[sourceID]; !allowed {
				err := &ZoneViolationError{ToolName: name, SourceID: sourceID}
				e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
				return nil, err
			}
		}
	}

	// Step 3: Classify and check safety policy
	classification := safety.ClassifyTool(name)

	// Safe mode: only T1 (Observe) tools are permitted while joe-core is in
	// emergency shutdown recovery mode.
	if safety.IsSafeModeActive() && classification.Tier > safety.TierObserve {
		err := fmt.Errorf("safe mode active: only read-only (T1) tools are allowed — run 'joe unlock --reason \"...\"' to resume")
		e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
		return nil, err
	}

	if err := safety.CheckAccess(name, e.policy); err != nil {
		e.metrics.RecordToolExecution(ctx, name, time.Since(start), err)
		return nil, err
	}

	// Step 4: Pre-execution notification (T3 only — blocking, cancellable)
	if classification.Tier == safety.TierAct {
		info := safety.ActionInfo{
			ToolName:    name,
			Tier:        classification.Tier,
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

	// Step 6: Post-execution notification (T2 and T3)
	if classification.Tier >= safety.TierRecord {
		info := safety.ActionInfo{
			ToolName:    name,
			Tier:        classification.Tier,
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
