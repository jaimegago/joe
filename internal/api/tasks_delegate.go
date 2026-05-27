package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/uid"
)

// sseEventLocalToolCall asks the streaming client to execute a local tool on
// its own machine and POST the result back.
const sseEventLocalToolCall = "local_tool_call"

// localToolCall is the SSE payload telling the client which local tool to run.
type localToolCall struct {
	TaskID string         `json:"task_id"`
	CallID string         `json:"call_id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
}

// toolResultRequest is the body the client POSTs to deliver a delegated result.
type toolResultRequest struct {
	CallID string `json:"call_id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// delegatedResult carries a local tool's outcome back to the blocked loop.
type delegatedResult struct {
	result any
	errMsg string
}

// delegationCoordinator bridges the synchronous agent loop and the asynchronous
// client callback. When a delegated tool runs, the loop blocks in call() while
// the client executes the tool and POSTs the result, which deliver() routes
// back. It is the single bidirectional point of the streaming protocol.
type delegationCoordinator struct {
	sse    *sseWriter
	taskID string

	mu      sync.Mutex
	pending map[string]chan delegatedResult
}

func newDelegationCoordinator(sse *sseWriter, taskID string) *delegationCoordinator {
	return &delegationCoordinator{
		sse:     sse,
		taskID:  taskID,
		pending: make(map[string]chan delegatedResult),
	}
}

// call emits a local_tool_call event and blocks (on the loop goroutine) until
// the client delivers a result or ctx is cancelled. It never spawns a
// goroutine — the loop stays single-threaded (Invariant 1).
func (c *delegationCoordinator) call(ctx context.Context, name string, args map[string]any) (any, error) {
	callID := uid.New()
	ch := make(chan delegatedResult, 1)

	c.mu.Lock()
	c.pending[callID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, callID)
		c.mu.Unlock()
	}()

	if err := c.sse.event(sseEventLocalToolCall, localToolCall{
		TaskID: c.taskID,
		CallID: callID,
		Name:   name,
		Args:   args,
	}); err != nil {
		return nil, fmt.Errorf("emit local tool call: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.errMsg != "" {
			return nil, errors.New(res.errMsg)
		}
		return res.result, nil
	}
}

// deliver routes a client-supplied result to the blocked call. Returns false
// if the call_id is unknown (already resolved, cancelled, or never issued).
func (c *delegationCoordinator) deliver(callID string, result any, errMsg string) bool {
	c.mu.Lock()
	ch, ok := c.pending[callID]
	c.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- delegatedResult{result: result, errMsg: errMsg}:
		return true
	default:
		// Buffered slot already taken (duplicate delivery) — ignore.
		return false
	}
}

// delegatedTool is a registry tool whose execution is delegated to the client.
// Its tier is still classified by name in the executor, so server-side safety
// (tier gating, safe mode, policy) applies before the callback is ever emitted.
type delegatedTool struct {
	def   clientToolDef
	coord *delegationCoordinator
}

func newDelegatedTool(def clientToolDef, coord *delegationCoordinator) *delegatedTool {
	return &delegatedTool{def: def, coord: coord}
}

func (d *delegatedTool) Name() string                    { return d.def.Name }
func (d *delegatedTool) Description() string             { return d.def.Description }
func (d *delegatedTool) Parameters() llm.ParameterSchema { return d.def.Parameters }
func (d *delegatedTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return d.coord.call(ctx, d.def.Name, args)
}

// inflightTasks tracks streamed turns currently awaiting delegated tool
// results, keyed by task ID, so the tool-results callback can find the right
// coordinator.
type inflightTasks struct {
	mu sync.Mutex
	m  map[string]*delegationCoordinator
}

func newInflightTasks() *inflightTasks {
	return &inflightTasks{m: make(map[string]*delegationCoordinator)}
}

func (i *inflightTasks) add(taskID string, c *delegationCoordinator) {
	i.mu.Lock()
	i.m[taskID] = c
	i.mu.Unlock()
}

func (i *inflightTasks) get(taskID string) (*delegationCoordinator, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	c, ok := i.m[taskID]
	return c, ok
}

func (i *inflightTasks) remove(taskID string) {
	i.mu.Lock()
	delete(i.m, taskID)
	i.mu.Unlock()
}

// handleToolResult receives a delegated local-tool result and routes it to the
// blocked agent loop for the named task.
func (h *taskHandler) handleToolResult(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	coord, ok := h.inflight.get(taskID)
	if !ok {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "no such in-flight task")
		return
	}

	var req toolResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	if req.CallID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "call_id is required")
		return
	}

	if !coord.deliver(req.CallID, req.Result, req.Error) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "no such pending tool call")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
