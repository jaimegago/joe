package adapters_test

import (
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

// stubAdapter implements Adapter for testing.
type stubAdapter struct {
	disconnected bool
}

func (a *stubAdapter) Connect(source store.Source) error { return nil }
func (a *stubAdapter) Disconnect() error                 { a.disconnected = true; return nil }
func (a *stubAdapter) Status() adapters.Status           { return adapters.Status{Connected: true} }

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := adapters.NewRegistry()
	a := &stubAdapter{}

	r.Register("prod-cluster", a)

	got, err := r.Get("prod-cluster")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != a {
		t.Error("Get returned different adapter")
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := adapters.NewRegistry()

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing adapter")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := adapters.NewRegistry()
	a := &stubAdapter{}

	r.Register("prod", a)

	if err := r.Unregister("prod"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	if !a.disconnected {
		t.Error("expected Disconnect to be called")
	}

	_, err := r.Get("prod")
	if err == nil {
		t.Error("expected error after unregister")
	}
}

func TestRegistry_UnregisterNonexistent(t *testing.T) {
	r := adapters.NewRegistry()

	if err := r.Unregister("nonexistent"); err != nil {
		t.Fatalf("Unregister nonexistent should not error: %v", err)
	}
}

func TestRegistry_List(t *testing.T) {
	r := adapters.NewRegistry()
	r.Register("a", &stubAdapter{})
	r.Register("b", &stubAdapter{})

	ids := r.List()
	if len(ids) != 2 {
		t.Fatalf("List returned %d items, want 2", len(ids))
	}

	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["a"] || !found["b"] {
		t.Errorf("List = %v, want [a, b]", ids)
	}
}
