package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
)

// ClientToolDef advertises a local tool the CLI can execute on the user's
// machine. joe-core registers it as a delegating stub for the LLM.
type ClientToolDef struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Parameters  llm.ParameterSchema `json:"parameters"`
}

// TaskStreamRequest is the body for a streamed agentic turn.
type TaskStreamRequest struct {
	Message     string          `json:"message"`
	SessionID   string          `json:"session_id,omitempty"`
	ClientTools []ClientToolDef `json:"client_tools,omitempty"`
}

// LocalToolCall is the decoded payload of a "local_tool_call" SSE event: a
// request from joe-core for the CLI to execute a local tool and POST the
// result via SubmitToolResult.
type LocalToolCall struct {
	TaskID string         `json:"task_id"`
	CallID string         `json:"call_id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
}

// TaskEvent is one decoded Server-Sent Event from the streaming task endpoint.
// Type is the SSE event name ("step", "final", ...); Data is its raw JSON
// payload, decoded by the caller according to Type.
type TaskEvent struct {
	Type string
	Data json.RawMessage
}

// StreamTask opens a streamed agentic turn against joe-core and invokes onEvent
// for each SSE event until the stream closes or onEvent returns an error.
//
// A streamed turn can run for minutes, so this uses a client without the
// Client's default total timeout; cancellation is via ctx.
func (c *Client) StreamTask(ctx context.Context, req TaskStreamRequest, onEvent func(TaskEvent) error) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal stream task: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+apiTasksStreamPath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	c.setAuth(httpReq)

	// Reuse the configured transport (TLS settings) but drop the total timeout
	// so a long-running stream is not cut off mid-turn.
	streamClient := &http.Client{Transport: c.httpClient.Transport}
	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("stream task request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if apiErr, ok := parseAPIError(body, resp.StatusCode); ok {
			return apiErr
		}
		return fmt.Errorf("stream task failed (status %d): %s", resp.StatusCode, string(body))
	}

	return parseSSE(resp.Body, onEvent)
}

// parseSSE reads a text/event-stream body and dispatches each complete event.
// Events are terminated by a blank line; this server emits single-line JSON
// data, but multi-line data lines are concatenated defensively.
func parseSSE(body io.Reader, onEvent func(TaskEvent) error) error {
	scanner := bufio.NewScanner(body)
	// Tool results can be large; allow generous lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var event string
	var data strings.Builder
	dispatch := func() error {
		if event == "" && data.Len() == 0 {
			return nil
		}
		err := onEvent(TaskEvent{Type: event, Data: json.RawMessage(data.String())})
		event = ""
		data.Reset()
		return err
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if err := dispatch(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// Flush a trailing event with no terminating blank line.
	return dispatch()
}

// SubmitToolResult delivers a delegated local-tool result back to joe-core for
// the given streamed task. errMsg is non-empty when the local tool failed.
func (c *Client) SubmitToolResult(ctx context.Context, taskID, callID string, result any, errMsg string) error {
	payload, err := json.Marshal(toolResultBody{CallID: callID, Result: result, Error: errMsg})
	if err != nil {
		return fmt.Errorf("marshal tool result: %w", err)
	}
	u := fmt.Sprintf("%s%s/%s/tool-results", c.baseURL, apiTasksStreamPath, url.PathEscape(taskID))
	return c.doJSON(ctx, "POST", u, bytes.NewReader(payload), http.StatusOK, nil, "submit tool result")
}

type toolResultBody struct {
	CallID string `json:"call_id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}
