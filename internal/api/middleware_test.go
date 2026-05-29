package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------- MaxRequestBody ----------

func TestMaxRequestBody_WithinLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if string(body) != "hello" {
			t.Errorf("body = %q, want 'hello'", body)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := MaxRequestBody(1024)(inner)
	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestMaxRequestBody_ExceedsLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err == nil {
			t.Error("expected error when reading oversized body")
			return
		}
		// http.MaxBytesReader returns a specific error
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	})

	handler := MaxRequestBody(5)(inner) // limit to 5 bytes
	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader("this body exceeds the limit"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got status %d, want 413", rec.Code)
	}
}

func TestMaxRequestBody_DefaultLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// 0 should use default (1 MB)
	handler := MaxRequestBody(0)(inner)
	req := httptest.NewRequest("POST", "/api/v1/sources", strings.NewReader("small"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestMaxRequestBody_NilBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := MaxRequestBody(1024)(inner)
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

// ---------- Chain ----------

func TestChain_Order(t *testing.T) {
	var order []string

	mw := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+"-before")
				next.ServeHTTP(w, r)
				order = append(order, name+"-after")
			})
		}
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	handler := Chain(inner, mw("first"), mw("second"))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if len(order) != len(expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

// ---------- RateLimit ----------

func TestRateLimit_Disabled(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimit(0, 5)(inner)
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: got %d, want 200 (rate limit disabled)", i, rec.Code)
		}
	}
}

func TestRateLimit_AllowsWithinBurst(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// 1 rps, burst 5 — first 5 requests should pass immediately.
	handler := RateLimit(1, 5)(inner)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		req.RemoteAddr = "10.0.0.1:9000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d within burst: got %d, want 200", i, rec.Code)
		}
	}
}

func TestRateLimit_BlocksOverLimit(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Very low rate: 0.001 rps, burst 1.
	// After the first request, subsequent ones should be rate-limited immediately.
	handler := RateLimit(0.001, 1)(inner)

	var limited int
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/v1/status", nil)
		req.RemoteAddr = "192.168.1.1:4321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Error("expected at least one 429 response, got none")
	}
}

func TestRateLimit_PerIP(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// burst=1 so each IP gets exactly one free token.
	handler := RateLimit(0.001, 1)(inner)

	ips := []string{"1.2.3.4:100", "5.6.7.8:200", "9.10.11.12:300"}
	for _, ip := range ips {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("first request from %s got %d, want 200", ip, rec.Code)
		}
	}
}

func TestRateLimit_XForwardedFor(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimit(0.001, 1)(inner)

	// First request with X-Forwarded-For should pass (uses the header IP for limiting).
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "proxy.internal:80"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rec.Code)
	}
}

func TestRateLimit_429ResponseBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimit(0.001, 1)(inner)

	// Exhaust the single token.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "172.16.0.1:999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			body := rec.Body.String()
			if !strings.Contains(body, "rate_limited") {
				t.Errorf("429 body = %q, want 'rate_limited'", body)
			}
			return
		}
	}
	t.Error("expected a 429 response but never got one")
}

func TestRemoteIP_SplitsPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.0.1:12345"
	if got := remoteIP(req); got != "192.168.0.1" {
		t.Errorf("remoteIP() = %q, want 192.168.0.1", got)
	}
}

func TestRemoteIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "10.1.2.3, 172.16.0.1")
	if got := remoteIP(req); got != "10.1.2.3" {
		t.Errorf("remoteIP() = %q, want 10.1.2.3", got)
	}
}

// TestRemoteIP_NoPort covers the fallback path when RemoteAddr has no colon.
func TestRemoteIP_NoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.0.1" // no port
	if got := remoteIP(req); got != "192.168.0.1" {
		t.Errorf("remoteIP() = %q, want 192.168.0.1", got)
	}
}

// ---------- CORS ----------

func TestCORS_SetsHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CORS()(inner)
	req := httptest.NewRequest("GET", "/api/v1/graph", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Access-Control-Allow-Methods header missing")
	}
}

func TestCORS_PreflightReturns204(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("inner handler should not be called for OPTIONS")
	})
	handler := CORS()(inner)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/graph", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204 for OPTIONS preflight", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Access-Control-Allow-Headers header missing in preflight response")
	}
}
