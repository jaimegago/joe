# Observability with OpenTelemetry

Joe uses OpenTelemetry for comprehensive observability - traces, metrics, and logs.

## Why OpenTelemetry?

✅ **Industry standard** - CNCF graduated project
✅ **Vendor neutral** - Export to any backend (Prometheus, Jaeger, DataDog, etc.)
✅ **Complete observability** - Traces, metrics, and logs in one framework
✅ **Distributed tracing** - Track requests across services
✅ **Cost tracking** - Token usage metrics for LLM cost estimation

## Architecture

```
┌─────────────┐
│   Joe CLI   │
└──────┬──────┘
       │
       v
┌─────────────────────────────────────────┐
│         Observability Layer             │
│  ┌────────────────────────────────────┐ │
│  │    LLMMiddleware (Decorator)       │ │
│  │  - Traces (spans)                  │ │
│  │  - Metrics (counters, histograms)  │ │
│  │  - Context propagation             │ │
│  └─────────────┬──────────────────────┘ │
└────────────────┼────────────────────────┘
                 │
       ┌─────────┴─────────┐
       v                   v
┌──────────────┐    ┌──────────────┐
│ Claude API   │    │ Gemini API   │
└──────────────┘    └──────────────┘
       │                   │
       v                   v
┌──────────────────────────────────┐
│       Telemetry Backends         │
│  - Prometheus (metrics)          │
│  - Jaeger (traces)               │
│  - Stdout (development)          │
│  - OTLP (any backend)            │
└──────────────────────────────────┘
```

## Configuration

### Environment Variables

```bash
# Enable/disable OpenTelemetry
export OTEL_ENABLED=true

# Tracing
export OTEL_TRACES_ENABLED=true
export OTEL_TRACES_EXPORTER=stdout  # stdout, otlp, none
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317

# Metrics
export OTEL_METRICS_ENABLED=true
export OTEL_METRICS_EXPORTER=prometheus  # prometheus, none
export OTEL_METRICS_PORT=9090
```

### Exporters

**Stdout (Development)**
```bash
export OTEL_TRACES_EXPORTER=stdout
export OTEL_METRICS_EXPORTER=none
make run
```

**Prometheus (Production)**
```bash
export OTEL_METRICS_EXPORTER=prometheus
export OTEL_METRICS_PORT=9090
make run

# Metrics available at http://localhost:9090/metrics
```

**OTLP (Jaeger, DataDog, etc.)**
```bash
export OTEL_TRACES_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
make run
```

## Metrics

### LLM Call Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `llm.calls` | Counter | Number of API calls | provider, model |
| `llm.errors` | Counter | Number of failed calls | provider, model |
| `llm.duration` | Histogram | API latency in ms | provider, model |
| `llm.tokens` | Counter | Token usage | provider, model, token_type |

### Example Prometheus Queries

**Request rate:**
```promql
rate(llm_calls_total[5m])
```

**Error rate:**
```promql
rate(llm_errors_total[5m]) / rate(llm_calls_total[5m])
```

**P95 latency:**
```promql
histogram_quantile(0.95, rate(llm_duration_bucket[5m]))
```

**Token usage per minute:**
```promql
rate(llm_tokens_total{token_type="input"}[1m]) * 60
```

**Cost estimation (Gemini):**
```promql
(
  rate(llm_tokens_total{provider="gemini",token_type="input"}[1h]) * 60 * 60 * 0.075 / 1000000
) + (
  rate(llm_tokens_total{provider="gemini",token_type="output"}[1h]) * 60 * 60 * 0.30 / 1000000
)
```

## Traces

### Span Structure

Each LLM call creates a span with attributes:

```
llm.chat
├── llm.provider: "gemini"
├── llm.model: "gemini-2.0-flash"
├── llm.messages.count: 3
├── llm.tools.count: 2
├── llm.tokens.input: 156
├── llm.tokens.output: 89
├── llm.tokens.total: 245
├── llm.tool_calls.count: 1
└── llm.duration_ms: 523
```

### Distributed Tracing

OpenTelemetry automatically propagates context across services:

```
User Request
  └── Agent.Run
      └── LLM.Chat (span)
          ├── tool_execution_1 (potential future span)
          └── tool_execution_2 (potential future span)
```

## Integration Examples

### Local Development (Stdout)

```bash
export OTEL_TRACES_EXPORTER=stdout
make run
```

