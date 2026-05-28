package observability

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

type httpMetrics struct {
	requestCounter metric.Int64Counter
	errorCounter   metric.Int64Counter
	durationHist   metric.Float64Histogram
	inFlight       metric.Int64UpDownCounter
}

func (m *Metrics) getHTTPMetrics() *httpMetrics {
	m.httpOnce.Do(func() {
		requestCounter, err := m.meter.Int64Counter(
			MetricHTTPRequests,
			metric.WithDescription("HTTP server requests"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricHTTPRequests, err)

		errorCounter, err := m.meter.Int64Counter(
			MetricHTTPErrors,
			metric.WithDescription("HTTP server errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricHTTPErrors, err)

		durationHist, err := m.meter.Float64Histogram(
			MetricHTTPDuration,
			metric.WithDescription("HTTP server duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricHTTPDuration, err)

		inFlight, err := m.meter.Int64UpDownCounter(
			MetricHTTPInFlight,
			metric.WithDescription("HTTP requests in flight"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricHTTPInFlight, err)

		m.http = httpMetrics{
			requestCounter: requestCounter,
			errorCounter:   errorCounter,
			durationHist:   durationHist,
			inFlight:       inFlight,
		}
	})
	return &m.http
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Flush passes through to the underlying ResponseWriter when it supports
// flushing. Without this, Server-Sent Events (e.g. the streaming task
// endpoint) would buffer behind the metrics middleware because the embedded
// http.ResponseWriter interface does not promote a Flush method.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func HTTPMetricsMiddleware(next http.Handler, metrics *Metrics) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
	}

	metrics = EnsureMetrics(metrics)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpMetrics := metrics.getHTTPMetrics()
		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}

		attrs := []attribute.KeyValue{
			attribute.String(AttrHTTPMethod, r.Method),
			attribute.String(AttrHTTPRoute, route),
		}

		ctx, span := metrics.tracer.Start(r.Context(), "http.request",
			trace.WithAttributes(
				semconv.HTTPMethodKey.String(r.Method),
				semconv.HTTPRouteKey.String(route),
			),
		)
		defer span.End()

		safeAddUpDownCounter(ctx, httpMetrics.inFlight, 1, attrs...)
		defer safeAddUpDownCounter(ctx, httpMetrics.inFlight, -1, attrs...)

		recorder := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(recorder, r.WithContext(ctx))
		duration := time.Since(start)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}

		attrs = append(attrs, attribute.Int(AttrHTTPStatus, status))
		safeAddCounter(ctx, httpMetrics.requestCounter, 1, attrs...)
		safeRecordHistogram(ctx, httpMetrics.durationHist, float64(duration.Milliseconds()), attrs...)
		if status >= http.StatusInternalServerError {
			safeAddCounter(ctx, httpMetrics.errorCounter, 1, attrs...)
			span.SetStatus(codes.Error, http.StatusText(status))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		span.SetAttributes(semconv.HTTPStatusCodeKey.Int(status))

		// Structured access log — visible in joecored output for every request.
		logFn := slog.Debug
		if status >= http.StatusInternalServerError {
			logFn = slog.Error
		} else if status >= http.StatusBadRequest {
			logFn = slog.Warn
		}
		logFn("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
		)
	})
}
