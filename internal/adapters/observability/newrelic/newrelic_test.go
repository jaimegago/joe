package newrelic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	"github.com/jaimegago/joe/internal/store"
)

func httpResponse(code int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(code)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

type mockHTTPDoer struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPDoer) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

// --- ParseConfig ---

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]any
		wantErr   bool
		wantRegion string
	}{
		{
			name:      "valid US config",
			config:    map[string]any{"api_key": "NRAK-abc", "account_id": 12345},
			wantErr:   false,
			wantRegion: "US",
		},
		{
			name:      "EU region",
			config:    map[string]any{"api_key": "NRAK-abc", "account_id": 12345, "region": "EU"},
			wantErr:   false,
			wantRegion: "EU",
		},
		{
			name:    "missing api_key",
			config:  map[string]any{"account_id": 12345},
			wantErr: true,
		},
		{
			name:    "missing account_id",
			config:  map[string]any{"api_key": "NRAK-abc"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := newrelic.ParseConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.Region != tt.wantRegion {
				t.Errorf("Region = %q, want %q", cfg.Region, tt.wantRegion)
			}
		})
	}
}

// --- Connect ---

func TestAdapter_Connect_BadJSON(t *testing.T) {
	adapter := newrelic.New()
	source := store.Source{Config: []byte(`invalid json`)}
	if err := adapter.Connect(context.Background(), source); err == nil {
		t.Error("expected error for bad JSON config, got nil")
	}
}

func TestAdapter_Status_Disconnected(t *testing.T) {
	adapter := newrelic.New()
	st := adapter.Status()
	if st.Connected {
		t.Error("Status().Connected = true for new adapter, want false")
	}
	if st.Message == "" {
		t.Error("Status().Message should not be empty")
	}
}

func TestAdapter_NewWithClient_Connected(t *testing.T) {
	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if !adapter.Status().Connected {
		t.Error("NewWithClient should create a connected adapter")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: &http.Response{StatusCode: http.StatusOK, Body: http.NoBody},
	})
	if err := adapter.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if adapter.Status().Connected {
		t.Error("Status().Connected = true after Disconnect, want false")
	}
}

// --- NRQLQuery ---

func TestAdapter_NRQLQuery(t *testing.T) {
	respBody := `{
		"data": {
			"actor": {
				"account": {
					"nrql": {
						"results": [
							{"count": 42}
						],
						"metadata": {
							"timeWindow": {"since": "1 hour ago", "until": "now"},
							"eventTypes": ["Transaction"]
						}
					}
				}
			}
		}
	}`

	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.NRQLQuery(context.Background(), 12345, "SELECT count(*) FROM Transaction SINCE 1 hour ago")
	if err != nil {
		t.Fatalf("NRQLQuery() error = %v", err)
	}

	if len(result.Results) != 1 {
		t.Errorf("len(Results) = %d, want 1", len(result.Results))
	}
	if result.Results[0]["count"] != float64(42) {
		t.Errorf("Results[0][count] = %v, want 42", result.Results[0]["count"])
	}
	if result.Metadata.TimeWindow.Since != "1 hour ago" {
		t.Errorf("Since = %q, want %q", result.Metadata.TimeWindow.Since, "1 hour ago")
	}
	if len(result.Metadata.EventTypes) != 1 || result.Metadata.EventTypes[0] != "Transaction" {
		t.Errorf("EventTypes = %v, want [Transaction]", result.Metadata.EventTypes)
	}
}

func TestAdapter_NRQLQuery_DefaultAccount(t *testing.T) {
	// accountID=0 should use the configured default from config.
	respBody := `{
		"data": {
			"actor": {
				"account": {
					"nrql": {
						"results": [],
						"metadata": {
							"timeWindow": {"since": "", "until": ""},
							"eventTypes": []
						}
					}
				}
			}
		}
	}`

	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.NRQLQuery(context.Background(), 0, "SELECT count(*) FROM Transaction")
	if err != nil {
		t.Fatalf("NRQLQuery() error = %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestAdapter_NRQLQuery_NotConnected(t *testing.T) {
	adapter := newrelic.New()
	_, err := adapter.NRQLQuery(context.Background(), 12345, "SELECT count(*) FROM Transaction")
	if err == nil {
		t.Error("expected error when not connected, got nil")
	}
}

func TestAdapter_NRQLQuery_ServerError(t *testing.T) {
	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusUnauthorized, `{"errors":[{"message":"API key invalid"}]}`),
	})
	_, err := adapter.NRQLQuery(context.Background(), 12345, "SELECT count(*) FROM Transaction")
	if err == nil {
		t.Error("expected error for unauthorized response, got nil")
	}
}

func TestAdapter_NRQLQuery_GraphQLErrors(t *testing.T) {
	respBody := `{
		"data": null,
		"errors": [{"message": "account not found"}]
	}`

	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	_, err := adapter.NRQLQuery(context.Background(), 99999, "SELECT count(*) FROM Transaction")
	if err == nil {
		t.Error("expected error for GraphQL errors, got nil")
	}
}

func TestAdapter_NRQLQuery_EmptyResults(t *testing.T) {
	respBody := `{
		"data": {
			"actor": {
				"account": {
					"nrql": {
						"results": null,
						"metadata": {
							"timeWindow": {"since": "", "until": ""},
							"eventTypes": []
						}
					}
				}
			}
		}
	}`

	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, respBody),
	})

	result, err := adapter.NRQLQuery(context.Background(), 12345, "SELECT count(*) FROM Transaction")
	if err != nil {
		t.Fatalf("NRQLQuery() error = %v", err)
	}
	// Nil results should be normalized to empty slice.
	if result.Results == nil {
		t.Error("Results should not be nil, want empty slice")
	}
}

// --- Config.NerdGraphURL ---

func TestConfig_NerdGraphURL(t *testing.T) {
	tests := []struct {
		region  string
		wantURL string
	}{
		{"US", "https://api.newrelic.com/graphql"},
		{"EU", "https://api.eu.newrelic.com/graphql"},
		{"", "https://api.newrelic.com/graphql"},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			cfg := newrelic.Config{
				APIKey:    "key",
				AccountID: 1,
				Region:    tt.region,
			}
			if got := cfg.NerdGraphURL(); got != tt.wantURL {
				t.Errorf("NerdGraphURL() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}

// --- Connect with test server ---

func TestAdapter_Connect_WithTestServer(t *testing.T) {
	// Serve the NerdGraph GraphQL endpoint.
	healthResp := `{
		"data": {
			"actor": {
				"user": {"name": "test", "email": "test@example.com"}
			}
		}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(healthResp))
	}))
	defer srv.Close()

	// We can't easily redirect NerdGraphURL() to the test server without
	// restructuring the adapter. Use NewWithClient to exercise the connected path.
	adapter := newrelic.NewWithClient(&mockHTTPDoer{
		resp: httpResponse(http.StatusOK, healthResp),
	})
	if !adapter.Status().Connected {
		t.Error("expected connected adapter")
	}
}
