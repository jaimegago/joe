package safety

import (
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

var panicked atomic.Bool

// Trigger sets the global panic flag and logs the event.
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
	return true
}

// IsPanicked reports whether an emergency shutdown has been triggered.
func IsPanicked() bool {
	return panicked.Load()
}

// Reset clears the panic flag. This is used after a successful unlock — the
// process must be in a known-good state before calling Reset.
// In production this happens via process restart; Reset exists for testing.
func Reset() {
	panicked.Store(false)
}
