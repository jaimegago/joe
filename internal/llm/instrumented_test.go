package llm

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

// mockLLMForInstrumentation is a mock LLM for testing instrumentation
type mockLLMForInstrumentation struct {
	shouldError bool
	response    *ChatResponse
}

func (m *mockLLMForInstrumentation) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if m.shouldError {
		return nil, errors.New("mock error")
	}
	return m.response, nil
}

func TestNewInstrumentedAdapter(t *testing.T) {
	mock := &mockLLMForInstrumentation{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	instrumented := NewInstrumentedAdapter(mock, logger, "test-provider", "test-model")

	if instrumented == nil {
		t.Fatal("NewInstrumentedAdapter returned nil")
	}
	if instrumented.adapter != mock {
		t.Error("Adapter not properly wrapped")
	}
	if instrumented.logger != logger {
		t.Error("Logger not properly set")
	}
}

func TestInstrumentedAdapter_Chat_Success(t *testing.T) {
	mockResponse := &ChatResponse{
		Content: "test response",
		Usage: TokenUsage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	}

	mock := &mockLLMForInstrumentation{
		shouldError: false,
		response:    mockResponse,
	}

	instrumented := NewInstrumentedAdapter(mock, nil, "test-provider", "test-model")
	ctx := context.Background()
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	resp, err := instrumented.Chat(ctx, req)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != mockResponse.Content {
		t.Errorf("Expected content %q, got %q", mockResponse.Content, resp.Content)
	}

	stats := instrumented.GetStats()
	if stats.TotalCalls != 1 {
		t.Errorf("Expected 1 call, got %d", stats.TotalCalls)
	}
	if stats.TotalErrors != 0 {
		t.Errorf("Expected 0 errors, got %d", stats.TotalErrors)
	}
	if stats.TotalInputTokens != 10 {
		t.Errorf("Expected 10 input tokens, got %d", stats.TotalInputTokens)
	}
	if stats.TotalOutputTokens != 20 {
		t.Errorf("Expected 20 output tokens, got %d", stats.TotalOutputTokens)
	}
	if stats.TotalTokens != 30 {
		t.Errorf("Expected 30 total tokens, got %d", stats.TotalTokens)
	}
}

func TestInstrumentedAdapter_Chat_Error(t *testing.T) {
	mock := &mockLLMForInstrumentation{
		shouldError: true,
	}

	instrumented := NewInstrumentedAdapter(mock, nil, "test-provider", "test-model")
	ctx := context.Background()
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	_, err := instrumented.Chat(ctx, req)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	stats := instrumented.GetStats()
	if stats.TotalCalls != 1 {
		t.Errorf("Expected 1 call, got %d", stats.TotalCalls)
	}
	if stats.TotalErrors != 1 {
		t.Errorf("Expected 1 error, got %d", stats.TotalErrors)
	}
}

func TestInstrumentedAdapter_MultipleCalls(t *testing.T) {
	mockResponse := &ChatResponse{
		Content: "test response",
		Usage: TokenUsage{
			InputTokens:  5,
			OutputTokens: 10,
			TotalTokens:  15,
		},
	}

	mock := &mockLLMForInstrumentation{
		response: mockResponse,
	}

	instrumented := NewInstrumentedAdapter(mock, nil, "test-provider", "test-model")
	ctx := context.Background()
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	// Make 3 successful calls
	for i := 0; i < 3; i++ {
		_, err := instrumented.Chat(ctx, req)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i+1, err)
		}
	}

	stats := instrumented.GetStats()
	if stats.TotalCalls != 3 {
		t.Errorf("Expected 3 calls, got %d", stats.TotalCalls)
	}
	if stats.TotalInputTokens != 15 { // 5 * 3
		t.Errorf("Expected 15 input tokens, got %d", stats.TotalInputTokens)
	}
	if stats.TotalOutputTokens != 30 { // 10 * 3
		t.Errorf("Expected 30 output tokens, got %d", stats.TotalOutputTokens)
	}
}

// mockAPIDetailedError implements APIErrorDetails for testing the api error branch in Chat
type mockAPIDetailedError struct {
	code    int
	message string
}

func (e *mockAPIDetailedError) Error() string      { return e.message }
func (e *mockAPIDetailedError) APICode() int       { return e.code }
func (e *mockAPIDetailedError) APIMessage() string { return e.message }

// mockLLMReturnsAPIError always returns a specific error for all methods
type mockLLMReturnsAPIError struct {
	err error
}

func (m *mockLLMReturnsAPIError) Chat(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return nil, m.err
}

func TestInstrumentedAdapter_Chat_WithAPIErrorDetails(t *testing.T) {
	apiErr := &mockAPIDetailedError{code: 401, message: "unauthorized"}
	mock := &mockLLMReturnsAPIError{err: apiErr}

	instrumented := NewInstrumentedAdapter(mock, nil, "test-provider", "test-model")
	ctx := context.Background()
	req := ChatRequest{Messages: []Message{{Role: "user", Content: "test"}}}

	_, err := instrumented.Chat(ctx, req)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	stats := instrumented.GetStats()
	if stats.TotalCalls != 1 {
		t.Errorf("Expected 1 call, got %d", stats.TotalCalls)
	}
	if stats.TotalErrors != 1 {
		t.Errorf("Expected 1 error, got %d", stats.TotalErrors)
	}
}

func TestNewInstrumentedAdapter_WithLogger(t *testing.T) {
	mock := &mockLLMForInstrumentation{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	instrumented := NewInstrumentedAdapter(mock, logger, "test-provider", "test-model")
	if instrumented == nil {
		t.Fatal("Expected non-nil adapter")
	}
	if instrumented.provider != "test-provider" {
		t.Errorf("provider = %q, want %q", instrumented.provider, "test-provider")
	}
	if instrumented.model != "test-model" {
		t.Errorf("model = %q, want %q", instrumented.model, "test-model")
	}
}
