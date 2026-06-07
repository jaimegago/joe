package safety

import (
	"context"
	"fmt"
	"testing"
)

func TestTrigger_FirstCaller(t *testing.T) {
	// Reset before test to ensure clean state
	panicked.Store(false)
	t.Cleanup(func() { panicked.Store(false) })

	if IsPanicked() {
		t.Fatal("expected IsPanicked() == false initially")
	}

	triggered := Trigger(PanicSourceAPI, "test reason")
	if !triggered {
		t.Error("expected Trigger to return true on first call")
	}
	if !IsPanicked() {
		t.Error("expected IsPanicked() == true after Trigger")
	}
}

func TestTrigger_Idempotent(t *testing.T) {
	panicked.Store(false)
	t.Cleanup(func() { panicked.Store(false) })

	Trigger(PanicSourceAPI, "first")
	second := Trigger(PanicSourceAPI, "second")
	if second {
		t.Error("expected second Trigger to return false (idempotent)")
	}
	if !IsPanicked() {
		t.Error("panic flag should still be set after second trigger")
	}
}

// Note: there is no production Reset of the panic flag. The former Reset() (the
// in-process down-transition the old unlock path called) is deleted — recovery
// is process restart, which re-reads the persisted state at boot (D-0018). Tests
// that need a clean panic flag store directly into the unexported atomic.

func TestPanicSources(t *testing.T) {
	sources := []PanicSource{PanicSourceREPL, PanicSourceCLI, PanicSourceAPI, PanicSourceSignal}
	for _, s := range sources {
		if string(s) == "" {
			t.Errorf("PanicSource %v has empty string value", s)
		}
	}
}

func TestSetClusterStore(t *testing.T) {
	// SetClusterStore sets the package-level clusterStore variable.
	// Just verify it can be called without panicking.
	orig := clusterStore
	t.Cleanup(func() { clusterStore = orig })

	SetClusterStore(nil)
	if clusterStore != nil {
		t.Error("expected clusterStore to be nil after SetClusterStore(nil)")
	}
}

func TestTrigger_WithClusterStoreError(t *testing.T) {
	panicked.Store(false)
	t.Cleanup(func() {
		panicked.Store(false)
		clusterStore = nil
	})

	// Register a cluster store that returns an error on SetPanicked.
	clusterStore = &errClusterStore{setPanickedErr: errTest}

	// Trigger should still return true (in-process flag is set) even if the
	// cluster store call fails; the error is only logged.
	triggered := Trigger(PanicSourceAPI, "cluster store error test")
	if !triggered {
		t.Error("expected Trigger to return true even when cluster store errors")
	}
	if !IsPanicked() {
		t.Error("expected IsPanicked() == true after Trigger with cluster store error")
	}
}

var errTest = fmt.Errorf("simulated store error")

// errClusterStore is a ClusterPanicStore that returns configured errors.
type errClusterStore struct {
	setPanickedErr   error
	clearPanickedErr error
	isPanickedVal    bool
	isPanickedErr    error
}

func (s *errClusterStore) SetPanicked(_ context.Context) error   { return s.setPanickedErr }
func (s *errClusterStore) ClearPanicked(_ context.Context) error { return s.clearPanickedErr }
func (s *errClusterStore) IsPanicked(_ context.Context) (bool, error) {
	return s.isPanickedVal, s.isPanickedErr
}
