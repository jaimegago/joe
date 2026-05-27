package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/llm"
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

func TestHandleSetModel(t *testing.T) {
	// Stub the adapter factory so no real provider credentials are needed.
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
			server, sw := setupModelServer(t)
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
		})
	}
}
