package repl

import (
	"testing"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/tools"
)

// newTestREPL builds a thin REPL bound to the given joe-core client, with a
// real local tool registry/executor.
func newTestREPL(t *testing.T, c *client.Client, cfg *config.Config) *REPL {
	t.Helper()
	registry := tools.NewLocalRegistry(safety.DefaultPolicy())
	executor := tools.NewExecutor(registry, nil)
	return New(c, cfg, executor, registry)
}

func TestNew(t *testing.T) {
	c := client.New("http://localhost:9999")
	r := newTestREPL(t, c, testREPLConfig())

	if r == nil {
		t.Fatal("New() returned nil")
	}
	if r.client == nil {
		t.Error("New() did not set client")
	}
	if r.config == nil {
		t.Error("New() did not set config")
	}
	if r.executor == nil {
		t.Error("New() did not set executor")
	}
	if r.sessionID == "" {
		t.Error("New() did not assign a session ID")
	}
	// The local registry advertises read_file / write_file / run_command /
	// git / ask_user, so client tools must be non-empty.
	if len(r.clientTools) == 0 {
		t.Error("New() advertised no client tools")
	}
}
