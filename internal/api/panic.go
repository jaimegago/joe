package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jaimegago/joe/internal/safety"
)

type panicHandler struct {
	// floor is the boot-resolved write floor (D-0018), threaded from the
	// Services. The status endpoint reports safe mode from the floor's reason —
	// it does NOT re-derive the floor from disk or DB. There is deliberately no
	// unlock endpoint: the floor cannot be lowered via the API (the surface is
	// eliminated, not protected); recovery is the local `joe unlock` CLI (which
	// clears the panic DB row) plus a restart.
	floor safety.WriteFloor
	// panicInfo reads the trigger detail from the single panic DB row to enrich
	// the status response. It is the DB-row replacement for the deleted
	// panic.state file read. nil when no store is wired (e.g. in tests).
	panicInfo func(ctx context.Context) (*safety.PanicInfo, error)
}

func (s *Server) registerPanicRoutes(mux *http.ServeMux, prefix string) {
	var floor safety.WriteFloor
	var panicInfo func(context.Context) (*safety.PanicInfo, error)
	if s.services != nil {
		floor = s.services.WriteFloor
		if s.services.Store != nil {
			panicInfo = s.services.Store.PanicStore().PanicInfo
		}
	}
	h := &panicHandler{floor: floor, panicInfo: panicInfo}
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

// handleTriggerPanic sets the global panic flag, persists the panic to the
// single cluster_panic_state DB row (via the boot-registered cluster store
// inside safety.Trigger), and schedules a process exit with code 2. The response
// is sent before the exit. There is no panic.state file write — panic state has
// one home (the DB row), read by boot to raise the safe-mode floor on restart.
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

	writeJSON(w, http.StatusOK, panicResponse{
		Acknowledged: true,
		Message:      "emergency shutdown initiated — joe will restart in safe mode",
	})

	// Flush the response before exiting. The goroutine gives the HTTP server
	// time to send the response body before we terminate the process.
	go func() {
		time.Sleep(150 * time.Millisecond)
		os.Exit(2)
	}()
}

// handlePanicStatus returns the current safe mode status. Safe mode is the
// floor's safe_mode reason (panic recovery) — the calm observation posture is
// NOT reported as safe mode here. Reads the boot-resolved floor; trigger detail
// is enriched from the single panic DB row, never from disk.
func (h *panicHandler) handlePanicStatus(w http.ResponseWriter, r *http.Request) {
	inSafeMode := h.floor.Reason() == safety.FloorReasonSafeMode
	if !inSafeMode && !safety.IsPanicked() {
		writeJSON(w, http.StatusOK, panicStatusResponse{SafeMode: false})
		return
	}

	if h.panicInfo != nil {
		if info, err := h.panicInfo(r.Context()); err == nil && info != nil {
			writeJSON(w, http.StatusOK, panicStatusResponse{
				SafeMode:      true,
				TriggeredAt:   info.TriggeredAt,
				TriggerSource: string(info.TriggerSource),
				TriggerReason: info.TriggerReason,
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, panicStatusResponse{SafeMode: true})
}
