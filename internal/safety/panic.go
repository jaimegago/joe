package safety

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// PanicSource identifies what triggered the emergency shutdown.
type PanicSource string

const (
	PanicSourceREPL   PanicSource = "repl"
	PanicSourceCLI    PanicSource = "cli"
	PanicSourceAPI    PanicSource = "api"
	PanicSourceSignal PanicSource = "signal"
)

// ClusterPanicStore persists panic state to a shared store (e.g. SQLite) that
// is visible to all joecored instances pointing at the same database.
// Implement this interface in the store package and register it via
// SetClusterStore so that Trigger propagates cluster-wide and boot reads the
// shared panicked state. There is no live cluster-wide clear: recovery is a
// local panic-state clear plus restart (D-0018).
type ClusterPanicStore interface {
	SetPanicked(ctx context.Context) error
	ClearPanicked(ctx context.Context) error
	IsPanicked(ctx context.Context) (bool, error)
}

var (
	panicked     atomic.Bool
	clusterStore ClusterPanicStore
)

// SetClusterStore registers the DB-backed store. Call once at startup before
// the first possible Trigger call.
func SetClusterStore(s ClusterPanicStore) {
	clusterStore = s
}

// Trigger sets the global panic flag and logs the event.
// It also persists the state to the cluster store (if registered) so that
// sibling joecored instances boot in safe mode on their next startup.
// It is idempotent — calling it when already panicked is a no-op and returns false.
// Returns true if this call triggered the panic (first caller), false if already panicked.
func Trigger(source PanicSource, reason string) bool {
	if !panicked.CompareAndSwap(false, true) {
		return false // already panicked, no-op
	}
	slog.Error("EMERGENCY SHUTDOWN TRIGGERED",
		"source", string(source),
		"reason", reason,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
	)
	if clusterStore != nil {
		if err := clusterStore.SetPanicked(context.Background()); err != nil {
			slog.Error("failed to persist cluster panic state", "error", err)
		}
	}
	return true
}

// IsPanicked reports whether an emergency shutdown has been triggered.
func IsPanicked() bool {
	return panicked.Load()
}
