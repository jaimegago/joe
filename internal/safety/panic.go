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

// PanicInfo carries the who/when/why of a recorded panic, read back from the
// single panic store row for boot logging and the panic status endpoint. It
// replaces the deleted file-serialization PanicState struct (D-0018
// consolidation): panic state has ONE home, the DB row, and is never persisted
// to a panic.state file.
type PanicInfo struct {
	TriggeredAt   time.Time
	TriggerSource PanicSource
	TriggerReason string
}

// ClusterPanicStore is the SINGLE home for panic state (D-0018 consolidation):
// the cluster_panic_state DB row. Panic entry writes the row via SetPanicked and
// boot reads it via IsPanicked; there is no panic.state file. Implement this
// interface in the store package and register it via SetClusterStore so that
// Trigger persists the panic and boot resolves the safe-mode floor from it.
// There is no live in-process clear: recovery is clearing the row (the local
// `joe unlock` CLI, which opens the DB directly) plus a restart (D-0018).
type ClusterPanicStore interface {
	SetPanicked(ctx context.Context, source PanicSource, reason string) error
	ClearPanicked(ctx context.Context) error
	IsPanicked(ctx context.Context) (bool, error)
	PanicInfo(ctx context.Context) (*PanicInfo, error)
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
		if err := clusterStore.SetPanicked(context.Background(), source, reason); err != nil {
			slog.Error("failed to persist panic state", "error", err)
		}
	}
	return true
}

// IsPanicked reports whether an emergency shutdown has been triggered.
func IsPanicked() bool {
	return panicked.Load()
}
