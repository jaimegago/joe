package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to PostgreSQL"
	statusConnectedFmt = "Connected to PostgreSQL at %s:%d/%s"
)

// ErrNotConnected means the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to PostgreSQL")

// ActivityRow is one row from pg_stat_activity.
type ActivityRow struct {
	PID      int    `json:"pid"`
	State    string `json:"state"`
	Wait     string `json:"wait_event"`
	Duration string `json:"duration_ms"` // elapsed since query_start
	Query    string `json:"query"`
}

// TableStat is one row from pg_stat_user_tables.
type TableStat struct {
	Schema  string `json:"schema"`
	Table   string `json:"table"`
	SeqScan int64  `json:"seq_scans"`
	IdxScan int64  `json:"idx_scans"`
	LiveTup int64  `json:"live_tuples"`
	DeadTup int64  `json:"dead_tuples"`
}

// ReplicationStat is one row from pg_replication_slots.
type ReplicationStat struct {
	SlotName string `json:"slot_name"`
	Active   bool   `json:"active"`
	LagBytes int64  `json:"lag_bytes"`
}

// Stat holds the aggregate statistics result.
type Stat struct {
	Activity    []ActivityRow     `json:"activity"`
	Tables      []TableStat       `json:"tables"`
	Replication []ReplicationStat `json:"replication"`
}

// querier abstracts database/sql.DB for testability.
type querier interface {
	ping(ctx context.Context) error
	scan(ctx context.Context, query string) ([]map[string]any, error)
	close()
}

// sqlQuerier wraps *sql.DB.
type sqlQuerier struct{ db *sql.DB }

func (q *sqlQuerier) ping(ctx context.Context) error { return q.db.PingContext(ctx) }
func (q *sqlQuerier) close()                         { q.db.Close() }
func (q *sqlQuerier) scan(ctx context.Context, query string) ([]map[string]any, error) {
	rows, err := q.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// PostgreSQLAdapter is the interface for PostgreSQL operations.
type PostgreSQLAdapter interface {
	adapters.Adapter
	Stat(ctx context.Context) (*Stat, error)
	Query(ctx context.Context, rawSQL string) ([]map[string]any, error)
}

// Adapter is the concrete PostgreSQL adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	db        querier
	connected bool
}

// New creates a new PostgreSQL adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithQuerier creates an adapter with a custom querier (for testing).
func NewWithQuerier(q querier) *Adapter {
	return &Adapter{db: q, connected: true}
}

// Connect establishes and verifies connectivity to PostgreSQL.
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

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open PostgreSQL connection: %w", err)
	}

	q := &sqlQuerier{db: db}
	if err := q.ping(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping PostgreSQL at %s:%d: %w", cfg.Host, cfg.Port, err)
	}

	a.db = q
	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.db != nil {
		a.db.close()
		a.db = nil
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
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.Host, a.config.Port, a.config.Database),
		}
	}
	return adapters.Status{
		Connected: false,
		Message:   statusNotConnected,
	}
}

// Stat queries pg_stat_activity, pg_stat_user_tables, and pg_replication_slots.
func (a *Adapter) Stat(ctx context.Context) (*Stat, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	stat := &Stat{}

	// Activity
	activityRows, err := a.db.scan(ctx, `
		SELECT
			pid,
			COALESCE(state, '') AS state,
			COALESCE(wait_event, '') AS wait_event,
			COALESCE(
				EXTRACT(EPOCH FROM (now() - query_start))::text,
				'0'
			) AS duration_ms,
			COALESCE(query, '') AS query
		FROM pg_stat_activity
		WHERE query != '<IDLE>'
		  AND pid != pg_backend_pid()`)
	if err != nil {
		return nil, fmt.Errorf("query pg_stat_activity: %w", err)
	}
	for _, row := range activityRows {
		ar := ActivityRow{
			State: toString(row["state"]),
			Wait:  toString(row["wait_event"]),
			Query: toString(row["query"]),
		}
		if v, ok := row["pid"]; ok {
			ar.PID = toInt(v)
		}
		ar.Duration = toString(row["duration_ms"])
		stat.Activity = append(stat.Activity, ar)
	}

	// Tables
	tableRows, err := a.db.scan(ctx, `
		SELECT
			schemaname,
			tablename,
			COALESCE(seq_scan, 0) AS seq_scan,
			COALESCE(idx_scan, 0) AS idx_scan,
			COALESCE(n_live_tup, 0) AS n_live_tup,
			COALESCE(n_dead_tup, 0) AS n_dead_tup
		FROM pg_stat_user_tables`)
	if err != nil {
		return nil, fmt.Errorf("query pg_stat_user_tables: %w", err)
	}
	for _, row := range tableRows {
		ts := TableStat{
			Schema:  toString(row["schemaname"]),
			Table:   toString(row["tablename"]),
			SeqScan: toInt64(row["seq_scan"]),
			IdxScan: toInt64(row["idx_scan"]),
			LiveTup: toInt64(row["n_live_tup"]),
			DeadTup: toInt64(row["n_dead_tup"]),
		}
		stat.Tables = append(stat.Tables, ts)
	}

	// Replication
	replRows, err := a.db.scan(ctx, `
		SELECT
			slot_name,
			active,
			COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn), 0) AS lag_bytes
		FROM pg_replication_slots`)
	if err != nil {
		return nil, fmt.Errorf("query pg_replication_slots: %w", err)
	}
	for _, row := range replRows {
		rs := ReplicationStat{
			SlotName: toString(row["slot_name"]),
			Active:   toBool(row["active"]),
			LagBytes: toInt64(row["lag_bytes"]),
		}
		stat.Replication = append(stat.Replication, rs)
	}

	return stat, nil
}

// Query executes a read-only SQL query (SELECT only).
func (a *Adapter) Query(ctx context.Context, rawSQL string) ([]map[string]any, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(strings.ToLower(rawSQL))
	if !strings.HasPrefix(trimmed, "select") {
		return nil, fmt.Errorf("only SELECT statements are allowed, got: %q", rawSQL)
	}

	rows, err := a.db.scan(ctx, rawSQL)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	return rows, nil
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

// helper conversions for raw map[string]any values.
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

func toBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case string:
		return x == "t" || x == "true" || x == "1"
	}
	return false
}
