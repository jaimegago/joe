package safety

import (
	"errors"
	"fmt"
	"testing"
)

// TestResolveWriteFloor_Precedence is the boot-resolution break-test (D-0018
// point 5/8). It pins all four input combinations, including both-set where
// panic must win over the observation env var.
func TestResolveWriteFloor_Precedence(t *testing.T) {
	tests := []struct {
		name       string
		panicState bool
		observEnv  bool
		wantUp     bool
		wantReason FloorReason
	}{
		{"neither → floor down", false, false, false, FloorReasonNone},
		{"observation only → up/observation", false, true, true, FloorReasonObservation},
		{"panic only → up/safe_mode", true, false, true, FloorReasonSafeMode},
		{"both → up/safe_mode (panic wins)", true, true, true, FloorReasonSafeMode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := ResolveWriteFloor(tc.panicState, tc.observEnv)
			if f.Up() != tc.wantUp {
				t.Errorf("Up() = %v; want %v", f.Up(), tc.wantUp)
			}
			if f.Reason() != tc.wantReason {
				t.Errorf("Reason() = %q; want %q", f.Reason(), tc.wantReason)
			}
		})
	}
}

// TestWriteFloorError_ErrorsIs is the errors.Is-compatibility break-test: the
// reason-carrying error must satisfy errors.Is against the floor identity for
// every reason (preserving the former ErrSafeModeActive dependents) and carry
// the correct reason, including when wrapped.
func TestWriteFloorError_ErrorsIs(t *testing.T) {
	for _, reason := range []FloorReason{FloorReasonSafeMode, FloorReasonObservation} {
		err := &WriteFloorError{Reason: reason}
		if !errors.Is(err, ErrWriteFloor) {
			t.Errorf("errors.Is(%v, ErrWriteFloor) = false; want true", err)
		}
		wrapped := fmt.Errorf("tool failed: %w", err)
		if !errors.Is(wrapped, ErrWriteFloor) {
			t.Errorf("errors.Is(wrapped, ErrWriteFloor) = false; want true")
		}
		var got *WriteFloorError
		if !errors.As(wrapped, &got) {
			t.Fatalf("errors.As(wrapped, *WriteFloorError) = false")
		}
		if got.Reason != reason {
			t.Errorf("recovered reason = %q; want %q", got.Reason, reason)
		}
	}
}

// TestWriteFloor_NoSetter documents the immutability contract structurally: the
// WriteFloor value type exposes only read methods. ResolveWriteFloor is the sole
// constructor and there is no exported method or package function that lowers a
// resolved floor — recovery is restart. (The accompanying repo-walk guard in
// floor_guard_test.go enforces that the former mutable mechanism stays deleted.)
func TestWriteFloor_NoSetter(t *testing.T) {
	up := ResolveWriteFloor(true, false)
	if !up.Up() || up.Reason() != FloorReasonSafeMode {
		t.Fatalf("unexpected resolved floor: up=%v reason=%q", up.Up(), up.Reason())
	}
	// Copying the value cannot mutate the source — values are immutable by
	// construction; there is simply no API to flip it down.
	cp := up
	_ = cp
	if !up.Up() {
		t.Error("floor value must remain up; nothing can lower it")
	}
}
