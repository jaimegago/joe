package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

func TestAPIError_Error(t *testing.T) {
	t.Run("with code", func(t *testing.T) {
		err := (&APIError{Status: 404, Code: "not_found", Message: "missing"}).Error()
		if err != "api error (404 not_found): missing" {
			t.Fatalf("unexpected message: %q", err)
		}
	})

	t.Run("without code", func(t *testing.T) {
		err := (&APIError{Status: 500, RawBody: "oops"}).Error()
		if err != "api error (500): oops" {
			t.Fatalf("unexpected message: %q", err)
		}
	})
}

func TestParseAPIError(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "empty", body: "", ok: false},
		{name: "invalid json", body: "{", ok: false},
		{name: "missing fields", body: `{"foo":"bar"}`, ok: false},
		{name: "valid", body: `{"error":"bad_request","message":"invalid"}`, ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err, ok := parseAPIError([]byte(tt.body), 400)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v err=%v", ok, tt.ok, err)
			}
		})
	}
}

func TestGetStatus_DecodeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GetStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode status response") {
		t.Fatalf("expected decode status error, got %v", err)
	}
}

func TestGraphQuery_Non200StructuredError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_query", "message": "bad q"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GraphQuery(context.Background(), "bad")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T (%v)", err, err)
	}
	if apiErr.Code != "invalid_query" || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
}

func TestGraphQuery_Non200RawBodyError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("plain error"))
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GraphQuery(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "graph query failed (status 500)") {
		t.Fatalf("expected wrapped non-200 error, got %v", err)
	}
}

func TestGraphRelated_NotFoundFallbackAndDecode(t *testing.T) {
	t.Run("404 fallback when response is non-json", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("missing"))
		}))
		defer ts.Close()

		c := New(ts.URL)
		_, err := c.GraphRelated(context.Background(), "node-x", 1)
		if err == nil || !strings.Contains(err.Error(), `node "node-x" not found`) {
			t.Fatalf("expected node not found fallback, got %v", err)
		}
	})

	t.Run("decode error on success status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{"))
		}))
		defer ts.Close()

		c := New(ts.URL)
		_, err := c.GraphRelated(context.Background(), "n", 1)
		if err == nil || !strings.Contains(err.Error(), "decode graph related response") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})
}

func TestCreateDeleteSource_RequestShapeAndHeaders(t *testing.T) {
	var methods []string
	var contentTypes []string
	var authHeaders []string
	var capturedRequestURI string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		capturedRequestURI = r.RequestURI

		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(store.Component{ID: "s1", Name: "src"})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer ts.Close()

	c := New(ts.URL, WithAPIKey("token-1"))
	_, err := c.CreateComponent(context.Background(), &store.Component{ID: "s1", Name: "src"})
	if err != nil {
		t.Fatalf("CreateComponent() error: %v", err)
	}

	err = c.DeleteComponent(context.Background(), "source with spaces")
	if err != nil {
		t.Fatalf("DeleteComponent() error: %v", err)
	}

	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodDelete {
		t.Fatalf("unexpected methods: %v", methods)
	}
	if contentTypes[0] != "application/json" {
		t.Fatalf("expected JSON content type on POST, got %q", contentTypes[0])
	}
	if contentTypes[1] != "" {
		t.Fatalf("expected empty content type on DELETE, got %q", contentTypes[1])
	}
	if authHeaders[0] != "Bearer token-1" || authHeaders[1] != "Bearer token-1" {
		t.Fatalf("unexpected auth headers: %v", authHeaders)
	}
	if capturedRequestURI != "/api/v1/components/source%20with%20spaces" {
		t.Fatalf("unexpected escaped delete request URI: %q", capturedRequestURI)
	}
}

func TestContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"v","time":"t"}`))
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := c.GetStatus(ctx)
	if err == nil || !strings.Contains(err.Error(), "status request failed") {
		t.Fatalf("expected request failed due to context cancellation, got %v", err)
	}
}

func TestPingListSourcesGraphSummary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case apiStatusPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "v", "time": "t"})
		case apiComponentsPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"components": []map[string]any{}, "count": 0})
		case apiGraphSummaryPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"NodeCount": 1, "EdgeCount": 2, "NodesByType": map[string]int{"svc": 1}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}

	components, err := c.ListComponents(context.Background())
	if err != nil {
		t.Fatalf("ListComponents() error: %v", err)
	}
	if len(components) != 0 {
		t.Fatalf("expected no components, got %d", len(components))
	}

	summary, err := c.GraphSummary(context.Background())
	if err != nil {
		t.Fatalf("GraphSummary() error: %v", err)
	}
	if summary.NodeCount != 1 || summary.EdgeCount != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q to contain %q", got, want)
	}
}
