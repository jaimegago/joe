package api

const (
	// API version and status values. The reported build version now comes from
	// internal/buildinfo (read by handleStatus / handleVersion), not a constant
	// here.
	apiVersion = "v1"      // Current API version for route paths
	apiPrefix  = "/api/v1" // Full API path prefix
	statusOK   = "ok"

	// HTTP Content-Type header value.
	contentTypeJSON = "application/json"

	// Error codes for structured error responses.
	errorCodeInternal           = "internal_error"
	errorCodeInvalidRequest     = "invalid_request"
	errorCodeNotFound           = "not_found"
	errorCodeInvalidComponent   = "invalid_component"
	errorCodeServiceUnavailable = "service_unavailable"
	errorCodeForbidden          = "forbidden"
	errorCodeConflict           = "conflict"

	// Typed tool-failure codes (Item 8 / differentiated write-failure
	// feedback). A refused tool call surfaces one of these so the chat UI can
	// show a specific message instead of a generic error. errorCodeInternal
	// ("internal_error") above doubles as the fallback bucket.
	//
	// "tool-failure", not "write-failure": only incident_mode, safe_mode and
	// observation are write-only (the captain gate and the write floor both key
	// on ActionMutate). zone_denial and scope_denial fire on reads too — the
	// RBAC accessor guards read seams with rbac.ActionRead, and the executor's
	// scope check runs ahead of the action class entirely. classifyWriteFailure
	// keeps its name for continuity; the channel it feeds is wider than that.
	errorCodeZoneDenial   = "zone_denial"   // RBAC: caller lacks access to the target zone
	errorCodeScopeDenial  = "scope_denial"  // executor scope check: target component/namespace outside the session's authorized scope
	errorCodeIncidentMode = "incident_mode" // captain gate refused: system in incident mode
	errorCodeSafeMode     = "safe_mode"     // write floor up, reason safe_mode: panic recovery (T1-only)
	errorCodeObservation  = "observation"   // write floor up, reason observation: intended read-only posture

	internalErrorMessage = "internal server error"
)