Output:
```json
{
  "Name": "llm.chat",
  "SpanContext": {
    "TraceID": "4bf92f3577b34da6a3ce929d0e0e4736",
    "SpanID": "00f067aa0ba902b7"
  },
  "Attributes": [
    {"Key": "llm.provider", "Value": {"Type":"STRING","Value":"gemini"}},
    {"Key": "llm.model", "Value": {"Type":"STRING","Value":"gemini-2.0-flash"}},
    {"Key": "llm.tokens.input", "Value": {"Type":"INT64","Value":156}},
    {"Key": "llm.tokens.output", "Value": {"Type":"INT64","Value":89}}
  ]
}
```

### Prometheus + Grafana

**1. Start Prometheus:**
```bash
# prometheus.yml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'joe'
    static_configs:
      - targets: ['localhost:9090']

# Run
prometheus --config.file=prometheus.yml
```

**2. Start Joe:**
```bash
export OTEL_METRICS_EXPORTER=prometheus
export OTEL_METRICS_PORT=9090
make run
```

**3. Query metrics:**
```bash
curl http://localhost:9090/metrics
```

### Jaeger (Distributed Tracing)

**1. Start Jaeger:**
```bash
docker run -d --name jaeger \
  -p 4317:4317 \
  -p 16686:16686 \
  jaegertracing/all-in-one:latest
```

**2. Configure Joe:**
```bash
export OTEL_TRACES_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
make run
```

**3. View traces:**
Open http://localhost:16686

### DataDog

```bash
export OTEL_TRACES_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_ENDPOINT=<datadog-agent>:4317
export DD_ENV=production
export DD_SERVICE=joe
export DD_VERSION=0.1.0
make run
```

## Package Organization

```
internal/
├── observability/              # NEW: Observability package
│   ├── otel.go                # OpenTelemetry setup
│   ├── llm_middleware.go      # LLM instrumentation
│   └── metrics.go             # Metric definitions (future)
│
├── llm/                       # LLM clients (no instrumentation)
│   ├── claude/
│   ├── gemini/
│   └── instrumented.go        # DEPRECATED: Use observability package
```

**Design Principles:**
- ✅ Instrumentation separate from business logic
- ✅ Decorator pattern for composability
- ✅ Standard OpenTelemetry conventions
- ✅ Vendor-neutral exporters

## Migration from Simple Logging

**Old approach** (internal/llm/instrumented.go):
```go
// Simple slog-based logging
instrumented := llm.NewInstrumentedAdapter(adapter, logger)
```

**New approach** (internal/observability):
```go
// Full OpenTelemetry with traces, metrics, context propagation
middleware, _ := observability.NewLLMMiddleware(adapter, "gemini", "gemini-2.0-flash")
```

**Why migrate?**
- OpenTelemetry is the industry standard
- Richer data: traces + metrics + logs
- Better tooling ecosystem
- Distributed tracing support
- Vendor-neutral

## Cost Tracking

Use token metrics to estimate API costs:

**Gemini Pricing:**
- Input: $0.075 per 1M tokens
- Output: $0.30 per 1M tokens

**Prometheus query for hourly cost:**
```promql
(
  increase(llm_tokens_total{provider="gemini",token_type="input"}[1h]) * 0.075 / 1000000
) + (
  increase(llm_tokens_total{provider="gemini",token_type="output"}[1h]) * 0.30 / 1000000
)
```

**Create alerts:**
```yaml
groups:
  - name: joe_cost
    rules:
      - alert: HighLLMCost
        expr: |
          (
            rate(llm_tokens_total{token_type="input"}[1h]) * 3600 * 0.075 / 1000000
          ) + (
            rate(llm_tokens_total{token_type="output"}[1h]) * 3600 * 0.30 / 1000000
          ) > 10
        annotations:
          summary: "LLM costs exceeding $10/hour"
```

## Best Practices

1. **Always propagate context** - Pass `ctx` through function calls
2. **Add custom attributes** - Enrich spans with relevant data
3. **Use semantic conventions** - Follow OpenTelemetry naming
4. **Sample in production** - Use `AlwaysSample()` for development, configure sampling for production
5. **Monitor costs** - Set up alerts on token usage
6. **Export to multiple backends** - Use different backends for different purposes

## Testing

Run with instrumentation enabled:
```bash
make test