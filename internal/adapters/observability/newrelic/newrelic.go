package newrelic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to New Relic"
	statusConnectedFmt = "Connected to New Relic (account %d, region %s)"
)

var ErrNotConnected = errors.New("adapter not connected to New Relic")

// NRQLTimeWindow holds the effective time window of an NRQL query result.
type NRQLTimeWindow struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

// NRQLMetadata holds metadata about an NRQL result.
type NRQLMetadata struct {
	EventTypes []string       `json:"event_types"`
	TimeWindow NRQLTimeWindow `json:"time_window"`
}

// NRQLResult holds the result of a New Relic NRQL query.
type NRQLResult struct {
	Results  []map[string]any `json:"results"`
	Metadata NRQLMetadata     `json:"metadata"`
}

// NewRelicAdapter extends the base Adapter with New Relic-specific operations.
type NewRelicAdapter interface {
	adapters.Adapter
	// NRQLQuery executes an NRQL query against the configured account.
	// accountID may be 0 to use the configured default account.
	NRQLQuery(ctx context.Context, accountID int, query string) (*NRQLResult, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete New Relic adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new New Relic adapter (not yet connected).
func New() *Adapter {
	return &Adapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewWithClient creates an adapter with a custom HTTP client (for testing).
func NewWithClient(client httpDoer) *Adapter {
	return &Adapter{
		client:    client,
		connected: true,
	}
}

// Connect establishes and verifies connectivity to New Relic.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse source config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}
	a.config = cfg

	// Verify connectivity by running a trivial NerdGraph query.
	gql := `{ actor { user { name email } } }`
	if _, err := a.nerdGraphDo(ctx, cfg, gql); err != nil {
		return fmt.Errorf("connect to New Relic: %w", err)
	}

	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.connected {
		return adapters.Status{
			Connected: true,
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.AccountID, a.config.Region),
		}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// NRQLQuery executes an NRQL query against New Relic.
// If accountID is 0, the configured default account is used.
func (a *Adapter) NRQLQuery(ctx context.Context, accountID int, query string) (*NRQLResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if accountID == 0 {
		accountID = a.config.AccountID
	}

	gql := fmt.Sprintf(
		`{ actor { account(id: %d) { nrql(query: %q) { results metadata { timeWindow { since until } eventTypes } } } } }`,
		accountID, query,
	)

	body, err := a.nerdGraphDo(ctx, a.config, gql)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Data struct {
			Actor struct {
				Account struct {
					NRQL struct {
						Results  []map[string]any `json:"results"`
						Metadata struct {
							TimeWindow struct {
								Since string `json:"since"`
								Until string `json:"until"`
							} `json:"timeWindow"`
							EventTypes []string `json:"eventTypes"`
						} `json:"metadata"`
					} `json:"nrql"`
				} `json:"account"`
			} `json:"actor"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse NRQL response: %w", err)
	}
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("nrql query error: %s", raw.Errors[0].Message)
	}

	nrql := raw.Data.Actor.Account.NRQL
	result := &NRQLResult{
		Results: nrql.Results,
		Metadata: NRQLMetadata{
			EventTypes: nrql.Metadata.EventTypes,
			TimeWindow: NRQLTimeWindow{
				Since: nrql.Metadata.TimeWindow.Since,
				Until: nrql.Metadata.TimeWindow.Until,
			},
		},
	}
	if result.Results == nil {
		result.Results = []map[string]any{}
	}
	return result, nil
}

// nerdGraphDo executes a raw NerdGraph GraphQL query and returns the response body.
func (a *Adapter) nerdGraphDo(ctx context.Context, cfg Config, gql string) ([]byte, error) {
	payload, err := json.Marshal(map[string]string{"query": gql})
	if err != nil {
		return nil, fmt.Errorf("marshal nerdgraph request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.NerdGraphURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build nerdgraph request: %w", err)
	}
	req.Header.Set("Api-Key", cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nerdgraph request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read nerdgraph response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nerdgraph request failed (status %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
