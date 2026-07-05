package api

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultMaxRequestBytes is the default maximum request body size (1 MB).
	DefaultMaxRequestBytes int64 = 1 << 20

	// errorCodeRateLimited is returned when the per-IP rate limit is exceeded.
	errorCodeRateLimited = "rate_limited"

	// rateLimiterTTL is how long an idle IP entry is kept before cleanup.
	rateLimiterTTL = 5 * time.Minute
)

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

// cleanup removes entries that haven't been seen for rateLimiterTTL. The
// production boot constructs exactly one store for the process lifetime, so the
// goroutine runs until exit by design (no shutdown seam is plumbed for it).
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

// remoteIP extracts the rate-limit key from the request: the TCP peer address,
// stripped of its port. It deliberately does NOT trust X-Forwarded-For. Joe
// listens directly on :7777 with no trusted reverse proxy in the default
// deployment, so an XFF header is fully client-controlled — honoring it would
// let any direct client bypass the limiter (fresh spoofed IP per request) and
// grow the limiter map without bound. Keying on RemoteAddr is spoof-proof; a
// deployment that does sit behind a trusted proxy gets per-proxy-IP limiting,
// a safe degradation. If trusted-proxy support is ever added, gate XFF parsing
// behind that explicit configuration.
func remoteIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
