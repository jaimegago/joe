package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/safety"
)

// TestExecutor_WriteFloor_DeniesMutate_IndependentOfReadPosture is the
// read-posture-latch break-test for the mutate axis: the install-wide read
// posture (team_flat | zoned) governs ONLY the RBAC read decision
// (internal/rbac/policy.go); it has no input to the executor's write floor. A
// team_flat install — where every authenticated principal may READ every
// component — still denies EVERY managed-system mutate when the floor is up.
//
// The executor takes no posture argument by construction: the floor is
// boot-resolved and runtime-immutable (D-0018), and it denies on action class
// (ActionMutate) alone, BEFORE any RBAC scope check. So no read posture, present
// or future, can lower the floor or widen the mutate axis. This test pins that
// orthogonality so a later change cannot quietly couple read posture to mutate
// authorization.
func TestExecutor_WriteFloor_DeniesMutate_IndependentOfReadPosture(t *testing.T) {
	reg := NewRegistry()
	// write_file is a Mutate tool — must be blocked when the floor is up,
	// regardless of the (orthogonal) read posture.
	reg.Register(&safemodeTestTool{name: "write_file"})

	floor := safety.ResolveWriteFloor(true /*panic → safe_mode*/, false)
	e := NewExecutor(reg, nil, WithWriteFloor(floor))

	_, err := e.Execute(context.Background(), "write_file", nil)
	if !errors.Is(err, safety.ErrWriteFloor) {
		t.Fatalf("a Mutate must be denied by the floor regardless of read posture; got err=%v", err)
	}
	var floorErr *safety.WriteFloorError
	if !errors.As(err, &floorErr) {
		t.Fatalf("expected *safety.WriteFloorError; got %T", err)
	}
	if floorErr.Reason != safety.FloorReasonSafeMode {
		t.Errorf("reason = %q; want %q", floorErr.Reason, safety.FloorReasonSafeMode)
	}
}
