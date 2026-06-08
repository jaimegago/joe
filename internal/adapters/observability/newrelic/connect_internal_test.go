package newrelic

// White-box tests for Connect and nerdGraphDo that need access to unexported fields.
// These live in package newrelic (not newrelic_test) so they can construct Adapter directly.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

type internalMockDoer struct {
	resp *http.Response
	err  error
}

func (m *internalMockDoer) Do(_ *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func httpResp(code int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(code)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

// TestConnect_HappyPath tests Connect's full success path:
// JSON unmarshal → ParseConfig → nerdGraphDo succeeds → connected=true.
func TestConnect_HappyPath(t *testing.T) {
	healthBody := `{"data":{"actor":{"user":{"name":"Alice","email":"alice@example.com"}}}}`

	a := &Adapter{
		client: &internalMockDoer{resp: httpResp(http.StatusOK, healthBody)},
	}

	source := store.Component{
		Config: []byte(`{"api_key":"NRAK-abc123","account_id":99999}`),
	}

	if err := a.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	if !a.connected {
		t.Error("connected should be true after successful Connect()")
	}
	if a.config.AccountID != 99999 {
		t.Errorf("config.AccountID = %d, want 99999", a.config.AccountID)
	}
}

// TestConnect_HappyPath_EURegion verifies EU region config is stored correctly.
func TestConnect_HappyPath_EURegion(t *testing.T) {
	healthBody := `{"data":{"actor":{"user":{"name":"Bob","email":"bob@example.com"}}}}`

	a := &Adapter{
		client: &internalMockDoer{resp: httpResp(http.StatusOK, healthBody)},
	}

	source := store.Component{
		Config: []byte(`{"api_key":"NRAK-eu","account_id":11111,"region":"EU"}`),
	}

	if err := a.Connect(context.Background(), source); err != nil {
		t.Fatalf("Connect() unexpected error: %v", err)
	}
	if a.config.Region != "EU" {
		t.Errorf("config.Region = %q, want EU", a.config.Region)
	}
}

// TestConnect_NetworkFailure tests the path where nerdGraphDo's HTTP call fails.
func TestConnect_NetworkFailure(t *testing.T) {
	a := &Adapter{
		client: &internalMockDoer{err: errors.New("dial tcp: connection refused")},
	}

	source := store.Component{
		Config: []byte(`{"api_key":"NRAK-abc","account_id":12345}`),
	}

	if err := a.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error for network failure, got nil")
	}
	if a.connected {
		t.Error("connected should remain false after failed Connect()")
	}
}

// TestConnect_ServerError tests the path where nerdGraphDo returns a non-200 status.
func TestConnect_ServerError(t *testing.T) {
	a := &Adapter{
		client: &internalMockDoer{
			resp: httpResp(http.StatusUnauthorized, `{"message":"Invalid API key"}`),
		},
	}

	source := store.Component{
		Config: []byte(`{"api_key":"NRAK-bad","account_id":12345}`),
	}

	if err := a.Connect(context.Background(), source); err == nil {
		t.Error("Connect() expected error for 401 response, got nil")
	}
	if a.connected {
		t.Error("connected should remain false after failed Connect()")
	}
}
