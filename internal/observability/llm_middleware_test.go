package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

type llmStub struct {
	chatResp  *llm.ChatResponse
	chatErr   error
	embedResp []float32
	embedErr  error
}

func (s *llmStub) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return s.chatResp, s.chatErr
}

func (s *llmStub) Embed(ctx context.Context, text string) ([]float32, error) {
	return s.embedResp, s.embedErr
}

func TestNewLLMMiddleware(t *testing.T) {
	mw, err := NewLLMMiddleware(&llmStub{}, "claude", "sonnet")
	if err != nil {
		t.Fatalf("NewLLMMiddleware error: %v", err)
	}
	if mw == nil || mw.callCounter == nil || mw.errorCounter == nil || mw.durationHistogram == nil || mw.tokenCounter == nil {
		t.Fatal("expected middleware and metrics instruments to be initialized")
	}
}

func TestLLMMiddleware_Chat(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		stub := &llmStub{chatResp: &llm.ChatResponse{Content: "ok", Usage: llm.TokenUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7}}}
		mw, _ := NewLLMMiddleware(stub, "claude", "sonnet")

		resp, err := mw.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatalf("Chat error: %v", err)
		}
		if resp.Content != "ok" || resp.Usage.TotalTokens != 7 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("error", func(t *testing.T) {
		stub := &llmStub{chatErr: errors.New("chat failed")}
		mw, _ := NewLLMMiddleware(stub, "claude", "sonnet")

		_, err := mw.Chat(context.Background(), llm.ChatRequest{})
		if err == nil || err.Error() != "chat failed" {
			t.Fatalf("expected adapter error, got %v", err)
		}
	})
}

func TestLLMMiddleware_Embed(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		stub := &llmStub{embedResp: []float32{0.1, 0.2, 0.3}}
		mw, _ := NewLLMMiddleware(stub, "claude", "sonnet")

		vec, err := mw.Embed(context.Background(), "text")
		if err != nil {
			t.Fatalf("Embed error: %v", err)
		}
		if len(vec) != 3 {
			t.Fatalf("unexpected vector length: %d", len(vec))
		}
	})

	t.Run("error", func(t *testing.T) {
		stub := &llmStub{embedErr: errors.New("embed failed")}
		mw, _ := NewLLMMiddleware(stub, "claude", "sonnet")

		_, err := mw.Embed(context.Background(), "text")
		if err == nil || err.Error() != "embed failed" {
			t.Fatalf("expected adapter error, got %v", err)
		}
	})
}
