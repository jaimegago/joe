package falco_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/store"
)

// eventsResponse is the JSON body returned by /api/v1/events.
const eventsJSON = `{
	"events": [
		{
			"uuid": "aaa-111",
			"output": "Warning shell spawned (user=root container=myapp)",
			"priority": "Warning",
			"rule": "Terminal shell in container",
			"time": "2024-01-01T12:00:00Z",
			"source": "syscall",
			"tags": ["container", "shell"],
			"output_fields": {"container.id": "c1", "proc.name": "bash"}
		},
		{
			"uuid": "bbb-222",
			"output": "Critical file write in /etc (user=root)",
			"priority": "Critical",
			"rule": "Write below etc",
			"time": "2024-01-01T12:01:00Z",
			"source": "syscall",
			"tags": ["filesystem"],
			"output_fields": {"fd.name": "/etc/passwd"}
		}
	],
	"total": 2
}`

// healthHandler responds 200 to /api/v1/events and 404 otherwise.
func newTestServer(t *testing.T, eventsHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/events" {
			eventsHandler(w, r)
			return
		}
		http.NotFound(w, r)
	}))
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		wantURL string
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  map[string]any{"url": "http://falcosidekick:2802"},
			wantURL: "http://falcosidekick:2802",
		},
		{
			name:    "with api_key",
			config:  map[string]any{"url": "http://falcosidekick:2802", "api_key": "tok"},
			wantURL: "http://falcosidekick:2802",
		},
		{
			name:    "missing url",
			config:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := falco.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", cfg.URL, tt.wantURL)
			}
		})
	}
}

func TestAdapter_Connect(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"total":0}`))
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !adapter.Status().Connected {
		t.Error("Status().Connected = false, want true")
	}
}

func TestAdapter_Connect_Failure(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestAdapter_Connect_ClosedServer(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv.Close() // close before connecting

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for closed server, got nil")
	}
}

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := falco.New()
	source := store.Source{Config: []byte(`not valid json`)}

	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Connect_WithAPIKey(t *testing.T) {
	var gotAuth string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"total":0}`))
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{
		"url":     srv.URL,
		"api_key": "my-token",
	})}

	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if gotAuth != "Bearer my-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-token")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := falco.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"total":0}`))
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if adapter.Status().Connected {
		t.Error("Status().Connected = true after Disconnect, want false")
	}
	_, err := adapter.ListEvents(context.Background(), "", "", "", 10)
	if err == nil {
		t.Error("expected error after disconnect")
	}
}

func TestAdapter_ListEvents(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(eventsJSON))
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	events, err := adapter.ListEvents(context.Background(), "", "", "", 50)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	if len(events) != 2 {
		t.Errorf("len(events) = %d, want 2", len(events))
	}
	if events[0].UUID != "aaa-111" {
		t.Errorf("UUID = %q, want %q", events[0].UUID, "aaa-111")
	}
	if events[0].Priority != "Warning" {
		t.Errorf("Priority = %q, want %q", events[0].Priority, "Warning")
	}
	if events[0].Rule != "Terminal shell in container" {
		t.Errorf("Rule = %q, want %q", events[0].Rule, "Terminal shell in container")
	}
	if events[0].Source != "syscall" {
		t.Errorf("Source = %q, want %q", events[0].Source, "syscall")
	}
}

func TestAdapter_ListEvents_WithFilters(t *testing.T) {
	var gotPriority, gotSource, gotRule string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPriority = r.URL.Query().Get("priority")
		gotSource = r.URL.Query().Get("source")
		gotRule = r.URL.Query().Get("rule")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"events":[],"total":0}`))
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	_, err := adapter.ListEvents(context.Background(), "Critical", "syscall", "Write below etc", 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	if gotPriority != "Critical" {
		t.Errorf("priority query param = %q, want %q", gotPriority, "Critical")
	}
	if gotSource != "syscall" {
		t.Errorf("source query param = %q, want %q", gotSource, "syscall")
	}
	if gotRule != "Write below etc" {
		t.Errorf("rule query param = %q, want %q", gotRule, "Write below etc")
	}
}

func TestAdapter_ListEvents_ServerError(t *testing.T) {
	callCount := 0
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			// health check succeeds
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"events":[],"total":0}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	_ = adapter.Connect(context.Background(), source)

	_, err := adapter.ListEvents(context.Background(), "", "", "", 50)
	if err == nil {
		t.Error("expected error for server error, got nil")
	}
}

func TestAdapter_ListEvents_NotConnected(t *testing.T) {
	adapter := falco.New()
	_, err := adapter.ListEvents(context.Background(), "", "", "", 50)
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_ListRules(t *testing.T) {
	// Health check call + rules call (both go to /api/v1/events).
	callCount := 0
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"events":[],"total":0}`))
		} else {
			_, _ = w.Write([]byte(eventsJSON))
		}
	})
	defer srv.Close()

	adapter := falco.New()
	source := store.Source{Config: mustMarshal(t, map[string]any{"url": srv.URL})}
	if err := adapter.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	rules, err := adapter.ListRules(context.Background())
	if err != nil {
		t.Fatalf("ListRules() error = %v", err)
	}

	if len(rules) != 2 {
		t.Errorf("len(rules) = %d, want 2", len(rules))
	}

	// Verify both rules appear.
	ruleNames := map[string]bool{}
	for _, r := range rules {
		ruleNames[r.Name] = true
		if r.Count != 1 {
			t.Errorf("rule %q count = %d, want 1", r.Name, r.Count)
		}
	}
	if !ruleNames["Terminal shell in container"] {
		t.Error("expected rule 'Terminal shell in container' not found")
	}
	if !ruleNames["Write below etc"] {
		t.Error("expected rule 'Write below etc' not found")
	}
}

func TestAdapter_ListRules_NotConnected(t *testing.T) {
	adapter := falco.New()
	_, err := adapter.ListRules(context.Background())
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestNewWithClient(t *testing.T) {
	adapter := falco.NewWithClient(&mockDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

type mockDoer struct {
	resp *http.Response
	err  error
}

func (m *mockDoer) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}
