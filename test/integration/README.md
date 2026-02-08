# Instrumentation Tests

Contract-based tests that verify OpenTelemetry metrics and traces are correctly emitted by Joe's LLM instrumentation. These tests provide the same level of certainty as `if err != nil` checks.

## Running Tests

```bash
# Run all integration tests (including instrumentation tests)
make test-integration

# Run just instrumentation contract tests
go test -v -tags=integration ./test/integration/... -run TestInstrumentation_Contract

# Run all instrumentation tests (including GetStats)
go test -v -tags=integration ./test/integration/... -run TestInstrumentation
```

## Test Architecture

### Contract Definition ([instrumentation_contract.yaml](./instrumentation_contract.yaml))

YAML file defining all required metrics and traces:
- **Metrics**: llm.requests, llm.errors, llm.tokens.input, llm.tokens.output, llm.request.duration
- **Required Attributes**: llm.provider, llm.model, operation, error status
- **Test Scenarios**: Given/When/Then format for validation

### Test Infrastructure ([otel_test_helpers.go](./otel_test_helpers.go))

`OTelTestHarness` provides in-memory OpenTelemetry verification:

**Setup/Teardown:**
- `NewOTelTestHarness(t)` - Creates harness with ManualReader and SpanRecorder
- `Install()` - Replaces global OTel providers (`otel.SetMeterProvider`, `otel.SetTracerProvider`)
- `Cleanup()` - Restores original providers

**Metric Assertions:**
- `AssertCounterIncremented(name, expectedValue, attrs...)` - Verifies counter value and attributes
- `AssertHistogramRecorded(name, attrs...)` - Verifies histogram recorded values

**Trace Assertions:**
- `AssertSpanExists(name)` - Verifies span creation
- `AssertSpanHasAttribute(name, key, value)` - Verifies span attributes
- `AssertSpanStatus(name, status)` - Verifies OK vs Error status
- `AssertSpanHasError(name)` - Verifies error events recorded

**Implementation Details:**
- Uses `go.opentelemetry.io/otel/sdk/metric.ManualReader` for synchronous metric collection
- Uses `go.opentelemetry.io/otel/sdk/trace/tracetest.SpanRecorder` for in-memory trace capture
- Stores original providers as interfaces (`metric.MeterProvider`, `trace.TracerProvider`) for restoration

## How to Write Instrumentation Tests

### Basic Pattern

```go
func TestYourInstrumentation(t *testing.T) {
    // 1. Setup: Install OTel test harness
    harness := NewOTelTestHarness(t)
    defer harness.Cleanup()
    harness.Install() // Replaces global OTel providers

    // 2. Given: Create instrumented component
    mockLLM := mocks.NewMockLLM()
    mockLLM.DefaultResponse = &llm.ChatResponse{
        Content: "Test response",
        Usage: llm.TokenUsage{
            InputTokens:  100,
            OutputTokens: 50,
        },
    }
    adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")

    // 3. When: Perform operation
    ctx := context.Background()
    resp, err := adapter.Chat(ctx, llm.ChatRequest{
        Messages: []llm.Message{{Role: "user", Content: "Hello"}},
    })

    // 4. Then: Assert metrics were emitted
    harness.AssertCounterIncremented("llm.requests", 1,
        attribute.String("llm.provider", "test-provider"),
        attribute.String("llm.model", "test-model"),
        attribute.String("operation", "chat"),
    )
    
    harness.AssertCounterIncremented("llm.tokens.input", 100,
        attribute.String("llm.provider", "test-provider"),
        attribute.String("llm.model", "test-model"),
        attribute.String("operation", "chat"),
    )
}
```

### Testing Error Scenarios

