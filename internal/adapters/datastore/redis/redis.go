package redis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
	goredis "github.com/redis/go-redis/v9"
)

const (
	statusNotConnected = "Not connected to Redis"
	statusConnectedFmt = "Connected to Redis at %s:%d"
)

// ErrNotConnected means the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to Redis")

// SlowLogEntry is one entry from the Redis slow log.
type SlowLogEntry struct {
	ID              int64    `json:"id"`
	ExecutionTimeUS int64    `json:"execution_time_us"`
	Keys            int64    `json:"keys"`
	Command         []string `json:"command"`
}

// rediser is the minimal Redis interface for testability.
type rediser interface {
	ping(ctx context.Context) error
	info(ctx context.Context, section string) (string, error)
	slowlogGet(ctx context.Context, count int64) ([]SlowLogEntry, error)
	dbsize(ctx context.Context) (int64, error)
	close() error
}

// goRediser wraps go-redis/v9 client.
type goRediser struct{ c *goredis.Client }

func (r *goRediser) ping(ctx context.Context) error {
	return r.c.Ping(ctx).Err()
}

func (r *goRediser) info(ctx context.Context, section string) (string, error) {
	if section == "" {
		return r.c.Info(ctx).Result()
	}
	return r.c.Info(ctx, section).Result()
}

func (r *goRediser) slowlogGet(ctx context.Context, count int64) ([]SlowLogEntry, error) {
	logs, err := r.c.SlowLogGet(ctx, count).Result()
	if err != nil {
		return nil, err
	}
	entries := make([]SlowLogEntry, len(logs))
	for i, l := range logs {
		entries[i] = SlowLogEntry{
			ID:              l.ID,
			ExecutionTimeUS: l.Duration.Microseconds(),
			Keys:            int64(len(l.Args)),
			Command:         l.Args,
		}
	}
	return entries, nil
}

func (r *goRediser) dbsize(ctx context.Context) (int64, error) {
	return r.c.DBSize(ctx).Result()
}

func (r *goRediser) close() error {
	return r.c.Close()
}

// RedisAdapter is the interface for Redis operations.
type RedisAdapter interface {
	adapters.Adapter
	Info(ctx context.Context, section string) (map[string]string, error)
	SlowLog(ctx context.Context, count int64) ([]SlowLogEntry, error)
	DBSize(ctx context.Context) (int64, error)
}

// Adapter is the concrete Redis adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    rediser
	connected bool
}

// New creates a new Redis adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithClient creates an adapter with a custom client (for testing).
func NewWithClient(c rediser) *Adapter {
	return &Adapter{client: c, connected: true}
}

// Connect establishes and verifies connectivity to Redis.
func (a *Adapter) Connect(ctx context.Context, source store.Source) error {
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

	opts := &goredis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	if cfg.TLSEnabled {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	rc := goredis.NewClient(opts)
	r := &goRediser{c: rc}

	if err := r.ping(ctx); err != nil {
		_ = rc.Close()
		return fmt.Errorf("ping Redis at %s:%d: %w", cfg.Host, cfg.Port, err)
	}

	a.client = r
	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.client != nil {
		_ = a.client.close()
		a.client = nil
	}
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
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.Host, a.config.Port),
		}
	}
	return adapters.Status{
		Connected: false,
		Message:   statusNotConnected,
	}
}

// Info returns parsed Redis INFO output for the given section.
func (a *Adapter) Info(ctx context.Context, section string) (map[string]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	raw, err := a.client.info(ctx, section)
	if err != nil {
		return nil, fmt.Errorf("INFO %s: %w", section, err)
	}

	return parseInfo(raw), nil
}

// SlowLog returns the last count entries from the Redis slow log.
func (a *Adapter) SlowLog(ctx context.Context, count int64) ([]SlowLogEntry, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	entries, err := a.client.slowlogGet(ctx, count)
	if err != nil {
		return nil, fmt.Errorf("SLOWLOG GET: %w", err)
	}
	return entries, nil
}

// DBSize returns the number of keys in the current database.
func (a *Adapter) DBSize(ctx context.Context) (int64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return 0, err
	}

	n, err := a.client.dbsize(ctx)
	if err != nil {
		return 0, fmt.Errorf("DBSIZE: %w", err)
	}
	return n, nil
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

// parseInfo parses the Redis INFO text format ("key:value\r\n") into a map.
func parseInfo(raw string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		result[line[:idx]] = line[idx+1:]
	}
	return result
}
