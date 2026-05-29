package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorBody mirrors internal/api's error envelope ({"error","message"}) so the
// auth endpoints and the edge 401 are shape-compatible with the rest of the API.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorBody{Error: code, Message: message}); err != nil {
		slog.Error("auth: failed to encode error response", "error", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("auth: failed to encode JSON response", "error", err)
	}
}
