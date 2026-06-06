package api

import (
	"errors"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/safety"
)

// classifyWriteFailure maps a per-tool execution error to a stable,
// machine-readable write-failure code the chat UI dispatches on (DECISIONS.md
// D-0014, write-failure feedback). The two distinguishable denials:
//
//   - incident_mode — the captain gate refused a mutation because the system
//     is in incident regime and the calling session is not the captain
//     (*captaingate.GateRefusalError).
//   - zone_denial — the RBAC accessor refused because the caller lacks access
//     to the target zone (access.ErrPermissionDenied, possibly wrapped by the
//     inproc client's mapAccessError).
//   - safe_mode — the executor refused a T2/T3 tool because the system is in
//     safe mode (panic recovery), where only read-only (T1) tools are allowed
//     (safety.ErrSafeModeActive). Distinct from incident_mode: safe mode is the
//     panic axis, incident_mode is the captain-gate axis; both can be active.
//
// Anything else returns "" — NOT every tool error is a write denial (a
// malformed-args or upstream-timeout error is an ordinary tool failure the LLM
// handles, not an authorization signal). The empty result keeps those out of
// the differentiated-feedback path; the turn-level fallback to a generic
// message is applied by the frontend only when it has a non-empty code.
//
// This lives in the api layer (not agentloop) because it owns the gate/RBAC
// error vocabulary; it is injected into the loop via
// agentloop.WithToolErrorClassifier so the loop stays unaware of these types.
// It runs on the TYPED error before it is stringified onto the wire.
func classifyWriteFailure(err error) string {
	if err == nil {
		return ""
	}
	var refusal *captaingate.GateRefusalError
	switch {
	case errors.As(err, &refusal):
		return errorCodeIncidentMode
	case errors.Is(err, access.ErrPermissionDenied):
		return errorCodeZoneDenial
	case errors.Is(err, safety.ErrSafeModeActive):
		return errorCodeSafeMode
	default:
		return ""
	}
}
