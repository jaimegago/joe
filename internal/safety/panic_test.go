package safety

import (
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

func TestReset(t *testing.T) {
	panicked.Store(false)
	t.Cleanup(func() { panicked.Store(false) })

	Trigger(PanicSourceREPL, "test")
	if !IsPanicked() {
		t.Fatal("panic should be set")
	}

	Reset()
	if IsPanicked() {
		t.Error("expected IsPanicked() == false after Reset")
	}
}

func TestPanicSources(t *testing.T) {
	sources := []PanicSource{PanicSourceREPL, PanicSourceCLI, PanicSourceAPI, PanicSourceSignal}
	for _, s := range sources {
		if string(s) == "" {
			t.Errorf("PanicSource %v has empty string value", s)
		}
	}
}
