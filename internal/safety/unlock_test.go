package safety

import (
	"testing"
	"time"
)

func TestUnlock_Success(t *testing.T) {
	dir := t.TempDir()

	// Set up: write panic.state, activate safe mode, set panicked flag
	state := PanicState{
		TriggeredAt:   time.Now().UTC(),
		TriggerSource: PanicSourceAPI,
	}
	if err := WritePanicState(dir, state); err != nil {
		t.Fatalf("WritePanicState: %v", err)
	}
	safeModeActive.Store(true)
	panicked.Store(true)
	t.Cleanup(func() {
		safeModeActive.Store(false)
		panicked.Store(false)
	})

	if err := Unlock(dir, "incident resolved, false alarm"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	// Panic state file should be gone
	got, err := ReadPanicState(dir)
	if err != nil {
		t.Fatalf("ReadPanicState after unlock: %v", err)
	}
	if got != nil {
		t.Error("expected nil panic state after unlock")
	}

	// Safe mode should be inactive
	if IsSafeModeActive() {
		t.Error("expected safe mode inactive after unlock")
	}

	// Panic flag should be cleared
	if IsPanicked() {
		t.Error("expected panic flag cleared after unlock")
	}
}

func TestUnlock_EmptyReason(t *testing.T) {
	dir := t.TempDir()
	err := Unlock(dir, "")
	if err == nil {
		t.Error("expected error for empty reason")
	}
}
