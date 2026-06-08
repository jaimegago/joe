package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/safety"
)

// Write-posture tri-state. These are the only values the posture endpoint
// reports; they map one-to-one onto the boot-resolved write floor's reason
// (D-0018), translating the floor's empty FloorReasonNone into the explicit
// "normal" so callers never have to interpret an empty string.
const (
	// postureNormal — the floor is down; Joe may mutate the managed system.
	postureNormal = "normal"
	// postureObservation — the floor is up because Joe booted into observation
	// mode (JOE_MODE=observation). A calm, intended read-only posture.
	postureObservation = "observation"
	// postureSafeMode — the floor is up because a sticky panic state was present
	// at boot. Read-only until the panic state is cleared and Joe is restarted.
	postureSafeMode = "safe_mode"
)

// postureHandler serves the read-only write-posture endpoint. It reports the
// boot-resolved write floor (D-0018) as a tri-state enum so callers (e.g. the
// Web UI banner, operators) can tell normal/observation/safe_mode apart without
// re-deriving the floor from disk or DB. Like the panic status handler it reads
// the single boot-resolved floor threaded from the Services; it never lowers or
// mutates anything — there is no write surface here.
type postureHandler struct {
	floor safety.WriteFloor
}

func (s *Server) registerPostureRoutes(mux *http.ServeMux, prefix string) {
	var floor safety.WriteFloor
	if s.services != nil {
		floor = s.services.WriteFloor
	}
	h := &postureHandler{floor: floor}
	mux.HandleFunc(fmt.Sprintf("GET %s/posture", prefix), h.handlePosture)
}

// writePostureResponse reports the resolved write posture. Explicit snake_case
// json tags mirror panicStatusResponse (not the tagless Regime struct).
type writePostureResponse struct {
	// Posture is the tri-state write posture: "normal", "observation", or
	// "safe_mode". It is derived from the floor's reason, with the floor's
	// empty FloorReasonNone surfaced as the explicit "normal".
	Posture string `json:"posture"`
	// Writable mirrors !floor.Up() — true exactly when posture is "normal". A
	// convenience for callers that only need "can Joe write?" and do not want to
	// enumerate the posture values themselves.
	Writable bool `json:"writable"`
}

// handlePosture returns the current write posture as a tri-state enum. It is a
// pure read of the boot-resolved floor; observation and safe_mode are reported
// distinctly (unlike panic status, which collapses observation into not-safe).
func (h *postureHandler) handlePosture(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, writePostureResponse{
		Posture:  postureFromReason(h.floor.Reason()),
		Writable: !h.floor.Up(),
	})
}

// postureFromReason translates the floor reason into the tri-state enum,
// mapping FloorReasonNone ("") to the explicit "normal".
func postureFromReason(reason safety.FloorReason) string {
	switch reason {
	case safety.FloorReasonObservation:
		return postureObservation
	case safety.FloorReasonSafeMode:
		return postureSafeMode
	default:
		return postureNormal
	}
}
