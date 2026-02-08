//go:build integration
// +build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/test/mocks"
	"go.opentelemetry.io/otel/attribute"
)

// TestInstrumentation_Contract validates that all instrumentation defined in
// instrumentation_contract.yaml is properly emitted.
// These tests provide certainty similar to "if err != nil" checks.
func TestInstrumentation_Contract(t *testing.T) {
	t.Run("InstrumentedAdapter", testInstrumentedAdapter)
}

func testInstrumentedAdapter(t *testing.T) {
	t.Run("SuccessfulChatRequest", testInstrumentedAdapter_SuccessfulChat)
	t.Run("FailedChatRequest", testInstrumentedAdapter_FailedChat)
	t.Run("TokenTracking", testInstrumentedAdapter_TokenTracking)
	t.Run("LatencyMetrics", testInstrumentedAdapter_LatencyMetrics)
	t.Run("ErrorWithAPICode", testInstrumentedAdapter_ErrorWithAPICode)
}

// Scenario: Successful Chat Request
// Given an instrumented LLM adapter with a mock backend
// When a chat request is made and succeeds
// Then metrics are emitted:
//   - llm.requests counter increments by 1
//   - llm.tokens.input counter increments by actual input tokens
//   - llm.tokens.output counter increments by actual output tokens
//   - llm.request.duration histogram records the latency
func testInstrumentedAdapter_SuccessfulChat(t *testing.T) {
	// Setup: Install OTel test harness
	harness := NewOTelTestHarness(t)
	defer harness.Cleanup()
	harness.Install()

	// Given: Create instrumented adapter
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: "Test response",
		Usage: llm.TokenUsage{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	}

	adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")

	// When: Perform chat operation
	ctx := context.Background()
	req := llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	resp, err := adapter.Chat(ctx, req)

	// Then: Assert success
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	// Then: Assert metrics were emitted
	baseAttrs := []attribute.KeyValue{
		attribute.String("llm.provider", "test-provider"),
		attribute.String("llm.model", "test-model"),
		attribute.String("operation", "chat"),
	}

	// Verify llm.requests counter
	harness.AssertCounterIncremented("llm.requests", 1, baseAttrs...)

	// Verify llm.tokens.input counter
	harness.AssertCounterIncremented("llm.tokens.input", 100, baseAttrs...)

	// Verify llm.tokens.output counter
	harness.AssertCounterIncremented("llm.tokens.output", 50, baseAttrs...)

	// Verify llm.request.duration histogram
	latencyAttrs := append(baseAttrs, attribute.Bool("error", false))
	harness.AssertHistogramRecorded("llm.request.duration", latencyAttrs...)

	t.Log("✅ Successful chat request emitted all required metrics")
}

// Scenario: Failed Chat Request
// Given an instrumented LLM adapter
// When a chat request fails
// Then metrics are emitted:
//   - llm.requests counter increments by 1
//   - llm.errors counter increments by 1
//   - llm.request.duration histogram records the latency with error=true
func testInstrumentedAdapter_FailedChat(t *testing.T) {
	// Setup
	harness := NewOTelTestHarness(t)
	defer harness.Cleanup()
	harness.Install()

	// Given: Adapter configured to fail
	mockLLM := mocks.NewMockLLM()
	mockLLM.ShouldError = true
	mockLLM.ErrorMessage = "API connection failed"

	adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")

	// When: Perform chat that fails
	ctx := context.Background()
	req := llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	_, err := adapter.Chat(ctx, req)

	// Then: Assert error occurred
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Then: Assert error metrics
	baseAttrs := []attribute.KeyValue{
		attribute.String("llm.provider", "test-provider"),
		attribute.String("llm.model", "test-model"),
		attribute.String("operation", "chat"),
	}

	// Verify llm.requests counter (errors also count as requests)
	harness.AssertCounterIncremented("llm.requests", 1, baseAttrs...)

	// Verify llm.errors counter
	harness.AssertCounterIncremented("llm.errors", 1, baseAttrs...)

	// Verify latency recorded with error=true
	latencyAttrs := append(baseAttrs, attribute.Bool("error", true))
	harness.AssertHistogramRecorded("llm.request.duration", latencyAttrs...)

	t.Log("✅ Failed chat request emitted all required error metrics")
}

