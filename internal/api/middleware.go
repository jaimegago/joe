package api

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultMaxRequestBytes is the default maximum request body size (1 MB).
	DefaultMaxRequestBytes int64 = 1 << 20

	// errorCodeUnauthorized is returned when the Bearer token is missing or invalid.
	errorCodeUnauthorized = "unauthorized"

	// errorCodeRateLimited is returned when the per-IP rate limit is exceeded.
	errorCodeRateLimited = "rate_limited"

	// rateLimiterTTL is how long an idle IP entry is kept before cleanup.
	rateLimiterTTL = 5 * time.Minute
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

// CORS returns middleware that adds permissive CORS headers for local
// development. It allows requests from any origin and handles OPTIONS
// preflight requests so the browser does not block cross-origin calls
// from the Vite dev server (localhost:5173) to joecored (localhost:7777).
func CORS() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
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

// ipLimiter holds a token-bucket limiter and the last time it was accessed.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimitStore tracks per-IP limiters with periodic cleanup.
type rateLimitStore struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	rps      rate.Limit
	burst    int
}

func newRateLimitStore(rps float64, burst int) *rateLimitStore {
	s := &rateLimitStore{
		limiters: make(map[string]*ipLimiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go s.cleanup()
	return s
}

func (s *rateLimitStore) allow(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.limiters[ip]
	if !ok {
		l = &ipLimiter{limiter: rate.NewLimiter(s.rps, s.burst)}
		s.limiters[ip] = l
	}
	l.lastSeen = time.Now()
	return l.limiter.Allow()
}

// cleanup removes entries that haven't been seen for rateLimiterTTL.
func (s *rateLimitStore) cleanup() {
	ticker := time.NewTicker(rateLimiterTTL)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-rateLimiterTTL)
		s.mu.Lock()
		for ip, l := range s.limiters {
			if l.lastSeen.Before(cutoff) {
				delete(s.limiters, ip)
			}
		}
		s.mu.Unlock()
	}
}

// RateLimit returns middleware that enforces a per-IP token-bucket rate limit.
// rps is the sustained requests-per-second allowed per IP; burst is the
// maximum instantaneous burst. If rps <= 0, the middleware is a no-op.
// Requests that exceed the limit receive HTTP 429 Too Many Requests.
func RateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	if rps <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	if burst <= 0 {
		burst = 1
	}
	store := newRateLimitStore(rps, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := remoteIP(r)
			if !store.allow(ip) {
				slog.Warn("api rate limit exceeded", "ip", ip, "path", r.URL.Path)
				writeError(w, http.StatusTooManyRequests, errorCodeRateLimited,
					"rate limit exceeded — please slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// remoteIP extracts the client IP from the request, stripping the port.
func remoteIP(r *http.Request) string {
	// Check X-Forwarded-For first (set by reverse proxies).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first (leftmost) IP which is the original client.
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
