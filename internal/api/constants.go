package api

const (
	// API version and status values.
	defaultVersion = "dev"
	apiVersion     = "v1"      // Current API version for route paths
	apiPrefix      = "/api/v1" // Full API path prefix
	statusOK       = "ok"

	// HTTP Content-Type header value.
	contentTypeJSON = "application/json"

	// Error codes for structured error responses.
	errorCodeInternal           = "internal_error"
	errorCodeInvalidRequest     = "invalid_request"
	errorCodeNotFound           = "not_found"
	errorCodeInvalidSource      = "invalid_source"
	errorCodeServiceUnavailable = "service_unavailable"
	errorCodeForbidden          = "forbidden"

	// Typed write-failure codes (Item 8 / differentiated write-failure
	// feedback). A denied write surfaces one of these so the chat UI can show
	// a specific message instead of a generic error. errorCodeInternal
	// ("internal_error") above doubles as the fallback bucket.
	errorCodeZoneDenial   = "zone_denial"   // RBAC: caller lacks access to the target zone
	errorCodeIncidentMode = "incident_mode" // captain gate refused: system in incident mode

	internalErrorMessage = "internal server error"
)
