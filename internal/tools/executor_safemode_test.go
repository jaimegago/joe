package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/safety"
)

// safemodeTestTool is a minimal Tool implementation for write-floor tests.
type safemodeTestTool struct{ name string }

func (f *safemodeTestTool) Name() string        { return f.name }
func (f *safemodeTestTool) Description() string { return "write floor test tool" }
func (f *safemodeTestTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{Type: "object"}
}
func (f *safemodeTestTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return "ok", nil
}

func TestExecutor_WriteFloor_BlocksMutate(t *testing.T) {
	reg := NewRegistry()
	// write_file is a Mutate tool — must be blocked when the floor is up.
	// (graph_add_node is no longer Mutate: it is T1 model maintenance.)
	reg.Register(&safemodeTestTool{name: "write_file"})

	// The floor is INJECTED as a boot-resolved value — there is no global to
	// activate. A safe_mode floor (panic state present) is up.
	floor := safety.ResolveWriteFloor(true /*panic*/, false)
	e := NewExecutor(reg, nil, WithWriteFloor(floor))
	_, err := e.Execute(context.Background(), "write_file", nil)
	if err == nil {
		t.Fatal("expected error when the write floor is up for a Mutate tool")
	}
	// The denial must satisfy errors.Is against the floor identity so the api
	// layer's classifyWriteFailure can emit a stable error_code, distinct from
	// an ordinary tool failure.
	if !errors.Is(err, safety.ErrWriteFloor) {
		t.Fatalf("expected errors.Is(err, ErrWriteFloor); got %v", err)
	}
	// The reason rides out of the single error as data.
	var floorErr *safety.WriteFloorError
	if !errors.As(err, &floorErr) {
		t.Fatalf("expected *safety.WriteFloorError; got %T", err)
	}
	if floorErr.Reason != safety.FloorReasonSafeMode {
		t.Errorf("reason = %q; want %q", floorErr.Reason, safety.FloorReasonSafeMode)
	}
}

func TestExecutor_WriteFloor_ObservationReason(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&safemodeTestTool{name: "write_file"})

	// Observation floor: env var set, no panic.
	floor := safety.ResolveWriteFloor(false, true /*observation*/)
	e := NewExecutor(reg, nil, WithWriteFloor(floor))
	_, err := e.Execute(context.Background(), "write_file", nil)
	if err == nil {
		t.Fatal("expected error when the write floor is up under observation")
	}
	var floorErr *safety.WriteFloorError
	if !errors.As(err, &floorErr) {
		t.Fatalf("expected *safety.WriteFloorError; got %T", err)
	}
	if floorErr.Reason != safety.FloorReasonObservation {
		t.Errorf("reason = %q; want %q (a denied Mutate under observation must "+
			"present distinctly from safe mode)", floorErr.Reason, safety.FloorReasonObservation)
	}
}

func TestExecutor_WriteFloor_AllowsReadWhenUp(t *testing.T) {
	reg := NewRegistry()
	// read_file is a Read tool — must succeed even when the floor is up.
	reg.Register(&safemodeTestTool{name: "read_file"})

	floor := safety.ResolveWriteFloor(true, false)
	e := NewExecutor(reg, nil, WithWriteFloor(floor))
	_, err := e.Execute(context.Background(), "read_file", nil)
	if err != nil {
		t.Fatalf("expected Read tool to succeed when the floor is up, got: %v", err)
	}
}

func TestExecutor_FloorDown_AllowsModelMaintenance(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&safemodeTestTool{name: "graph_add_node"})

	policy := safety.DefaultPolicy()
	// No WithWriteFloor → zero-value floor (down).
	e := NewExecutor(reg, nil, WithPolicy(policy))
	// graph_add_node is T1 (Joe's own model maintenance) — always allowed.
	_, err := e.Execute(context.Background(), "graph_add_node", nil)
	if err != nil {
		t.Fatalf("expected model-maintenance tool to succeed with the floor down: %v", err)
	}
}

// TestExecutor_Floor_NotReDerivedFromDisk is the immutability break-test: a
// floor injected at construction stays up for the executor's lifetime even after
// the persisted panic state file is removed on disk. The executor holds the
// boot-sealed value and never re-derives the floor from disk mid-process, so a
// concurrent `joe unlock` (which clears the file) cannot lower a running floor.
func TestExecutor_Floor_NotReDerivedFromDisk(t *testing.T) {
	dir := t.TempDir()
	// Simulate boot finding a panic state: floor resolves up/safe_mode.
	if err := safety.WritePanicState(dir, safety.PanicState{TriggerSource: safety.PanicSourceAPI}); err != nil {
		t.Fatalf("write panic state: %v", err)
	}
	present, err := safety.ReadPanicState(dir)
	if err != nil || present == nil {
		t.Fatalf("expected panic state present; err=%v state=%v", err, present)
	}
	floor := safety.ResolveWriteFloor(present != nil, false)

	reg := NewRegistry()
	reg.Register(&safemodeTestTool{name: "write_file"})
	e := NewExecutor(reg, nil, WithWriteFloor(floor))

	// A local `joe unlock` clears the persisted state file mid-process.
	if err := safety.ClearPanicState(dir); err != nil {
		t.Fatalf("clear panic state: %v", err)
	}

	// The already-constructed executor must STILL deny — the floor is not
	// re-read from disk.
	if _, err := e.Execute(context.Background(), "write_file", nil); !errors.Is(err, safety.ErrWriteFloor) {
		t.Fatalf("floor must stay up after the panic file is cleared mid-process; got err=%v", err)
	}
}
