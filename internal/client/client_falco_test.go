package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFalcoEvents_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]any{
				{"rule": "Terminal shell in container", "priority": "WARNING", "output": "shell spawned", "source": "syscall"},
			},
			"count":     1,
			"source_id": "falco-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	events, err := c.FalcoEvents(context.Background(), "falco-prod", "WARNING", "", "", 10)
	if err != nil {
		t.Fatalf("FalcoEvents() error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("FalcoEvents(): got %d events, want 1", len(events))
	}
	if events[0].Rule != "Terminal shell in container" {
		t.Errorf("FalcoEvents(): unexpected rule %q", events[0].Rule)
	}
}

func TestFalcoEvents_NoFilters(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events":    []map[string]any{},
			"count":     0,
			"source_id": "falco-1",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.FalcoEvents(context.Background(), "falco-1", "", "", "", 0)
	if err != nil {
		t.Fatalf("FalcoEvents() error: %v", err)
	}
	assertContains(t, capturedURI, "/api/v1/falco/falco-1/events")
}

func TestFalcoEvents_WithAllFilters(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events":    []map[string]any{},
			"count":     0,
			"source_id": "falco-1",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.FalcoEvents(context.Background(), "falco-1", "CRITICAL", "k8s_audit", "Write below root", 5)
	if err != nil {
		t.Fatalf("FalcoEvents() error: %v", err)
	}
	assertContains(t, capturedURI, "priority=CRITICAL")
	assertContains(t, capturedURI, "source=k8s_audit")
	assertContains(t, capturedURI, "rule=Write+below+root")
}

func TestFalcoEvents_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "db down"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.FalcoEvents(context.Background(), "falco-prod", "", "", "", 0)
	if err == nil {
		t.Fatal("FalcoEvents(): expected error for 500 response")
	}
}

func TestFalcoRules_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rules": []map[string]any{
				{"name": "Write below root", "priority": "ERROR", "source": "syscall", "count": 3},
			},
			"count":     1,
			"source_id": "falco-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	rules, err := c.FalcoRules(context.Background(), "falco-prod")
	if err != nil {
		t.Fatalf("FalcoRules() error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("FalcoRules(): got %d rules, want 1", len(rules))
	}
	if rules[0].Name != "Write below root" {
		t.Errorf("FalcoRules(): unexpected rule name %q", rules[0].Name)
	}
}

func TestFalcoRules_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "falco unreachable"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.FalcoRules(context.Background(), "falco-prod")
	if err == nil {
		t.Fatal("FalcoRules(): expected error for 500 response")
	}
}

func TestFalcoRules_URLConstruction(t *testing.T) {
	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rules":     []map[string]any{},
			"count":     0,
			"source_id": "falco-2",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.FalcoRules(context.Background(), "falco-2")
	if err != nil {
		t.Fatalf("FalcoRules() error: %v", err)
	}
	if capturedPath != "/api/v1/falco/falco-2/rules" {
		t.Errorf("FalcoRules(): unexpected path %q", capturedPath)
	}
}
