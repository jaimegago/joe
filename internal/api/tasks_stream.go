package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/agentctx"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/uid"
)

// SSE event names emitted by the streaming task endpoint.
const (
	sseEventStep  = "step"  // one agentic-loop iteration completed
	sseEventFinal = "final" // terminal event carrying the full taskResponse
)

// sseWriter serializes Server-Sent Events to the response and flushes each one
// so the client renders incrementally. The mutex guards concurrent writers; in
// Phase 2's streaming-only path all writes are on the loop goroutine, but the
// lock documents intent and protects the local-tool callback path added later.
type sseWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
	f  http.Flusher
}

func (s *sseWriter) event(event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// streamObserver emits an SSE step event for each loop iteration and also
// collects the records so the handler can assemble the final response (token
// totals, tools-used, redaction). The first SSE write error is retained to
// stop emitting once the client has disconnected.
type streamObserver struct {
	sse   *sseWriter
	steps []agentloop.StepRecord
	err   error
}

func (o *streamObserver) OnStep(step agentloop.StepRecord) {
	o.steps = append(o.steps, step)
	if o.err == nil {
		o.err = o.sse.event(sseEventStep, taskStepFromRecord(step))
	}
}

// handleTaskStream runs the single agentic loop and streams its progress to the
// client as SSE. It shares construction (buildTaskRun) and finalization
// (finalizeTaskResponse) with the non-streaming /tasks handler; the difference
// is incremental step events plus multi-turn history seeding.
func (h *taskHandler) handleTaskStream(w http.ResponseWriter, r *http.Request) {
	if h.server.services.LLM == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "LLM not available")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errorCodeInternal, "streaming not supported")
		return
	}

	var req taskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "message is required")
		return
	}

	// Owner-scope a continued session before committing to the SSE stream
	// (§11 Phase 1): a non-owner must not seed/read another user's history.
	if !h.sessionAccessAllowed(r.Context(), req.SessionID, string(rbac.PrincipalFromContext(r.Context()))) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "session not found")
		return
	}

	timeout := 5 * time.Minute
	if req.Config != nil && req.Config.Timeout != "" {
		parsed, err := time.ParseDuration(req.Config.Timeout)
		if err != nil {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, fmt.Sprintf("invalid timeout: %s", err))
			return
		}
		timeout = parsed
	}

	maxIterations, err := resolveMaxIterations(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, err.Error())
		return
	}

	// Once we commit to streaming, all further signalling is via SSE events;
	// the status line is 200 and errors travel in the final event.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sse := &sseWriter{w: w, f: flusher}
	observer := &streamObserver{sse: sse}
	prepared := h.buildTaskRun(r.Context(), req, maxIterations, observer)
	defer prepared.session.Close()

	// Multi-turn continuity: seed prior conversation for this session.
	h.seedHistory(r.Context(), prepared.session, prepared.sessionID)

	taskID := uid.New()

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	// Stream G phase G2: thread the prepared session id and the freshly
	// minted task id into context BEFORE the agentic loop runs so the
	// usage recorder can read them when persisting each per-call row.
	ctx = agentctx.WithSessionID(ctx, prepared.sessionID)
	ctx = agentctx.WithTaskID(ctx, taskID)

	start := time.Now()
	answer, runErr := prepared.agent.Run(ctx, prepared.session, req.Message)
	duration := time.Since(start)

	status, errMsg := taskStatus(ctx, runErr)
	if errors.Is(runErr, llm.ErrContextOverflow) {
		h.writeContextOverflowAudit(ctx, prepared)
	}
	h.persistTaskMessages(r.Context(), prepared.sessionID, req.Message, answer, prepared.session.StopReason(), start)
	resp := finalizeTaskResponse(taskID, prepared.sessionID, status, errMsg, answer, observer.steps, prepared.session, prepared.caps.ContextWindowTokens, duration)

	slog.Info("task stream completed",
		"task_id", taskID,
		"session_id", prepared.sessionID,
		"status", status,
		"iterations", resp.Iterations,
		"duration_ms", resp.DurationMs,
	)

	_ = sse.event(sseEventFinal, resp)
}
