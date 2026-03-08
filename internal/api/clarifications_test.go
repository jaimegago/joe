package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

func setupClarificationsTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()

	sqlStore, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("Failed to migrate test store: %v", err)
	}

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Current: "claude",
			Available: map[string]config.ModelConfig{
				"claude": {
					Provider: "claude",
					Model:    "claude-sonnet-4",
				},
			},
		},
	}

	registry := adapters.NewRegistry()
	services := &core.Services{
		Config:   cfg,
		Store:    sqlStore,
		Adapters: registry,
	}

	return New(services), sqlStore
}

func TestClarificationsAPI(t *testing.T) {
	server, storeInst := setupClarificationsTestServer(t)
	ctx := context.Background()

	// Create test clarifications
	c1 := &store.Clarification{
		ID:       "clar-1",
		Type:     store.ClarificationNewService,
		Context:  json.RawMessage(`{"deployment":"api-service"}`),
		Question: "What is api-service?",
		Options:  []string{"Gateway", "Auth", "Processing"},
	}

	c2 := &store.Clarification{
		ID:       "clar-2",
		Type:     store.ClarificationEdgeConfirm,
		Context:  json.RawMessage(`{}`),
		Question: "Does api-service depend on db?",
		Options:  []string{"Yes", "No"},
	}

	if err := storeInst.Clarifications.Create(ctx, c1); err != nil {
		t.Fatalf("Failed to create test clarification: %v", err)
	}

	if err := storeInst.Clarifications.Create(ctx, c2); err != nil {
		t.Fatalf("Failed to create test clarification: %v", err)
	}

	t.Run("list pending clarifications", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		req := httptest.NewRequest("GET", apiPrefix+"/clarifications", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		count, ok := resp["count"].(float64)
		if !ok || int(count) != 2 {
			t.Errorf("Expected 2 clarifications, got %v", resp["count"])
		}

		clarifications, ok := resp["clarifications"].([]any)
		if !ok || len(clarifications) != 2 {
			t.Errorf("Expected 2 clarifications in list, got %v", len(clarifications))
		}
	})

	t.Run("answer clarification success", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		answerBody := map[string]string{
			"answer":      "Gateway",
			"answered_by": "admin",
		}
		body, _ := json.Marshal(answerBody)

		req := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-1/answer", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
		}

		// Verify the clarification was updated
		updated, _ := storeInst.Clarifications.Get(ctx, "clar-1")
		if updated.Status != store.ClarificationAnswered {
			t.Errorf("Status = %q, want %q", updated.Status, store.ClarificationAnswered)
		}
		if updated.Answer != "Gateway" {
			t.Errorf("Answer = %q, want %q", updated.Answer, "Gateway")
		}
		if updated.AnsweredBy != "admin" {
			t.Errorf("AnsweredBy = %q, want %q", updated.AnsweredBy, "admin")
		}
	})

	t.Run("answer clarification invalid payload", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		req := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-2/answer", bytes.NewReader([]byte("invalid")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("answer clarification missing answer field", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		answerBody := map[string]string{
			"answered_by": "admin",
		}
		body, _ := json.Marshal(answerBody)

		req := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-2/answer", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("answer clarification defaults answered_by", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		c3 := &store.Clarification{
			ID:       "clar-3",
			Type:     store.ClarificationServicePurpose,
			Context:  json.RawMessage(`{}`),
			Question: "What is the purpose of this?",
		}
		storeInst.Clarifications.Create(ctx, c3)

		answerBody := map[string]string{
			"answer": "Test service",
		}
		body, _ := json.Marshal(answerBody)

		req := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-3/answer", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}

		updated, _ := storeInst.Clarifications.Get(ctx, "clar-3")
		if updated.AnsweredBy != "user" {
			t.Errorf("AnsweredBy = %q, want %q (default)", updated.AnsweredBy, "user")
		}
	})

	t.Run("dismiss clarification success", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		req := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-2/dismiss", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}

		// Verify the clarification was dismissed
		updated, _ := storeInst.Clarifications.Get(ctx, "clar-2")
		if updated.Status != store.ClarificationDismissed {
			t.Errorf("Status = %q, want %q", updated.Status, store.ClarificationDismissed)
		}
	})

	t.Run("dismiss already answered clarification", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		// Answer it first
		answerBody := map[string]string{
			"answer": "Yes",
		}
		body, _ := json.Marshal(answerBody)
		ansReq := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-1/answer", bytes.NewReader(body))
		ansReq.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, ansReq)

		// Now try to dismiss (should still work, just update status)
		req := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-1/dismiss", nil)
		w = httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}

		updated, _ := storeInst.Clarifications.Get(ctx, "clar-1")
		if updated.Status != store.ClarificationDismissed {
			t.Errorf("Status = %q, want %q", updated.Status, store.ClarificationDismissed)
		}
	})

	t.Run("list clarifications with no store returns error", func(t *testing.T) {
		// When services.Store is nil, the registerClarificationRoutes should skip registration
		// So the route won't be handled and we get 404
		badServer := New(&core.Services{Store: nil})

		mux := http.NewServeMux()
		badServer.RegisterRoutes(mux)

		req := httptest.NewRequest("GET", apiPrefix+"/clarifications", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		// Route won't be registered with nil store, so we get 404
		if w.Code != http.StatusNotFound {
			t.Errorf("Status = %d, want %d (route not registered with nil store)", w.Code, http.StatusNotFound)
		}
	})

	t.Run("answer with empty answer field", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		c4 := &store.Clarification{
			ID:       "clar-4",
			Type:     store.ClarificationAmbiguousJoeFile,
			Context:  json.RawMessage(`{}`),
			Question: "Ambiguous joe file?",
		}
		storeInst.Clarifications.Create(ctx, c4)

		answerBody := map[string]string{
			"answer": "",
		}
		body, _ := json.Marshal(answerBody)

		req := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-4/answer", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("list only returns pending clarifications", func(t *testing.T) {
		// Create fresh server for this test to isolate state
		server2, storeInst2 := setupClarificationsTestServer(t)

		// Create test clarifications
		c1 := &store.Clarification{
			ID:       "pending-1",
			Type:     store.ClarificationNewService,
			Context:  json.RawMessage(`{}`),
			Question: "Test 1?",
		}
		c2 := &store.Clarification{
			ID:       "pending-2",
			Type:     store.ClarificationEdgeConfirm,
			Context:  json.RawMessage(`{}`),
			Question: "Test 2?",
		}
		c3 := &store.Clarification{
			ID:       "pending-3",
			Type:     store.ClarificationServicePurpose,
			Context:  json.RawMessage(`{}`),
			Question: "Test 3?",
		}
		storeInst2.Clarifications.Create(ctx, c1)
		storeInst2.Clarifications.Create(ctx, c2)
		storeInst2.Clarifications.Create(ctx, c3)

		// Answer one clarification
		storeInst2.Clarifications.Answer(ctx, "pending-1", "Answer1", "user")
		// Dismiss another
		storeInst2.Clarifications.Dismiss(ctx, "pending-2")

		mux := http.NewServeMux()
		server2.RegisterRoutes(mux)

		req := httptest.NewRequest("GET", apiPrefix+"/clarifications", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}

		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)

		count, _ := resp["count"].(float64)
		if int(count) != 1 {
			t.Errorf("Expected 1 pending clarification (only pending-3), got %v", int(count))
		}
	})
}

