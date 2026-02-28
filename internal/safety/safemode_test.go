package safety

import "testing"

func TestSafeMode(t *testing.T) {
	// Ensure clean state
	safeModeActive.Store(false)
	t.Cleanup(func() { safeModeActive.Store(false) })

	if IsSafeModeActive() {
		t.Fatal("expected safe mode inactive initially")
	}

	ActivateSafeMode()
	if !IsSafeModeActive() {
		t.Error("expected safe mode active after ActivateSafeMode")
	}

	DeactivateSafeMode()
	if IsSafeModeActive() {
		t.Error("expected safe mode inactive after DeactivateSafeMode")
	}
}
