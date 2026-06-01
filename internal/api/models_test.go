package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/store"
)

// stubAdapter is a no-op LLMAdapter used as swap fodder in model tests.
type stubAdapter struct{}

func (stubAdapter) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}
func (stubAdapter) ChatStream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}
func (stubAdapter) Embed(context.Context, string) ([]float32, error) { return nil, nil }

func setupModelServer(t *testing.T) (*Server, *llm.SwappableAdapter) {
	t.Helper()
	sw := llm.NewSwappableAdapter(&stubAdapter{}, "claude-sonnet")
	services := &core.Services{
		Config: &config.Config{
			LLM: config.LLMConfig{
				Current: "claude-sonnet",
				Available: map[string]config.ModelConfig{
					"claude-sonnet": {Provider: "claude", Model: "claude-sonnet-4-20250514"},
					"gemini-pro":    {Provider: "gemini", Model: "gemini-2.0-pro"},
				},
			},
		},
		LLM: sw,
	}
	return New(services), sw
}

// setupModelServerWithSettings wires a real (in-memory SQLite) llmsettings
// stack into the services container so the change-model endpoint can
// exercise the persist+audit+swap path. Returns the server, the
// swappable adapter, the underlying database for direct assertions on
// llm_settings and audit_log, and a teardown.
func setupModelServerWithSettings(t *testing.T) (*Server, *llm.SwappableAdapter, *sql.DB) {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	settingsRepo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(settingsRepo, auditRepo)

	sw := llm.NewSwappableAdapter(&stubAdapter{}, "claude-sonnet")
	services := &core.Services{
		Config: &config.Config{
			LLM: config.LLMConfig{
				Current: "claude-sonnet",
				Available: map[string]config.ModelConfig{
					"claude-sonnet": {Provider: "claude", Model: "claude-sonnet-4-20250514"},
					"gemini-pro":    {Provider: "gemini", Model: "gemini-2.0-pro"},
				},
			},
		},
		LLM:         sw,
		LLMSettings: svc,
		Audit:       auditRepo,
	}
	return New(services), sw, s.DB()
}

func TestHandleListModels(t *testing.T) {
	server, _ := setupModelServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp modelsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Current != "claude-sonnet" {
		t.Errorf("current = %q, want claude-sonnet", resp.Current)
	}
	// ModelNames is sorted: claude-sonnet, gemini-pro.
	if len(resp.Available) != 2 || resp.Available[0] != "claude-sonnet" || resp.Available[1] != "gemini-pro" {
		t.Errorf("available = %v, want [claude-sonnet gemini-pro]", resp.Available)
	}
}

// TestHandleSetModel is the happy-path smoke test. Stream G phase G4
// correction: the endpoint REQUIRES the settings service, so this
// test wires the same real settings harness the persist+audit test
// uses. The narrow assertions here cover the HTTP-shape contract
// (status code, response body, post-swap Current); the durable side
// effects (settings row + audit row with target/before/after) are
// covered by TestHandleSetModel_PersistsAuditsAndSwaps.
func TestHandleSetModel(t *testing.T) {
	// Stub the adapter factory so no real provider credentials are needed.
	orig := newModelAdapter
	newModelAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return &stubAdapter{}, nil
	}
	t.Cleanup(func() { newModelAdapter = orig })

	server, sw, _ := setupModelServerWithSettings(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"name": "gemini-pro"})
	req := httptest.NewRequest("POST", "/api/v1/models/current", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp setModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Current != "gemini-pro" || resp.Provider != "gemini" {
		t.Errorf("resp = %+v, want current=gemini-pro provider=gemini", resp)
	}
	if got := sw.Current(); got != "gemini-pro" {
		t.Errorf("swappable Current() = %q, want gemini-pro (swap not applied)", got)
	}
}