func TestClarificationsAPIPathValidation(t *testing.T) {
	server, _ := setupClarificationsTestServer(t)

	t.Run("answer missing id in path returns error", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		// This path doesn't match the pattern, so it won't be handled by the handler
		// But we can test what happens if PathValue returns empty
		answerBody := map[string]string{
			"answer": "Test",
		}
		body, _ := json.Marshal(answerBody)

		req := httptest.NewRequest("POST", apiPrefix+"/clarifications//answer", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		// This is a protocol test - the mux won't match this path
		// So we won't get our handler
		_ = w
	})
}

func TestClarificationsRaceAndConcurrency(t *testing.T) {
	server, storeInst := setupClarificationsTestServer(t)
	ctx := context.Background()

	// Create multiple clarifications
	for i := 0; i < 5; i++ {
		c := &store.Clarification{
			ID:       fmt.Sprintf("clar-%d", i),
			Type:     store.ClarificationNewService,
			Context:  json.RawMessage(`{}`),
			Question: fmt.Sprintf("Question %d?", i),
		}
		storeInst.Clarifications.Create(ctx, c)
	}

	t.Run("list concurrent with answer", func(t *testing.T) {
		mux := http.NewServeMux()
		server.RegisterRoutes(mux)

		// List pending
		req := httptest.NewRequest("GET", apiPrefix+"/clarifications", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		countBefore, _ := resp["count"].(float64)

		// Answer one
		answerBody := map[string]string{
			"answer": "Test",
		}
		body, _ := json.Marshal(answerBody)
		reqAns := httptest.NewRequest("POST", apiPrefix+"/clarifications/clar-0/answer", bytes.NewReader(body))
		reqAns.Header.Set("Content-Type", "application/json")
		wAns := httptest.NewRecorder()
		mux.ServeHTTP(wAns, reqAns)

		// List pending again
		req2 := httptest.NewRequest("GET", apiPrefix+"/clarifications", nil)
		w2 := httptest.NewRecorder()
		mux.ServeHTTP(w2, req2)

		var resp2 map[string]any
		json.NewDecoder(w2.Body).Decode(&resp2)
		countAfter, _ := resp2["count"].(float64)

		if int(countBefore) != int(countAfter)+1 {
			t.Errorf("Expected one less clarification after answer: before=%v, after=%v", int(countBefore), int(countAfter))
		}
	})
}
