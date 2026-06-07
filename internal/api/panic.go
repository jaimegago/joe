package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/safety"
)

type panicHandler struct {
	joeDirFn func() (string, error)
	// floor is the boot-resolved write floor (D-0018), threaded from the
	// Services. The status endpoint reports safe mode from the floor's reason —
	// it does NOT re-derive the floor from disk. There is deliberately no unlock
	// endpoint: the floor cannot be lowered via the API (the surface is
	// eliminated, not protected); recovery is the local `joe unlock` CLI plus a
	// restart.
	floor safety.WriteFloor
}

func newPanicHandler(floor safety.WriteFloor) *panicHandler {
	return &panicHandler{joeDirFn: paths.JoeDirPath, floor: floor}
}

func (s *Server) registerPanicRoutes(mux *http.ServeMux, prefix string) {
	var floor safety.WriteFloor
	if s.services != nil {
		floor = s.services.WriteFloor
	}
	h := newPanicHandler(floor)
	mux.HandleFunc(fmt.Sprintf("POST %s/panic", prefix), h.handleTriggerPanic)
	mux.HandleFunc(fmt.Sprintf("GET %s/panic/status", prefix), h.handlePanicStatus)
}

type panicRequest struct {
	Reason string `json:"reason"`
}

type panicResponse struct {
	Acknowledged bool   `json:"acknowledged"`
	Message      string `json:"message"`
}

type panicStatusResponse struct {
	SafeMode      bool      `json:"safe_mode"`
	TriggeredAt   time.Time `json:"triggered_at,omitempty"`
	TriggerSource string    `json:"trigger_source,omitempty"`
	TriggerReason string    `json:"trigger_reason,omitempty"`
}

// handleTriggerPanic sets the global panic flag, persists panic.state, and
// schedules a process exit with code 2. The response is sent before the exit.
func (h *panicHandler) handleTriggerPanic(w http.ResponseWriter, r *http.Request) {
	var req panicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Body is optional — empty body is fine
		req.Reason = ""
	}

	if !safety.Trigger(safety.PanicSourceAPI, req.Reason) {
		// Already panicked — idempotent
		writeJSON(w, http.StatusOK, panicResponse{
			Acknowledged: true,
			Message:      "already in emergency shutdown mode",
		})
		return
	}

	// Persist panic state so joecored restarts in safe mode.
	joeDir, err := h.joeDirFn()
	if err != nil {
		slog.Error("panic: failed to resolve joe dir", "error", err)
	} else {
		state := safety.PanicState{
			TriggeredAt:   time.Now().UTC(),
			TriggerSource: safety.PanicSourceAPI,
			TriggerReason: req.Reason,
		}
		if err := safety.WritePanicState(joeDir, state); err != nil {
			slog.Error("panic: failed to write panic.state", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, panicResponse{
		Acknowledged: true,
		Message:      "emergency shutdown initiated — joecored will restart in safe mode",
	})

	// Flush the response before exiting. The goroutine gives the HTTP server
	// time to send the response body before we terminate the process.
	go func() {
		time.Sleep(150 * time.Millisecond)
		slog.Info("panic: process exiting with code 2")
		os.Exit(2)
	}()
}

// handlePanicStatus returns the current safe mode status. Safe mode is the
// floor's safe_mode reason (panic recovery) — the calm observation posture is
// NOT reported as safe mode here. Reads the boot-resolved floor, never disk.
func (h *panicHandler) handlePanicStatus(w http.ResponseWriter, r *http.Request) {
	inSafeMode := h.floor.Reason() == safety.FloorReasonSafeMode
	if !inSafeMode && !safety.IsPanicked() {
		writeJSON(w, http.StatusOK, panicStatusResponse{SafeMode: false})
		return
	}

	joeDir, err := h.joeDirFn()
	if err != nil {
		writeJSON(w, http.StatusOK, panicStatusResponse{SafeMode: true})
		return
	}

	state, err := safety.ReadPanicState(joeDir)
	if err != nil || state == nil {
		writeJSON(w, http.StatusOK, panicStatusResponse{SafeMode: true})
		return
	}

	writeJSON(w, http.StatusOK, panicStatusResponse{
		SafeMode:      true,
		TriggeredAt:   state.TriggeredAt,
		TriggerSource: string(state.TriggerSource),
		TriggerReason: state.TriggerReason,
	})
}
