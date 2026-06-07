package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTriggerPanic_Success(t *testing.T) {
	var capturedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("TriggerPanic: expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.TriggerPanic(context.Background(), "runaway automation")
	if err != nil {
		t.Fatalf("TriggerPanic() error: %v", err)
	}
	if capturedBody["reason"] != "runaway automation" {
		t.Errorf("TriggerPanic(): unexpected reason %q", capturedBody["reason"])
	}
}

func TestTriggerPanic_EmptyReason(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.TriggerPanic(context.Background(), "")
	if err != nil {
		t.Fatalf("TriggerPanic() error with empty reason: %v", err)
	}
}

func TestTriggerPanic_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "panic failed"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.TriggerPanic(context.Background(), "test")
	if err == nil {
		t.Fatal("TriggerPanic(): expected error for 500 response")
	}
}

func TestGetPanicStatus_Success(t *testing.T) {
	triggeredAt := time.Now().UTC().Truncate(time.Second)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GetPanicStatus: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"safe_mode":      true,
			"triggered_at":   triggeredAt,
			"trigger_source": "repl",
			"trigger_reason": "test emergency",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	status, err := c.GetPanicStatus(context.Background())
	if err != nil {
		t.Fatalf("GetPanicStatus() error: %v", err)
	}
	if !status.SafeMode {
		t.Error("GetPanicStatus(): expected safe_mode=true")
	}
	if status.TriggerSource != "repl" {
		t.Errorf("GetPanicStatus(): unexpected trigger_source %q", status.TriggerSource)
	}
	if status.TriggerReason != "test emergency" {
		t.Errorf("GetPanicStatus(): unexpected trigger_reason %q", status.TriggerReason)
	}
}

func TestGetPanicStatus_NotInSafeMode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"safe_mode": false,
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	status, err := c.GetPanicStatus(context.Background())
	if err != nil {
		t.Fatalf("GetPanicStatus() error: %v", err)
	}
	if status.SafeMode {
		t.Error("GetPanicStatus(): expected safe_mode=false")
	}
}

func TestGetPanicStatus_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "store unavailable"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.GetPanicStatus(context.Background())
	if err == nil {
		t.Fatal("GetPanicStatus(): expected error for 500 response")
	}
}

// Note: the client has no Unlock method anymore — panic recovery is a
// local-file-only host CLI (D-0018 point 4), not an HTTP-client call. There is
// deliberately no /api/v1/unlock endpoint for the client to reach.

func TestPanicURLPaths(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"safe_mode": false, "status": "ok"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_ = c.TriggerPanic(context.Background(), "test")
	_, _ = c.GetPanicStatus(context.Background())

	assertContains(t, paths[0], "/api/v1/panic")
	assertContains(t, paths[1], "/api/v1/panic/status")
}
