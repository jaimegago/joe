package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/store"
)

type apiError struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string, details ...map[string]any) {
	var payloadDetails map[string]any
	if len(details) > 0 {
		payloadDetails = details[0]
	}
	writeJSON(w, status, apiError{Error: code, Message: message, Details: payloadDetails})
}

func writeInternalError(w http.ResponseWriter, err error, context string) {
	if err != nil {
		slog.Error("api error", "context", context, "error", err)
	}
	writeError(w, http.StatusInternalServerError, errorCodeInternal, internalErrorMessage)
}

func writeBadRequest(w http.ResponseWriter, err error, context, message string) {
	if err != nil {
		slog.Error("api bad request", "context", context, "error", err)
	}
	writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, message)
}

// writeAccessError maps transport-agnostic errors returned by the guarded
// accessor (internal/access) to HTTP responses, returning true when it has
// written one. It handles the permission-denied decision (the authoritative
// RBAC check now living in the accessor) and the graph-unavailable case.
// Returns false for any other error so the caller can apply its own mapping
// (e.g. writeInternalError for a genuine adapter/method failure).
func writeAccessError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, access.ErrPermissionDenied):
		// 403 body carries the structured RBAC decision reason (no_grant,
		// action_not_in_zone, …) under details.reason when the accessor returns
		// the typed *access.PermissionDeniedError, so a caller (or an operator
		// diagnosing a wiring bug) can see WHY without reading the audit table.
		// The message is unchanged; this is additive observability.
		var denied *access.PermissionDeniedError
		if errors.As(err, &denied) && denied.Reason != "" {
			writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied by RBAC policy",
				map[string]any{"reason": denied.Reason})
			return true
		}
		writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied by RBAC policy")
		return true
	case errors.Is(err, access.ErrGraphUnavailable):
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "graph store not available")
		return true
	default:
		return false
	}
}

// handleAccessError maps every error the guarded accessor can return when
// resolving a typed adapter: the permission decision (403), an unknown
// source (404), or a source whose adapter is the wrong type (400). It
// returns true when it has written a response. A method-level error from the
// underlying adapter (which is none of these sentinels) yields false, so the
// caller writes it via writeInternalError, preserving the previous behaviour
// where adapter call failures returned 500.
//
// expected is the human-readable adapter kind used in the wrong-type message
// (e.g. "k8s", "git", "aws"), matching the pre-Phase-A error text.
func handleAccessError(w http.ResponseWriter, err error, sourceID, expected string) bool {
	if err == nil {
		return false
	}
	if writeAccessError(w, err) {
		return true
	}
	if errors.Is(err, store.ErrComponentNotFound) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("component not found: %s", sourceID), map[string]any{
			"component_id": sourceID,
		})
		return true
	}
	if errors.Is(err, access.ErrWrongAdapterType) {
		displayType := expected
		switch displayType {
		case "aws":
			displayType = "AWS"
		case "k8s":
			displayType = "Kubernetes"
		case "git":
			displayType = "Git"
		}
		article := "a"
		if displayType == "AWS" {
			article = "an"
		}
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent, fmt.Sprintf("component is not %s %s adapter", article, displayType), map[string]any{
			"component_id": sourceID,
			"expected":     expected,
		})
		return true
	}
	return false
}
