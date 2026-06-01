package llmsettings_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/store"
)

// TestResolveActiveModelOnStartup_StoredResolves: a stored, valid
// active model wins over the configured Current. Logs at info naming
// the stored value.
func TestResolveActiveModelOnStartup_StoredResolves(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, auditRepo)
	if err := svc.SetActiveModel(context.Background(), "gemini-pro"); err != nil {
		t.Fatalf("SetActiveModel: %v", err)
	}

	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	got := llmsettings.ResolveActiveModelOnStartup(context.Background(), repo,
		"claude-sonnet",
		map[string]bool{"claude-sonnet": true, "gemini-pro": true},
		logger,
	)
	if got != "gemini-pro" {
		t.Errorf("resolved = %q, want gemini-pro (stored value resolves and must win)", got)
	}
	if !strings.Contains(buf.String(), "using stored active model") {
		t.Errorf("info log missing 'using stored active model'; got:\n%s", buf.String())
	}
}

// TestResolveActiveModelOnStartup_EmptyStoredFallsBack: the empty
// migration seed silently falls back to configured. No warn.
func TestResolveActiveModelOnStartup_EmptyStoredFallsBack(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)

	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	got := llmsettings.ResolveActiveModelOnStartup(context.Background(), repo,
		"claude-sonnet",
		map[string]bool{"claude-sonnet": true},
		logger,
	)
	if got != "claude-sonnet" {
		t.Errorf("resolved = %q, want claude-sonnet (empty stored must fall back)", got)
	}
	if strings.Contains(strings.ToLower(buf.String()), "warn") {
		t.Errorf("warn emitted for empty stored value; that is the expected fresh-install state. Got:\n%s", buf.String())
	}
}

// TestResolveActiveModelOnStartup_StaleStoredFallsBackWithWarning is
// the load-bearing test: a stored model NOT in the configured
// available set must fall back to configured AND emit a warn naming
// the stale stored value. The system must not fail startup.
func TestResolveActiveModelOnStartup_StaleStoredFallsBackWithWarning(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, auditRepo)
	if err := svc.SetActiveModel(context.Background(), "model-from-a-previous-deployment"); err != nil {
		t.Fatalf("SetActiveModel: %v", err)
	}

	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	got := llmsettings.ResolveActiveModelOnStartup(context.Background(), repo,
		"claude-sonnet",
		map[string]bool{"claude-sonnet": true, "gemini-pro": true}, // stored not in set
		logger,
	)
	if got != "claude-sonnet" {
		t.Errorf("resolved = %q, want claude-sonnet (stale stored must fall back to configured)", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, "not present in configured available models") {
		t.Errorf("warn log missing 'not present in configured available models'; got:\n%s", logged)
	}
	if !strings.Contains(logged, "model-from-a-previous-deployment") {
		t.Errorf("warn log missing the stale stored value name; got:\n%s", logged)
	}
}
