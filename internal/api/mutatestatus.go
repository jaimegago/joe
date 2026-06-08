package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/safety"
)

// Mutate-status reason vocabulary. These are the ONLY three values the
// mutate-status endpoint emits on the wire; each maps one-to-one onto the
// boot-resolved write floor's reason (D-0018/D-0019). The floor's empty
// FloorReasonNone is translated to the explicit "full" so the wire value is
// never the empty string.
const (
	// mutateReasonFull — the floor is down; Joe is in full mode and may mutate
	// the managed system (RBAC still governs). Maps from FloorReasonNone ("").
	// "full" aligns with D-0019's "full mode" vocabulary.
	mutateReasonFull = "full"
	// mutateReasonObservation — the floor is up because Joe booted into
	// observation mode (JOE_MODE=observation). A calm, intended read-only mode.
	mutateReasonObservation = "observation"
	// mutateReasonSafeMode — the floor is up because a sticky panic state was
	// present at boot. Read-only until the panic state is cleared and Joe is
	// restarted.
	mutateReasonSafeMode = "safe_mode"
)

// mutateStatusHandler serves the read-only mutate-status endpoint. It reports
// whether Joe may currently mutate the managed system and why, derived from the
// boot-resolved write floor (D-0018/D-0019). Like the panic status handler it
// reads the single boot-resolved floor threaded from the Services; it never
// lowers or mutates anything — there is no write surface here.
type mutateStatusHandler struct {
	floor safety.WriteFloor
}

func (s *Server) registerMutateStatusRoutes(mux *http.ServeMux, prefix string) {
	var floor safety.WriteFloor
	if s.services != nil {
		floor = s.services.WriteFloor
	}
	h := &mutateStatusHandler{floor: floor}
	mux.HandleFunc(fmt.Sprintf("GET %s/mutate-status", prefix), h.handleMutateStatus)
}

// mutateStatusResponse reports whether Joe may mutate and why. Explicit
// snake_case json tags mirror panicStatusResponse (not the tagless Regime
// struct).
type mutateStatusResponse struct {
	// CanMutate mirrors !floor.Up() — true exactly when the floor is down
	// (full mode).
	CanMutate bool `json:"can_mutate"`
	// Reason is one of "full", "observation", or "safe_mode" — ALWAYS a
	// non-empty string. The floor's empty FloorReasonNone is surfaced as the
	// explicit "full"; "" is never emitted on the wire.
	Reason string `json:"reason"`
}

// handleMutateStatus returns the current mutate status. Both fields are
// computed from a SINGLE read of the boot-resolved floor — can_mutate from
// Up(), reason from Reason() — so they cannot disagree and the floor is not
// read twice or re-resolved.
func (h *mutateStatusHandler) handleMutateStatus(w http.ResponseWriter, r *http.Request) {
	floor := h.floor
	writeJSON(w, http.StatusOK, mutateStatusResponse{
		CanMutate: !floor.Up(),
		Reason:    mutateReasonFromFloor(floor.Reason()),
	})
}

// mutateReasonFromFloor translates the typed floor reason into the wire
// vocabulary via an explicit switch — never by stringifying the typed reason —
// so an unexpected reason value cannot leak onto the wire. FloorReasonNone ("")
// maps to the explicit "full". For an unrecognized reason it preserves the
// prior task's defensive choice: fall through to the floor-down value ("full",
// the renamed equivalent of the prior task's "normal" default).
func mutateReasonFromFloor(reason safety.FloorReason) string {
	switch reason {
	case safety.FloorReasonObservation:
		return mutateReasonObservation
	case safety.FloorReasonSafeMode:
		return mutateReasonSafeMode
	case safety.FloorReasonNone:
		return mutateReasonFull
	default:
		return mutateReasonFull
	}
}
