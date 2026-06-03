package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// TaskStreamRequest is the body for a streamed agentic turn.
type TaskStreamRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
}

// SSE event names emitted by joe's streaming task endpoint. These mirror
// the server-side names; the end-to-end test asserts they stay in sync.
const (
	TaskEventStep  = "step"
	TaskEventFinal = "final"
)

// TaskEvent is one decoded Server-Sent Event from the streaming task endpoint.
// Type is the SSE event name ("step", "final", ...); Data is its raw JSON
// payload, decoded by the caller according to Type.
type TaskEvent struct {
	Type string
	Data json.RawMessage
}

// StreamTask opens a streamed agentic turn against joe and invokes onEvent
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
