# Instrumentation

Joe includes built-in instrumentation to track LLM API calls, token usage, and performance.

## Features

The instrumentation layer provides:
- **Call counting** - Track total number of API calls
- **Token usage** - Monitor input/output tokens per call and cumulative
- **Latency tracking** - Measure API response times
- **Error tracking** - Count and log failed calls
- **Structured logging** - All metrics logged with structured fields

## How It Works

Joe uses the **decorator pattern** to wrap LLM adapters with instrumentation. This keeps business logic clean and makes instrumentation optional.

```
User Request → Agent → InstrumentedAdapter → Actual LLM Client → API
                            ↓
                      Metrics & Logs
```

The `InstrumentedAdapter` wraps any `LLMAdapter` and logs:
- Request: tool count, message count, call number
- Response: duration, tokens (input/output/total), tool calls
- Errors: error details, duration, call number
- Cumulative stats: total tokens used across all calls

## Configuration

### Log Level

Set in [config.yaml](../config.yaml):
```yaml
logging:
  level: info  # debug, info, warn, error
  file: ""     # empty = stdout, or path to log file
```

### Log Output

**To stdout (default):**
```yaml
logging:
  level: info
  file: ""
```

**To file:**
```yaml
logging:
  level: debug
  file: "/var/log/joe/joe.log"
```

When logging to a file, Joe uses JSON format for easier parsing. When logging to stdout, it uses human-readable text format.

## Example Output

### Info Level (Default)
```
2026/02/02 21:20:03 INFO llm_request tools_count=2 messages_count=3 call_number=1
2026/02/02 21:20:04 INFO llm_response duration_ms=523 input_tokens=156 output_tokens=89 total_tokens=245 tool_calls=1 call_number=1 total_input_tokens=156 total_output_tokens=89
2026/02/02 21:20:04 INFO llm_request tools_count=2 messages_count=4 call_number=2
2026/02/02 21:20:05 INFO llm_response duration_ms=412 input_tokens=178 output_tokens=45 total_tokens=223 tool_calls=0 call_number=2 total_input_tokens=334 total_output_tokens=134
```

### Debug Level
```
2026/02/02 21:20:03 DEBUG llm_request tools_count=2 messages_count=3 call_number=1
2026/02/02 21:20:04 DEBUG llm_response duration_ms=523 input_tokens=156 output_tokens=89 total_tokens=245 tool_calls=1 call_number=1 total_input_tokens=156 total_output_tokens=89
```

## Metrics Tracked

### Per-Call Metrics
- `duration_ms` - API call latency in milliseconds
- `input_tokens` - Tokens in the request
- `output_tokens` - Tokens in the response
- `total_tokens` - Sum of input + output tokens
- `tool_calls` - Number of tool calls in response
- `call_number` - Sequence number of this call

### Cumulative Metrics
- `total_input_tokens` - Sum of all input tokens across calls
- `total_output_tokens` - Sum of all output tokens across calls
- Total calls and errors tracked internally

## Implementation Details

### Decorator Pattern

The instrumentation follows the decorator pattern from CLAUDE.md:

```go
// Create base LLM adapter
baseAdapter, _ := gemini.NewClient(ctx, model)

// Wrap with instrumentation
instrumentedAdapter := llm.NewInstrumentedAdapter(baseAdapter, logger)

// Use as normal LLMAdapter
response, _ := instrumentedAdapter.Chat(ctx, request)
```

This approach:
- ✅ Keeps business logic clean (no instrumentation code in agent/tools)
- ✅ Makes instrumentation optional and composable
- ✅ Allows easy addition of other decorators (caching, rate limiting, etc.)
- ✅ Follows separation of concerns

### Thread Safety

All metrics use atomic operations (`atomic.Int64`) for thread-safe counting, making the instrumentation safe for concurrent use.

### Zero Dependencies

The instrumentation uses only Go standard library (`log/slog`), with no external dependencies.

## Accessing Metrics Programmatically

The `InstrumentedAdapter` provides a `GetStats()` method:

```go
stats := instrumentedAdapter.GetStats()
fmt.Printf("Total calls: %d\n", stats.TotalCalls)
fmt.Printf("Total tokens: %d\n", stats.TotalTokens)
fmt.Printf("Error rate: %.2f%%\n", float64(stats.TotalErrors)/float64(stats.TotalCalls)*100)
```

Or log a summary:
```go
instrumentedAdapter.LogStats()
```

Output:
```
INFO llm_stats_summary total_calls=15 total_errors=0 total_input_tokens=2341 total_output_tokens=1156 total_tokens=3497 error_rate=0
```

## Cost Estimation

Use the token counts to estimate API costs:

**Gemini Pricing (example):**
- Input: $0.075 per 1M tokens
- Output: $0.30 per 1M tokens

**Cost calculation:**
```
Input cost = (total_input_tokens / 1,000,000) × $0.075
Output cost = (total_output_tokens / 1,000,000) × $0.30
Total cost = Input cost + Output cost
```

You can parse the logs to extract these metrics and calculate costs automatically.

## Future Enhancements

Potential additions:
- Prometheus metrics exporter
- OpenTelemetry tracing integration
- Cost tracking per session
- Rate limiting based on token usage
- Alerting on error thresholds
- Metrics dashboard

## Testing

See [internal/llm/instrumented_test.go](../internal/llm/instrumented_test.go) for examples of testing instrumented adapters.

Run instrumentation tests:
```bash
go test ./internal/llm -v -run Instrumented
```
