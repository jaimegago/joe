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
//   - safe_mode / observation — the executor refused a Mutate because the
//     boot-resolved write floor is up (D-0018). The single *safety.WriteFloorError
//     carries a reason: safe_mode (panic recovery) or observation (the intended
//     read-only resting posture set via JOE_MODE=observation). Both surface a
//     distinct calm/recovery message; enforcement is one branch. Distinct from
//     incident_mode: the floor is the panic/observation axis, incident_mode is
//     the captain-gate axis; both can be active.
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
//
// PRECEDENCE (D-0022 / D-0019 decision 9: floor > incident > RBAC, ordered by
// resolvability depth). The branch order below MATCHES that precedence, but it
// is NOT what enforces it: enforcement short-circuits at the first failing
// check (the floor in tools.Executor / captaingate, the §C gate in captaingate,
// the RBAC accessor inside the tool), so exactly ONE typed error ever reaches
// this classifier. The three error types are mutually exclusive on a single
// err, so this switch only maps the one error that fired to its code — the
// precedence between them is decided upstream by check order, not here. The
// order is kept aligned with the precedence as documentation of intent.
func classifyWriteFailure(err error) string {
	if err == nil {
		return ""
	}
	var floorErr *safety.WriteFloorError
	var refusal *captaingate.GateRefusalError
	switch {
	case errors.As(err, &floorErr):
		// The write floor is one enforcement branch but two presentations: the
		// reason rides out of the error as data (D-0018 point 1). safe_mode and
		// observation never co-occur — the floor resolves to exactly one reason
		// at boot (safe_mode wins, ResolveWriteFloor), so "safe_mode >
		// observation within the floor" is settled before this point.
		if floorErr.Reason == safety.FloorReasonObservation {
			return errorCodeObservation
		}
		return errorCodeSafeMode
	case errors.As(err, &refusal):
		return errorCodeIncidentMode
	case errors.Is(err, access.ErrPermissionDenied):
		return errorCodeZoneDenial
	default:
		return ""
	}
}
