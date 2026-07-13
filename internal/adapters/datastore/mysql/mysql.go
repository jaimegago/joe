package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql" // registers "mysql" driver
	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to MySQL"
	statusConnectedFmt = "Connected to MySQL at %s:%d/%s"
)

// ErrNotConnected means the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to MySQL")

// ProcessRow is one row from SHOW PROCESSLIST.
type ProcessRow struct {
	ID      int64  `json:"id"`
	User    string `json:"user"`
	Host    string `json:"host"`
	DB      string `json:"db"`
	Command string `json:"command"`
	Time    int64  `json:"time_seconds"`
	State   string `json:"state"`
	Info    string `json:"info"`
}

// ReplicaStatus holds the parsed SHOW REPLICA STATUS output.
type ReplicaStatus struct {
	Running       bool   `json:"running"`
	SecondsBehind int64  `json:"seconds_behind_master"`
	Error         string `json:"last_error"`
	BinlogFile    string `json:"master_log_file"`
	BinlogPos     int64  `json:"read_master_log_pos"`
}

// Stat holds the aggregate statistics result.
type Stat struct {
	Processes []ProcessRow   `json:"processes"`
	Replica   *ReplicaStatus `json:"replica,omitempty"`
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

// MySQLAdapter is the interface for MySQL operations.
type MySQLAdapter interface {
	adapters.Adapter
	Stat(ctx context.Context) (*Stat, error)
	Query(ctx context.Context, rawSQL string) ([]map[string]any, error)
}

// Adapter is the concrete MySQL adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	db        querier
	connected bool
}

// New creates a new MySQL adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithQuerier creates an adapter with a custom querier (for testing).
func NewWithQuerier(q querier) *Adapter {
	return &Adapter{db: q, connected: true}
}

// Connect establishes and verifies connectivity to MySQL.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse component config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse component config: %w", err)
	}
	a.config = cfg

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open MySQL connection: %w", err)
	}

	q := &sqlQuerier{db: db}
	if err := q.ping(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping MySQL at %s:%d: %w", cfg.Host, cfg.Port, err)
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

// Stat runs SHOW PROCESSLIST and SHOW REPLICA STATUS.
func (a *Adapter) Stat(ctx context.Context) (*Stat, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	stat := &Stat{}

	// Processes
	procRows, err := a.db.scan(ctx, "SHOW PROCESSLIST")
	if err != nil {
		return nil, fmt.Errorf("SHOW PROCESSLIST: %w", err)
	}
	for _, row := range procRows {
		pr := ProcessRow{
			ID:      toInt64(row["Id"]),
			User:    toString(row["User"]),
			Host:    toString(row["Host"]),
			DB:      toString(row["db"]),
			Command: toString(row["Command"]),
			Time:    toInt64(row["Time"]),
			State:   toString(row["State"]),
			Info:    toString(row["Info"]),
		}
		stat.Processes = append(stat.Processes, pr)
	}

	// Replica status — try modern syntax first, fall back to old.
	replicaRows, err := a.db.scan(ctx, "SHOW REPLICA STATUS")
	if err != nil {
		// Fall back to pre-8.0.22 syntax.
		replicaRows, err = a.db.scan(ctx, "SHOW SLAVE STATUS")
		if err != nil {
			// Not a replica — that's fine.
			replicaRows = nil
		}
	}
	if len(replicaRows) > 0 {
		row := replicaRows[0]
		stat.Replica = &ReplicaStatus{
			Running:       toString(row["Replica_Running"]) == "Yes" || toString(row["Slave_Running"]) == "Yes",
			SecondsBehind: toInt64(row["Seconds_Behind_Master"]),
			Error:         toString(row["Last_Error"]),
			BinlogFile:    toString(row["Master_Log_File"]),
			BinlogPos:     toInt64(row["Read_Master_Log_Pos"]),
		}
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

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case uint64:
		return int64(x)
	case float64:
		return int64(x)
	case []byte:
		// MySQL sometimes returns numeric fields as []byte
		var n int64
		fmt.Sscanf(string(x), "%d", &n)
		return n
	}
	return 0
}
