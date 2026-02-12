package api

const (
	// API version and status values.
	version      = "0.1.0"
	statusOK     = "ok"
	errorNotImpl = "not implemented"

	// HTTP Content-Type header value.
	contentTypeJSON = "application/json"

	// Error codes for structured error responses.
	errorCodeInternal      = "internal_error"
	errorCodeInvalidRequest = "invalid_request"
	errorCodeNotFound       = "not_found"
	errorCodeInvalidSource  = "invalid_source"
	errorCodeNotImplemented = "not_implemented"

	internalErrorMessage = "internal server error"
)
