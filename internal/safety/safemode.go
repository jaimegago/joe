package safety

import "sync/atomic"

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
