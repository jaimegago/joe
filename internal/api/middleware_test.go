package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------- BearerAuth ----------

func TestBearerAuth_NoKey_PassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := BearerAuth("")(inner)
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 (auth disabled)", rec.Code)
	}
}

func TestBearerAuth_ValidToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := BearerAuth("secret-token")(inner)
	req := httptest.NewRequest("GET", "/api/v1/graph/query", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestBearerAuth_MissingHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	})

	handler := BearerAuth("secret-token")(inner)
	req := httptest.NewRequest("GET", "/api/v1/graph/query", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "missing Authorization") {
		t.Errorf("body = %q, want 'missing Authorization'", body)
	}
}

func TestBearerAuth_WrongToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	})

	handler := BearerAuth("correct-token")(inner)
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "invalid or expired") {
		t.Errorf("body = %q, want 'invalid or expired'", body)
	}
}

func TestBearerAuth_CaseInsensitive(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := BearerAuth("MyToken")(inner)
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	req.Header.Set("Authorization", "bearer MyToken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 (case-insensitive match)", rec.Code)
	}
}

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

