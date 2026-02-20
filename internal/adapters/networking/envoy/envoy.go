package envoy

import (
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

// ErrNotConnected is returned when the adapter is used before Connect.
var ErrNotConnected = errors.New("envoy adapter not connected")

// ClusterStatus summarises one Envoy cluster.
type ClusterStatus struct {
	Name         string       `json:"name"`
	HostStatuses []HostStatus `json:"host_statuses"`
}

// HostStatus describes a single upstream host.
type HostStatus struct {
	Address      string `json:"address"`
	HealthStatus string `json:"health_status"`
	Weight       int    `json:"weight"`
}

// Stat is a single Envoy stat entry.
type Stat struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// EnvoyAdapter is the interface for querying the Envoy admin API.
type EnvoyAdapter interface {
	adapters.Adapter
	// Clusters returns cluster status summaries.
	Clusters(ctx context.Context) ([]ClusterStatus, error)
	// ConfigDump returns raw config dump sections. Pass an empty string for all.
	ConfigDump(ctx context.Context, section string) (map[string]any, error)
	// Stats returns stats optionally filtered by a prefix string.
	Stats(ctx context.Context, filter string) ([]Stat, error)
}

// httpDoer is an interface over http.Client for testability.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter implements EnvoyAdapter.
type Adapter struct {
	mu        sync.RWMutex
	cfg       Config
	client    httpDoer
	connected bool
}

// New returns an unconnected Adapter.
func New() *Adapter {
	return &Adapter{}
}

// NewWithClient returns an Adapter pre-wired with a client (for tests).
func NewWithClient(client httpDoer, cfg Config) *Adapter {
	return &Adapter{client: client, cfg: cfg, connected: true}
}

// Connect validates the Envoy admin API URL and checks connectivity.
func (a *Adapter) Connect(ctx context.Context, source store.Source) error {
	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return err
	}

	cli := &http.Client{Timeout: time.Duration(cfg.TimeoutMS) * time.Millisecond}

	// Probe /server_info to verify connectivity.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL+"/server_info", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("connect to envoy admin: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("envoy admin returned status %d", resp.StatusCode)
	}

	a.mu.Lock()
	a.cfg = cfg
	a.client = cli
	a.connected = true
	a.mu.Unlock()
	return nil
}

// Disconnect marks the adapter as disconnected.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	a.connected = false
	a.mu.Unlock()
	return nil
}

// Status returns the current connection state.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.connected {
		return adapters.Status{Connected: true, Message: "connected"}
	}
	return adapters.Status{Connected: false, Message: "not connected"}
}

// Clusters returns cluster health summaries from /clusters?format=json.
func (a *Adapter) Clusters(ctx context.Context) ([]ClusterStatus, error) {
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	body, err := a.get(ctx, "/clusters?format=json")
	if err != nil {
		return nil, fmt.Errorf("envoy clusters: %w", err)
	}

	// Parse the Envoy cluster_statuses JSON structure.
	var raw struct {
		ClusterStatuses []struct {
			Name         string `json:"name"`
			HostStatuses []struct {
				Address struct {
					SocketAddress struct {
						Address   string `json:"address"`
						PortValue int    `json:"port_value"`
					} `json:"socket_address"`
				} `json:"address"`
				HealthStatus struct {
					EDSHealthStatus string `json:"eds_health_status"`
				} `json:"health_status"`
				Weight int `json:"weight"`
			} `json:"host_statuses"`
		} `json:"cluster_statuses"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse envoy clusters response: %w", err)
	}

	var out []ClusterStatus
	for _, cs := range raw.ClusterStatuses {
		cluster := ClusterStatus{Name: cs.Name}
		for _, hs := range cs.HostStatuses {
			sa := hs.Address.SocketAddress
			addr := fmt.Sprintf("%s:%d", sa.Address, sa.PortValue)
			status := hs.HealthStatus.EDSHealthStatus
			if status == "" {
				status = "UNKNOWN"
			}
			cluster.HostStatuses = append(cluster.HostStatuses, HostStatus{
				Address:      addr,
				HealthStatus: status,
				Weight:       hs.Weight,
			})
		}
		out = append(out, cluster)
	}
	return out, nil
}

// ConfigDump returns the Envoy config dump, optionally filtered by resource type.
// Supported sections: "routes", "listeners", "clusters", "endpoints", "secrets".
// Pass empty string for the full dump.
func (a *Adapter) ConfigDump(ctx context.Context, section string) (map[string]any, error) {
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	path := "/config_dump"
	if section != "" {
		path += "?resource=" + section
	}
	body, err := a.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("envoy config_dump: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse envoy config_dump response: %w", err)
	}
	return result, nil
}

// Stats returns Envoy stats from /stats?format=json, optionally filtered.
func (a *Adapter) Stats(ctx context.Context, filter string) ([]Stat, error) {
	if err := a.checkConnected(); err != nil {
		return nil, err
	}
	path := "/stats?format=json"
	if filter != "" {
		path += "&filter=" + filter
	}
	body, err := a.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("envoy stats: %w", err)
	}

	var raw struct {
		Stats []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse envoy stats response: %w", err)
	}

	out := make([]Stat, 0, len(raw.Stats))
	for _, s := range raw.Stats {
		out = append(out, Stat{Name: s.Name, Value: s.Value})
	}
	return out, nil
}

func (a *Adapter) checkConnected() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

func (a *Adapter) get(ctx context.Context, path string) ([]byte, error) {
	a.mu.RLock()
	baseURL := a.cfg.URL
	a.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("envoy admin returned status %d for %s", resp.StatusCode, path)
	}
	return io.ReadAll(resp.Body)
}