```go
func TestErrorMetrics(t *testing.T) {
    harness := NewOTelTestHarness(t)
    defer harness.Cleanup()
    harness.Install()

    // Given: Adapter configured to fail
    mockLLM := mocks.NewMockLLM()
    mockLLM.ShouldError = true
    adapter := llm.NewInstrumentedAdapter(mockLLM, nil, "test-provider", "test-model")

    // When: Operation fails
    _, err := adapter.Chat(ctx, req)

    // Then: Error metrics emitted
    harness.AssertCounterIncremented("llm.errors", 1,
        attribute.String("llm.provider", "test-provider"),
        attribute.String("llm.model", "test-model"),
        attribute.String("operation", "chat"),
    )
    
    // Latency recorded with error=true
    harness.AssertHistogramRecorded("llm.request.duration",
        attribute.String("llm.provider", "test-provider"),
        attribute.String("llm.model", "test-model"),
        attribute.String("operation", "chat"),
        attribute.Bool("error", true),
    )
}
```

### Adding New Metrics to the Contract

1. **Update [instrumentation_contract.yaml](./instrumentation_contract.yaml):**
```yaml
metrics:
  - name: llm.your_new_metric
    type: counter
    description: What this metric measures
    attributes:
      - llm.provider
      - llm.model
      - your.custom.attribute
```

2. **Add test scenario in [instrumentation_test.go](./instrumentation_test.go):**
```go
func testInstrumentedAdapter_YourNewScenario(t *testing.T) {
    // Given/When/Then pattern
    harness := NewOTelTestHarness(t)
    defer harness.Cleanup()
    harness.Install()
    
    // ... your test implementation
    
    harness.AssertCounterIncremented("llm.your_new_metric", expectedValue, attrs...)
}
```

3. **Register in parent test:**
```go
func testInstrumentedAdapter(t *testing.T) {
    t.Run("YourNewScenario", testInstrumentedAdapter_YourNewScenario)
}
```

## Current Test Coverage

- ✅ **Success scenarios** - llm.requests, llm.tokens.input, llm.tokens.output, llm.request.duration
- ✅ **Failure scenarios** - llm.errors with proper error attributes
- ✅ **Cumulative tracking** - Token counters accumulate correctly across requests
- ✅ **Latency measurement** - Histogram records both success (error=false) and error (error=true)
- ✅ **API error codes** - api_error_code attribute captured from APIErrorDetails interface
- ✅ **In-memory stats** - GetStats() method validation independent of OTel
- ✅ **Attribute validation** - All required attributes present on metrics

## Why This Approach Works

**Certainty Level:**
1. **Direct Verification** - Captures actual emitted metrics/traces, not mocks
2. **Contract-Based** - YAML contract defines what MUST be emitted
3. **Attribute Validation** - Verifies metrics have correct attributes, not just presence
4. **Value Validation** - Checks actual metric values and cumulative behavior
5. **Real Code Path** - Tests wire through actual instrumentation implementation

**Advantages over mocking:**
- Detects missing instrumentation calls
- Verifies attribute correctness
- Catches cumulative value bugs
- Tests global provider integration
- Validates OpenTelemetry SDK usage

## OTel SDK References

The test infrastructure uses official OpenTelemetry Go SDK testing components:
- [`go.opentelemetry.io/otel/sdk/metric`](https://pkg.go.dev/go.opentelemetry.io/otel/sdk/metric) - Metric SDK with ManualReader
- [`go.opentelemetry.io/otel/sdk/trace/tracetest`](https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace/tracetest) - Trace test utilities
- [`go.opentelemetry.io/otel`](https://pkg.go.dev/go.opentelemetry.io/otel) - Global provider registration

## Extending for Traces

While current tests focus on metrics, the harness supports trace validation:

```go
// Assert span exists
harness.AssertSpanExists("llm.chat")

// Assert span has attribute
harness.AssertSpanHasAttribute("llm.chat", "llm.provider", "test-provider")

// Assert span status
harness.AssertSpanStatus("llm.chat", codes.Ok)

// Assert span has error
harness.AssertSpanHasError("llm.chat")
```

Add trace tests when implementing distributed tracing or span-based debugging features.
