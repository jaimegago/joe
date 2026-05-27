package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// recordingLLM records each ChatRequest and returns a canned no-tool answer.
type recordingLLM struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	answer   string
}

func (r *recordingLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	return &llm.ChatResponse{
		Content: r.answer,
		Usage:   llm.TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
	}, nil
}

func (r *recordingLLM) ChatStream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func (r *recordingLLM) Embed(context.Context, string) ([]float32, error) { return []float32{0.1}, nil }

func (r *recordingLLM) firstRequest(t *testing.T) llm.ChatRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("LLM received no requests")
	}
	return r.requests[0]
}

type sseEvent struct {
	event string
	data  string
}

// parseRecordedSSE splits an SSE response body into ordered (event, data) pairs.
func parseRecordedSSE(body string) []sseEvent {
	var events []sseEvent
	var cur sseEvent
	for _, line := range strings.Split(body, "\n") {
		switch {
		case line == "":
			if cur.event != "" || cur.data != "" {
				events = append(events, cur)
				cur = sseEvent{}
			}
		case strings.HasPrefix(line, "event:"):
			cur.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			cur.data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if cur.event != "" || cur.data != "" {
		events = append(events, cur)
	}
	return events
}

func postStream(t *testing.T, mux *http.ServeMux, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/tasks/stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandleTaskStream_StepAndFinal(t *testing.T) {
	_, mux := setupTaskServer(t, &recordingLLM{answer: "Hello stream"})

	rec := postStream(t, mux, map[string]any{"message": "hi"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	events := parseRecordedSSE(rec.Body.String())
	if len(events) < 2 {
		t.Fatalf("expected >=2 SSE events (step + final), got %d: %s", len(events), rec.Body.String())
	}

	var sawStep bool
	var final *taskResponse
	for _, e := range events {
		switch e.event {
		case sseEventStep:
			sawStep = true
		case sseEventFinal:
			var resp taskResponse
			if err := json.Unmarshal([]byte(e.data), &resp); err != nil {
				t.Fatalf("decode final: %v (data=%s)", err, e.data)
			}
			final = &resp
		}
	}

	if !sawStep {
		t.Error("no step event emitted")
	}
	if final == nil {
		t.Fatal("no final event emitted")
	}
	if final.Status != "completed" {
		t.Errorf("final status = %q, want completed", final.Status)
	}
	if final.FinalAnswer != "Hello stream" {
		t.Errorf("final answer = %q, want %q", final.FinalAnswer, "Hello stream")
	}
	if final.SessionID == "" {
		t.Error("final session_id is empty")
	}
}

func TestHandleTaskStream_MultiTurnHistory(t *testing.T) {
	llmRec := &recordingLLM{answer: "a2"}
	srv, mux := setupTaskServer(t, llmRec)

	// Seed a prior turn for session "s1" directly in the store.
	ctx := context.Background()
	if err := srv.services.Store.Sessions.Create(ctx, &store.Session{ID: "s1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, m := range []store.SessionMessage{
		{SessionID: "s1", Role: "user", Content: "q1", CreatedAt: time.Now().UTC()},
		{SessionID: "s1", Role: "assistant", Content: "a1", CreatedAt: time.Now().UTC()},
	} {
		mm := m
		if err := srv.services.Store.Sessions.AddMessage(ctx, &mm); err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}

	rec := postStream(t, mux, map[string]any{"message": "q2", "session_id": "s1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The loop must have seen prior history followed by the new message.
	msgs := llmRec.firstRequest(t).Messages
	if len(msgs) != 3 {
		t.Fatalf("LLM saw %d messages, want 3 (q1,a1,q2): %+v", len(msgs), msgs)
	}
	want := []struct{ role, content string }{
		{"user", "q1"}, {"assistant", "a1"}, {"user", "q2"},
	}
	for i, w := range want {
		if msgs[i].Role != w.role || msgs[i].Content != w.content {
			t.Errorf("message[%d] = {%q,%q}, want {%q,%q}", i, msgs[i].Role, msgs[i].Content, w.role, w.content)
		}
	}
}

func TestHandleTaskStream_EmptyMessageRejected(t *testing.T) {
	_, mux := setupTaskServer(t, &recordingLLM{answer: "x"})
	rec := postStream(t, mux, map[string]any{"message": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