// TestHandleSetModel_NoSettingsServiceReturns503 pins the G4-
// correction invariant: when services.LLMSettings is not wired the
// endpoint MUST refuse rather than fall back to a swap-only path.
// There is no reachable code path that changes the active model
// without writing the settings row and its audit row in one
// transaction — the absence of the service is a refusal, not a
// fallback. The live adapter MUST remain pointed at the original
// model and the status MUST be 503.
func TestHandleSetModel_NoSettingsServiceReturns503(t *testing.T) {
	orig := newModelAdapter
	newModelAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return &stubAdapter{}, nil
	}
	t.Cleanup(func() { newModelAdapter = orig })

	server, sw := setupModelServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"name": "gemini-pro"})
	req := httptest.NewRequest("POST", "/api/v1/models/current", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (no settings service must refuse, not swap-only fallback); body=%s", rec.Code, rec.Body.String())
	}
	if got := sw.Current(); got != "claude-sonnet" {
		t.Errorf("Current() with no settings service = %q, want claude-sonnet (live model must NOT change on a refused request)", got)
	}
}

// TestHandleSetModel_PersistsAuditsAndSwaps covers the Stream G phase
// G4 endpoint behaviour: a successful change-model request persists
// the new value to llm_settings, writes one llm_settings_mutation
// audit row whose context carries target/before/after, and swaps the
// live adapter. The audit row commits together with the settings row
// inside the same transaction in the mutation service; the swap
// happens only after the transaction commits.
func TestHandleSetModel_PersistsAuditsAndSwaps(t *testing.T) {
	orig := newModelAdapter
	newModelAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return &stubAdapter{}, nil
	}
	t.Cleanup(func() { newModelAdapter = orig })

	server, sw, db := setupModelServerWithSettings(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"name": "gemini-pro"})
	req := httptest.NewRequest("POST", "/api/v1/models/current", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// 1) Live swap occurred.
	if got := sw.Current(); got != "gemini-pro" {
		t.Errorf("Current() after success = %q, want gemini-pro", got)
	}

	// 2) Settings row persisted.
	var stored string
	if err := db.QueryRowContext(context.Background(),
		`SELECT active_model FROM llm_settings WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatalf("read active_model: %v", err)
	}
	if stored != "gemini-pro" {
		t.Errorf("llm_settings.active_model = %q, want gemini-pro (persist must succeed)", stored)
	}

	// 3) Audit row written with the established target/before/after
	// vocabulary.
	var action, ctxJSON string
	if err := db.QueryRowContext(context.Background(),
		`SELECT action, context FROM audit_log WHERE kind = ? ORDER BY id DESC LIMIT 1`,
		string(audit.KindLLMSettingsMutation),
	).Scan(&action, &ctxJSON); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if action != audit.ActionLLMSetActiveModel {
		t.Errorf("audit action = %q, want %q", action, audit.ActionLLMSetActiveModel)
	}
	var blob map[string]any
	if err := json.Unmarshal([]byte(ctxJSON), &blob); err != nil {
		t.Fatalf("decode audit context: %v", err)
	}
	if got := blob[llmsettings.AuditCtxTarget]; got != llmsettings.AuditCtxTargetActiveModel {
		t.Errorf("audit target = %v, want %q", got, llmsettings.AuditCtxTargetActiveModel)
	}
	if got := blob[llmsettings.AuditCtxBefore]; got != "" {
		t.Errorf("audit before = %v, want \"\" (the seed)", got)
	}
	if got := blob[llmsettings.AuditCtxAfter]; got != "gemini-pro" {
		t.Errorf("audit after = %v, want gemini-pro", got)
	}
}

// failingSettingsService forces the mutation to fail so the endpoint
// can assert the live model is unchanged when persist fails.
type failingMutation struct{}

func (failingMutation) Insert(_ context.Context, _ audit.Event) error { return nil }
func (failingMutation) InsertTx(_ context.Context, _ *sql.Tx, _ audit.Event) error {
	return errors.New("forced audit failure")
}

// TestHandleSetModel_MutationFailureLeavesLiveModelUnchanged is the
// load-bearing durability test for the change-model endpoint. We
// inject a failing audit repository so the mutation service's
// transaction rolls back: NEITHER llm_settings.active_model NOR the
// audit row persists, AND the live swappable adapter still points at
// the original model. The endpoint returns 500 with an error message.
//
// This is the regression guard against the "swap first, persist later"
// failure mode: if the implementation ever swapped before persisting,
// a transient audit-store failure would leave the live model out of
// sync with the table.
func TestHandleSetModel_MutationFailureLeavesLiveModelUnchanged(t *testing.T) {
	orig := newModelAdapter
	newModelAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return &stubAdapter{}, nil
	}
	t.Cleanup(func() { newModelAdapter = orig })

	// Build a settings stack whose mutation will fail. The repository
	// has a real database (the settings UPDATE inside the transaction
	// still has to run for the rollback to be meaningful), but the
	// audit sink is the failing stub.
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	settingsRepo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(settingsRepo, failingMutation{})

	sw := llm.NewSwappableAdapter(&stubAdapter{}, "claude-sonnet")
	services := &core.Services{
		Config: &config.Config{
			LLM: config.LLMConfig{
				Current: "claude-sonnet",
				Available: map[string]config.ModelConfig{
					"claude-sonnet": {Provider: "claude", Model: "claude-sonnet-4-20250514"},
					"gemini-pro":    {Provider: "gemini", Model: "gemini-2.0-pro"},
				},
			},
		},
		LLM:         sw,
		LLMSettings: svc,
	}
	server := New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"name": "gemini-pro"})
	req := httptest.NewRequest("POST", "/api/v1/models/current", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (mutation failure must surface as 5xx); body=%s", rec.Code, rec.Body.String())
	}

	// Live model untouched.
	if got := sw.Current(); got != "claude-sonnet" {
		t.Errorf("Current() after mutation failure = %q, want claude-sonnet (live model must be unchanged when persist fails)", got)
	}
	// Settings row untouched (still the migration seed).
	var stored string
	if err := s.DB().QueryRowContext(context.Background(),
		`SELECT active_model FROM llm_settings WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatalf("read active_model: %v", err)
	}
	if stored != "" {
		t.Errorf("llm_settings.active_model = %q, want \"\" (rollback must leave the row untouched)", stored)
	}
}

