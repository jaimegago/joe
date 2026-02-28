package safety

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndReadPanicState(t *testing.T) {
	dir := t.TempDir()

	state := PanicState{
		TriggeredAt:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		TriggerSource: PanicSourceAPI,
		TriggerReason: "runaway scaling detected",
	}

	if err := WritePanicState(dir, state); err != nil {
		t.Fatalf("WritePanicState: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(filepath.Join(dir, panicStateFile))
	if err != nil {
		t.Fatalf("stat panic.state: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected mode 0600, got %o", info.Mode().Perm())
	}

	got, err := ReadPanicState(dir)
	if err != nil {
		t.Fatalf("ReadPanicState: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
	if got.TriggerSource != state.TriggerSource {
		t.Errorf("TriggerSource: got %q, want %q", got.TriggerSource, state.TriggerSource)
	}
	if got.TriggerReason != state.TriggerReason {
		t.Errorf("TriggerReason: got %q, want %q", got.TriggerReason, state.TriggerReason)
	}
	if !got.TriggeredAt.Equal(state.TriggeredAt) {
		t.Errorf("TriggeredAt: got %v, want %v", got.TriggeredAt, state.TriggeredAt)
	}
}

func TestReadPanicState_NotExist(t *testing.T) {
	dir := t.TempDir()

	got, err := ReadPanicState(dir)
	if err != nil {
		t.Fatalf("ReadPanicState: unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing panic.state, got %+v", got)
	}
}

func TestClearPanicState(t *testing.T) {
	dir := t.TempDir()

	state := PanicState{
		TriggeredAt:   time.Now().UTC(),
		TriggerSource: PanicSourceCLI,
	}
	if err := WritePanicState(dir, state); err != nil {
		t.Fatalf("WritePanicState: %v", err)
	}

	if err := ClearPanicState(dir); err != nil {
		t.Fatalf("ClearPanicState: %v", err)
	}

	got, err := ReadPanicState(dir)
	if err != nil {
		t.Fatalf("ReadPanicState after clear: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after clear, got %+v", got)
	}
}

func TestClearPanicState_NotExist(t *testing.T) {
	dir := t.TempDir()
	// Should be a no-op, not an error
	if err := ClearPanicState(dir); err != nil {
		t.Errorf("ClearPanicState on missing file: %v", err)
	}
}
