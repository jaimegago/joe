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
	// github_comment is a Mutate tool — must be blocked when the floor is up.
	// (graph_add_node is no longer Mutate: it is T1 model maintenance.)
	reg.Register(&safemodeTestTool{name: "github_comment"})

	// The floor is INJECTED as a boot-resolved value — there is no global to
	// activate. A safe_mode floor (panic state present) is up.
	floor := safety.ResolveWriteFloor(true /*panic*/, false)
	e := NewExecutor(reg, nil, WithWriteFloor(floor))
	_, err := e.Execute(context.Background(), "github_comment", nil)
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
	reg.Register(&safemodeTestTool{name: "github_comment"})

	// Observation floor: env var set, no panic.
	floor := safety.ResolveWriteFloor(false, true /*observation*/)
	e := NewExecutor(reg, nil, WithWriteFloor(floor))
	_, err := e.Execute(context.Background(), "github_comment", nil)
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
	// list_components is a Read tool — must succeed even when the floor is up.
	reg.Register(&safemodeTestTool{name: "list_components"})

	floor := safety.ResolveWriteFloor(true, false)
	e := NewExecutor(reg, nil, WithWriteFloor(floor))
	_, err := e.Execute(context.Background(), "list_components", nil)
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

// fakePanicStore is an in-memory safety.ClusterPanicStore for the
// not-re-derived break-test below — the panic state's single home is the DB row,
// modeled here without a real database.
type fakePanicStore struct{ panicked bool }

func (f *fakePanicStore) SetPanicked(context.Context, safety.PanicSource, string) error {
	f.panicked = true
	return nil
}
func (f *fakePanicStore) ClearPanicked(context.Context) error { f.panicked = false; return nil }
func (f *fakePanicStore) IsPanicked(context.Context) (bool, error) {
	return f.panicked, nil
}
func (f *fakePanicStore) PanicInfo(context.Context) (*safety.PanicInfo, error) {
	if !f.panicked {
		return nil, nil
	}
	return &safety.PanicInfo{TriggerSource: safety.PanicSourceAPI}, nil
}

// TestExecutor_Floor_NotReDerivedFromDBRow is the immutability break-test, now
// expressed against the single panic DB row (D-0018 consolidation): a floor
// injected at construction stays up for the executor's lifetime even after the
// panic row is cleared mid-process. The executor holds the boot-sealed value and
// never re-derives the floor, so a concurrent `joe unlock` (which clears the DB
// row) cannot lower a running floor — clearing affects only the NEXT boot.
func TestExecutor_Floor_NotReDerivedFromDBRow(t *testing.T) {
	ctx := context.Background()
	// Simulate boot finding the panic row set: floor resolves up/safe_mode.
	store := &fakePanicStore{panicked: true}
	dbPanicked, err := store.IsPanicked(ctx)
	if err != nil || !dbPanicked {
		t.Fatalf("expected panic row present; err=%v panicked=%v", err, dbPanicked)
	}
	floor := safety.ResolveWriteFloor(dbPanicked, false)

	reg := NewRegistry()
	reg.Register(&safemodeTestTool{name: "github_comment"})
	e := NewExecutor(reg, nil, WithWriteFloor(floor))

	// A local `joe unlock` clears the panic DB row mid-process.
	if err := store.ClearPanicked(ctx); err != nil {
		t.Fatalf("clear panic row: %v", err)
	}

	// The already-constructed executor must STILL deny — the floor is not
	// re-derived from the DB row (or anywhere) mid-process.
	if _, err := e.Execute(ctx, "github_comment", nil); !errors.Is(err, safety.ErrWriteFloor) {
		t.Fatalf("floor must stay up after the panic row is cleared mid-process; got err=%v", err)
	}
}
