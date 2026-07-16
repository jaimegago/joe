package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
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

	// Seed a prior turn for session "s1" directly in the session model. The
	// session is owned by the unauthenticated test principal (rbac.Unknown) so
	// the handler's owner-scope check admits the continue.
	ctx := context.Background()
	if _, err := srv.services.SessionModel.CreateSession(ctx, sessionmodel.AgentSession{
		ID:               "s1",
		Type:             sessionmodel.SessionTypeDefault,
		CreatorPrincipal: string(rbac.Unknown),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, m := range []sessionmodel.ChatMessage{
		{ID: "m1", SessionID: "s1", Role: "user", Content: "q1"},
		{ID: "m2", SessionID: "s1", Role: "assistant", Content: "a1"},
	} {
		if _, err := srv.services.SessionModel.AddChatMessage(ctx, m); err != nil {
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

// erroringLLM returns a fixed error from Chat so a test can drive the run's
// terminal error path (cancellation, timeout, or a generic failure).
type erroringLLM struct{ err error }

func (l *erroringLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, l.err
}

// streamedSessionID pulls the session id off the final SSE event so the test can
// read back the persisted transcript.
func streamedSessionID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, e := range parseRecordedSSE(rec.Body.String()) {
		if e.event == sseEventFinal {
			var resp taskResponse
			if err := json.Unmarshal([]byte(e.data), &resp); err != nil {
				t.Fatalf("decode final: %v (data=%s)", err, e.data)
			}
			return resp.SessionID
		}
	}
	t.Fatal("no final SSE event emitted")
	return ""
}

func assistantMessages(t *testing.T, srv *Server, sessionID string) []sessionmodel.ChatMessage {
	t.Helper()
	msgs, err := srv.services.SessionModel.ListChatMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	var assistants []sessionmodel.ChatMessage
	for _, m := range msgs {
		if m.Role == "assistant" {
			assistants = append(assistants, m)
		}
	}
	return assistants
}

// A run the caller aborted (context.Canceled) persists a minimal assistant
// marker row carrying stop_reason "cancelled", so a reloaded session shows the
// turn was stopped rather than a question with no reply.
func TestHandleTaskStream_ContextCanceled_PersistsCancelledMarker(t *testing.T) {
	srv, mux := setupTaskServer(t, &erroringLLM{err: context.Canceled})

	rec := postStream(t, mux, map[string]any{"message": "investigate"})
	sessionID := streamedSessionID(t, rec)

	assistants := assistantMessages(t, srv, sessionID)
	if len(assistants) != 1 {
		t.Fatalf("assistant rows = %d, want 1 (the cancelled marker)", len(assistants))
	}
	if assistants[0].StopReason != agentloop.StopReasonCancelled {
		t.Errorf("stop_reason = %q, want %q", assistants[0].StopReason, agentloop.StopReasonCancelled)
	}
	if assistants[0].Content == "" {
		t.Error("cancelled marker content is empty; want a minimal honest body")
	}
}

// The timeout path (DeadlineExceeded) is deliberately NOT treated as a
// cancellation: it persists no assistant row, exactly as before.
func TestHandleTaskStream_DeadlineExceeded_NoCancelledMarker(t *testing.T) {
	srv, mux := setupTaskServer(t, &erroringLLM{err: context.DeadlineExceeded})

	rec := postStream(t, mux, map[string]any{"message": "slow"})
	sessionID := streamedSessionID(t, rec)

	if n := len(assistantMessages(t, srv, sessionID)); n != 0 {
		t.Errorf("assistant rows = %d, want 0 (DeadlineExceeded must not persist a marker)", n)
	}
}

// Every other terminal error keeps the existing skip-when-empty behavior: no
// answer means no assistant row, and certainly no cancelled marker.
func TestHandleTaskStream_GenericError_NoAssistantRow(t *testing.T) {
	srv, mux := setupTaskServer(t, &erroringLLM{err: errors.New("boom")})

	rec := postStream(t, mux, map[string]any{"message": "go"})
	sessionID := streamedSessionID(t, rec)

	if n := len(assistantMessages(t, srv, sessionID)); n != 0 {
		t.Errorf("assistant rows = %d, want 0 (a generic error persists no assistant row)", n)
	}
}
