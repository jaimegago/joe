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
