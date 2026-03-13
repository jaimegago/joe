package safety

import (
	"os"
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

func TestUnlock_WithClusterStoreError(t *testing.T) {
	dir := t.TempDir()

	state := PanicState{
		TriggeredAt:   time.Now().UTC(),
		TriggerSource: PanicSourceAPI,
	}
	if err := WritePanicState(dir, state); err != nil {
		t.Fatalf("WritePanicState: %v", err)
	}
	safeModeActive.Store(true)
	panicked.Store(true)

	origStore := clusterStore
	clusterStore = &errClusterStore{clearPanickedErr: errTest}
	t.Cleanup(func() {
		safeModeActive.Store(false)
		panicked.Store(false)
		clusterStore = origStore
	})

	// Unlock should still succeed overall — the cluster store error is logged but not fatal.
	if err := Unlock(dir, "cluster store error is non-fatal"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if IsSafeModeActive() {
		t.Error("expected safe mode inactive after unlock")
	}
	if IsPanicked() {
		t.Error("expected panic flag cleared after unlock")
	}
}

func TestUnlock_ClearPanicStateError(t *testing.T) {
	// Pass a non-existent, non-writable directory so ClearPanicState can't even
	// check the path in a meaningful way. We force the error by making the
	// state file exist but the directory read-only so Remove fails.
	if testing.Short() {
		t.Skip("skipping permission-based test in short mode")
	}

	dir := t.TempDir()
	state := PanicState{
		TriggeredAt:   time.Now().UTC(),
		TriggerSource: PanicSourceCLI,
	}
	if err := WritePanicState(dir, state); err != nil {
		t.Fatalf("WritePanicState: %v", err)
	}

	// Make dir read-only so os.Remove(panic.state) fails.
	if err := chmodDir(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = chmodDir(dir, 0700) })

	err := Unlock(dir, "test unlock clear error")
	if err == nil {
		t.Error("expected error when ClearPanicState fails due to read-only dir")
	}
}

// chmodDir changes directory permissions; helper to skip on root.
func chmodDir(dir string, mode uint32) error {
	if isRoot() {
		return nil // root ignores permissions, skip
	}
	return os.Chmod(dir, os.FileMode(mode))
}

func isRoot() bool {
	return os.Getuid() == 0
}
