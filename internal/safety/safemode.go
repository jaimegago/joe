package safety

import (
	"errors"
	"sync/atomic"
)

// ErrSafeModeActive is the sentinel returned by the tool executor when a T2/T3
// tool is attempted while safe mode is active. It is a typed signal the api
// layer's write-failure classifier matches on (errors.Is) to emit a stable
// error_code, the same way captaingate.GateRefusalError and
// access.ErrPermissionDenied are recognized. The message preserves the
// human-readable unlock hint for anyone reading the raw tool error.
var ErrSafeModeActive = errors.New(
	"safe mode active: only read-only (T1) tools are allowed — run 'joe unlock --reason \"...\"' to resume",
)

var safeModeActive atomic.Bool

// ActivateSafeMode restricts all tool execution to T1 (Observe) only.
// Called by joecored on startup when a panic.state file is found.
func ActivateSafeMode() {
	safeModeActive.Store(true)
}

// DeactivateSafeMode lifts the T1-only restriction after a successful unlock.
func DeactivateSafeMode() {
	safeModeActive.Store(false)
}

// IsSafeModeActive reports whether safe mode is currently active.
// When true, the tool executor rejects T2 and T3 operations.
func IsSafeModeActive() bool {
	return safeModeActive.Load()
}
