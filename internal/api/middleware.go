package api

import (
	"log/slog"
	"net/http"
	"strings"
)

const (
	// DefaultMaxRequestBytes is the default maximum request body size (1 MB).
	DefaultMaxRequestBytes int64 = 1 << 20

	// errorCodeUnauthorized is returned when the Bearer token is missing or invalid.
	errorCodeUnauthorized = "unauthorized"
)

// BearerAuth returns middleware that validates Authorization: Bearer <token>
// on all requests under the given prefix. If apiKey is empty, the middleware
// is a no-op (auth disabled).
func BearerAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if apiKey == "" {
			return next // auth disabled
		}
		expected := "Bearer " + apiKey
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				slog.Warn("api auth: missing Authorization header", "path", r.URL.Path, "remote", r.RemoteAddr)
				writeError(w, http.StatusUnauthorized, errorCodeUnauthorized, "missing Authorization header")
				return
			}
			if !strings.EqualFold(auth, expected) {
				slog.Warn("api auth: invalid token", "path", r.URL.Path, "remote", r.RemoteAddr)
				writeError(w, http.StatusUnauthorized, errorCodeUnauthorized, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxRequestBody returns middleware that limits the size of incoming request
// bodies. If maxBytes <= 0, DefaultMaxRequestBytes is used.
func MaxRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRequestBytes
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Chain applies a sequence of middleware to a handler, in order.
// The first middleware in the slice wraps outermost (runs first).
func Chain(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