// TestHandleSetModelErrors covers the error paths that fail BEFORE
// the persist+audit transaction runs (input validation, missing
// provider key). Stream G phase G4 correction: this test now uses
// the same real settings harness as the happy path — there is no
// swap-only fallback to exercise. Each subtest additionally asserts
// that NO settings row was persisted and NO audit row was written:
// a pre-persist failure must leave both the live adapter AND the
// durable state untouched.
func TestHandleSetModelErrors(t *testing.T) {
	orig := newModelAdapter
	t.Cleanup(func() { newModelAdapter = orig })

	tests := []struct {
		name       string
		body       any
		factory    func(context.Context, config.ModelConfig) (llm.LLMAdapter, error)
		wantStatus int
	}{
		{
			name:       "unknown model",
			body:       map[string]string{"name": "nope"},
			factory:    func(context.Context, config.ModelConfig) (llm.LLMAdapter, error) { return &stubAdapter{}, nil },
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty name",
			body:       map[string]string{"name": ""},
			factory:    func(context.Context, config.ModelConfig) (llm.LLMAdapter, error) { return &stubAdapter{}, nil },
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "adapter creation fails (e.g. missing key)",
			body: map[string]string{"name": "gemini-pro"},
			factory: func(context.Context, config.ModelConfig) (llm.LLMAdapter, error) {
				return nil, fmt.Errorf("GEMINI_API_KEY not set")
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newModelAdapter = tt.factory
			server, sw, db := setupModelServerWithSettings(t)
			mux := http.NewServeMux()
			server.RegisterRoutes(mux)

			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/api/v1/models/current", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			// On any error the active model must remain unchanged.
			if got := sw.Current(); got != "claude-sonnet" {
				t.Errorf("after failed swap, Current() = %q, want claude-sonnet", got)
			}
			// Pre-persist failures must NOT touch the settings row.
			var stored string
			if err := db.QueryRowContext(context.Background(),
				`SELECT active_model FROM llm_settings WHERE id = 1`).Scan(&stored); err != nil {
				t.Fatalf("read active_model: %v", err)
			}
			if stored != "" {
				t.Errorf("llm_settings.active_model = %q, want \"\" (pre-persist failure must leave row untouched)", stored)
			}
			// Pre-persist failures must NOT write an audit row.
			var n int
			if err := db.QueryRowContext(context.Background(),
				`SELECT COUNT(*) FROM audit_log WHERE kind = ?`,
				string(audit.KindLLMSettingsMutation),
			).Scan(&n); err != nil {
				t.Fatalf("count audit_log: %v", err)
			}
			if n != 0 {
				t.Errorf("llm_settings_mutation audit row count = %d, want 0 (pre-persist failure must NOT produce a phantom audit row)", n)
			}
		})
	}
}
