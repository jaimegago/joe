package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// delegatingLLM calls a (delegated) tool on the first turn, then returns a
// final answer. On the second turn it records whether it saw the tool result,
// proving the delegated result flowed back through the loop.
type delegatingLLM struct {
	mu       sync.Mutex
	calls    int
	toolName string
	sawTool  string
}

func (l *delegatingLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.calls == 1 {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{ID: "tc1", Name: l.toolName, Args: map[string]any{"path": "/tmp/x"}}},
			Usage:     llm.TokenUsage{TotalTokens: 5},
		}, nil
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "SENTINEL-DATA") {
			l.sawTool = m.Content
		}
	}
	return &llm.ChatResponse{Content: "completed-with-tool", Usage: llm.TokenUsage{TotalTokens: 5}}, nil
}

func (l *delegatingLLM) ChatStream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (l *delegatingLLM) Embed(context.Context, string) ([]float32, error) { return []float32{0.1}, nil }

// TestProtocolConstantsInSync guards against drift between the SSE event names
// the server emits and the names the client matches on (the two live in
// separate packages and cannot share a constant without an import cycle).
func TestProtocolConstantsInSync(t *testing.T) {
	pairs := []struct {
		server, clientName string
	}{
		{sseEventStep, client.TaskEventStep},
		{sseEventFinal, client.TaskEventFinal},
		{sseEventLocalToolCall, client.TaskEventLocalToolCall},
	}
	for _, p := range pairs {
		if p.server != p.clientName {
			t.Errorf("SSE event-name drift: server %q != client %q", p.server, p.clientName)
		}
	}
}

// TestTaskStream_DelegatedToolRoundTrip drives the full streaming protocol
// through the real client: the LLM calls a local (delegated) tool, the client
// services it and POSTs the result, and the loop resumes to a final answer.
func TestTaskStream_DelegatedToolRoundTrip(t *testing.T) {
	scripted := &delegatingLLM{toolName: "read_file"} // read_file is T1 — not gated
	_, mux := setupTaskServer(t, scripted)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := client.New(ts.URL)

	var finalAnswer, finalStatus string
	var localCalls int
	err := c.StreamTask(context.Background(), client.TaskStreamRequest{
		Message: "read the file",
		ClientTools: []client.ClientToolDef{{
			Name:        "read_file",
			Description: "Read a local file",
			Parameters:  llm.ParameterSchema{Type: "object"},
		}},
	}, func(e client.TaskEvent) error {
		switch e.Type {
		case sseEventLocalToolCall:
			localCalls++
			var call client.LocalToolCall
			if err := json.Unmarshal(e.Data, &call); err != nil {
				return err
			}
			if call.Name != "read_file" {
				t.Errorf("delegated tool = %q, want read_file", call.Name)
			}
			// Execute the "local" tool and return a sentinel result.
			return c.SubmitToolResult(context.Background(), call.TaskID, call.CallID,
				map[string]any{"content": "SENTINEL-DATA"}, "")
		case sseEventFinal:
			var resp taskResponse
			if err := json.Unmarshal(e.Data, &resp); err != nil {
				return err
			}
			finalAnswer = resp.FinalAnswer
			finalStatus = resp.Status
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamTask: %v", err)
	}
	if localCalls != 1 {
		t.Errorf("local tool calls = %d, want 1", localCalls)
	}
	if finalStatus != "completed" {
		t.Errorf("status = %q, want completed", finalStatus)
	}
	if finalAnswer != "completed-with-tool" {
		t.Errorf("final answer = %q, want completed-with-tool", finalAnswer)
	}
	if !strings.Contains(scripted.sawTool, "SENTINEL-DATA") {
		t.Errorf("LLM did not see delegated tool result; sawTool=%q", scripted.sawTool)
	}
}

func TestHandleToolResult_Errors(t *testing.T) {
	srv, _ := setupTaskServer(t, &recordingLLM{answer: "x"})
	h := &taskHandler{server: srv, inflight: newInflightTasks()}

	post := func(taskID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/tasks/stream/"+taskID+"/tool-results", strings.NewReader(body))
		req.SetPathValue("taskID", taskID)
		rec := httptest.NewRecorder()
		h.handleToolResult(rec, req)
		return rec
	}

	if rec := post("nope", `{"call_id":"c1"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown task: status = %d, want 404", rec.Code)
	}

	// Register an in-flight coordinator so call-level errors are reachable.
	rec := httptest.NewRecorder()
	coord := newDelegationCoordinator(&sseWriter{w: rec, f: rec}, "t1")
	h.inflight.add("t1", coord)

	if rec := post("t1", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("missing call_id: status = %d, want 400", rec.Code)
	}
	if rec := post("t1", `{"call_id":"unknown"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown call_id: status = %d, want 404", rec.Code)
	}
}

// TestDelegationCoordinator_CancelUnblocks proves a suspended delegated call
// unblocks (with the context error) when the run is cancelled — the loop never
// hangs forever waiting on a client that went away.
func TestDelegationCoordinator_CancelUnblocks(t *testing.T) {
	rec := httptest.NewRecorder()
	coord := newDelegationCoordinator(&sseWriter{w: rec, f: rec}, "t1")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := coord.call(ctx, "read_file", nil)
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("call err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delegated call did not unblock on context cancel")
	}
}
