package env

import (
	"strings"
	"testing"
)

// TestResolveBootMode is the boot-mode decision break-test (D-0073). It pins the
// four JOE_MODE cases: unset and explicit observation resolve to the observation
// posture (observation=true, the day-one read-only default); full is refused as
// not-yet-implemented; any other value is refused fail-closed. The process-exit
// shim at the boot site (log + return 1) is the only part left untested.
func TestResolveBootMode(t *testing.T) {
	t.Run("unset resolves observation", func(t *testing.T) {
		obs, err := ResolveBootMode("")
		if err != nil {
			t.Fatalf("unset JOE_MODE: unexpected error %v", err)
		}
		if !obs {
			t.Errorf("unset JOE_MODE: observation = false; want true (day-one default)")
		}
	})

	t.Run("explicit observation resolves observation", func(t *testing.T) {
		obs, err := ResolveBootMode(ModeObservation)
		if err != nil {
			t.Fatalf("JOE_MODE=observation: unexpected error %v", err)
		}
		if !obs {
			t.Errorf("JOE_MODE=observation: observation = false; want true")
		}
	})

	t.Run("full returns not-implemented error", func(t *testing.T) {
		obs, err := ResolveBootMode(ModeFull)
		if err == nil {
			t.Fatalf("JOE_MODE=full: expected an error, got nil")
		}
		if obs {
			t.Errorf("JOE_MODE=full: observation = true; want false (refused, not enabled)")
		}
		msg := err.Error()
		if !strings.Contains(msg, "not yet implemented") {
			t.Errorf("JOE_MODE=full error %q must state full mode is not yet implemented", msg)
		}
		if !strings.Contains(msg, "observation mode only") {
			t.Errorf("JOE_MODE=full error %q must state Joe runs in observation mode only", msg)
		}
	})

	t.Run("unrecognized value returns fail-closed error", func(t *testing.T) {
		obs, err := ResolveBootMode("banana")
		if err == nil {
			t.Fatalf("JOE_MODE=banana: expected an error, got nil")
		}
		if obs {
			t.Errorf("JOE_MODE=banana: observation = true; want false (fail-closed)")
		}
		msg := err.Error()
		if !strings.Contains(msg, "banana") {
			t.Errorf("unrecognized-value error %q must name the offending value", msg)
		}
		if !strings.Contains(msg, ModeObservation) || !strings.Contains(msg, ModeFull) {
			t.Errorf("unrecognized-value error %q must name the accepted set (%q, %q)", msg, ModeObservation, ModeFull)
		}
	})
}
