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

	internalErrorMessage = "internal server error"
)