// Scenario: Token Tracking
// Given an instrumented adapter
// When multiple requests are made with different token counts
// Then token counters accurately reflect cumulative usage
func testInstrumentedAdapter_TokenTracking(t *testing.T) {
	// Setup
	harness := NewOTelTestHarness(t)
	defer harness.Cleanup()
	harness.Install()

	// Given: Adapter with specific token responses
	mockLLM := mocks.NewMockLLM()
	adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")
	ctx := context.Background()

	// When: First request
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: "First response",
		Usage: llm.TokenUsage{
			InputTokens:  50,
			OutputTokens: 30,
			TotalTokens:  80,
		},
	}

	_, err := adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "First"}},
	})
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// When: Second request with different token counts
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: "Second response",
		Usage: llm.TokenUsage{
			InputTokens:  75,
			OutputTokens: 45,
			TotalTokens:  120,
		},
	}

	_, err = adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Second"}},
	})
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}

	// Then: Verify cumulative token counts
	baseAttrs := []attribute.KeyValue{
		attribute.String("llm.provider", "test-provider"),
		attribute.String("llm.model", "test-model"),
		attribute.String("operation", "chat"),
	}

	// Input: 50 + 75 = 125
	harness.AssertCounterIncremented("llm.tokens.input", 125, baseAttrs...)

	// Output: 30 + 45 = 75
	harness.AssertCounterIncremented("llm.tokens.output", 75, baseAttrs...)

	t.Log("✅ Token tracking correctly accumulates across requests")
}

// Scenario: Latency Metrics
// Given an instrumented adapter
// When both successful and failed requests are made
// Then latency is recorded for both with appropriate error attribute
func testInstrumentedAdapter_LatencyMetrics(t *testing.T) {
	// Setup
	harness := NewOTelTestHarness(t)
	defer harness.Cleanup()
	harness.Install()

	mockLLM := mocks.NewMockLLM()
	adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")
	ctx := context.Background()

	// When: Successful request
	mockLLM.ShouldError = false
	_, err := adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Success"}},
	})
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	// When: Failed request
	mockLLM.ShouldError = true
	_, err = adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Fail"}},
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Then: Both should have latency recorded with different error attributes
	baseAttrs := []attribute.KeyValue{
		attribute.String("llm.provider", "test-provider"),
		attribute.String("llm.model", "test-model"),
		attribute.String("operation", "chat"),
	}

	// Success latency (error=false)
	successAttrs := append(baseAttrs, attribute.Bool("error", false))
	harness.AssertHistogramRecorded("llm.request.duration", successAttrs...)

	// Error latency (error=true)
	errorAttrs := append(baseAttrs, attribute.Bool("error", true))
	harness.AssertHistogramRecorded("llm.request.duration", errorAttrs...)

	t.Log("✅ Latency metrics recorded correctly for both success and error cases")
}

// APIError implements both error and APIErrorDetails for testing
type APIError struct {
	message string
	code    int
}

func (e APIError) Error() string      { return e.message }
func (e APIError) APICode() int       { return e.code }
func (e APIError) APIMessage() string { return e.message }

// MockLLMWithAPIError is a mock that returns errors with API codes
type MockLLMWithAPIError struct {
	errorCode int
}

func (m *MockLLMWithAPIError) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, APIError{message: "Rate limit exceeded", code: m.errorCode}
}

func (m *MockLLMWithAPIError) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (m *MockLLMWithAPIError) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("not implemented")
}

// Scenario: Error with API Code
// Given an instrumented adapter
// When an API error with error code occurs
// Then error metric includes the api_error_code attribute
func testInstrumentedAdapter_ErrorWithAPICode(t *testing.T) {
	// Setup
	harness := NewOTelTestHarness(t)
	defer harness.Cleanup()
	harness.Install()

	// Given: Adapter that returns API errors
	mockLLM := &MockLLMWithAPIError{errorCode: 429}
	adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")

	// When: Request fails with API error code
	ctx := context.Background()
	_, err := adapter.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Test"}},
	})

	// Then: Assert error occurred
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Then: Verify error metric includes API code
	errorAttrs := []attribute.KeyValue{
		attribute.String("llm.provider", "test-provider"),
		attribute.String("llm.model", "test-model"),
		attribute.String("operation", "chat"),
		attribute.Int("api_error_code", 429),
	}

	harness.AssertCounterIncremented("llm.errors", 1, errorAttrs...)

	t.Log("✅ API error code captured in error metrics")
}

// TestInstrumentation_GetStats validates the in-memory stats methods
func TestInstrumentation_GetStats(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: "Test",
		Usage: llm.TokenUsage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	}

	adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")
	ctx := context.Background()

	// Make some requests
	adapter.Chat(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "1"}}})
	adapter.Chat(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "2"}}})

	// Get stats
	stats := adapter.GetStats()

	// Verify in-memory counters
	if stats.TotalCalls != 2 {
		t.Errorf("Expected 2 calls, got %d", stats.TotalCalls)
	}
	if stats.TotalInputTokens != 20 {
		t.Errorf("Expected 20 input tokens, got %d", stats.TotalInputTokens)
	}
	if stats.TotalOutputTokens != 40 {
		t.Errorf("Expected 40 output tokens, got %d", stats.TotalOutputTokens)
	}

	t.Log("✅ GetStats returns accurate in-memory counters")
}
