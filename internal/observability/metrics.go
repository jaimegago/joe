package observability

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Metrics provides injected OpenTelemetry metric emitters.
type Metrics struct {
	meter  metric.Meter
	tracer trace.Tracer

	toolOnce sync.Once
	tool     toolMetrics

	adapterOnce sync.Once
	adapter     adapterMetrics

	graphOnce sync.Once
	graph     graphMetrics

	dbOnce sync.Once
	db     dbMetrics

	coreAgentOnce sync.Once
	coreAgent     coreAgentMetrics

	agentOnce sync.Once
	agent     agentMetrics

	sessionOnce         sync.Once
	session             sessionMetrics
	sessionActiveCount  atomic.Int64
	sessionRegistration metric.Registration

	httpOnce sync.Once
	http     httpMetrics
}

// NewMetrics returns an initialized metrics recorder using the global providers.
func NewMetrics() *Metrics {
	return &Metrics{
		meter:  otel.Meter(metricsInstrumentationName),
		tracer: otel.Tracer(httpTracerName),
	}
}

// EnsureMetrics guarantees a non-nil Metrics instance.
func EnsureMetrics(metrics *Metrics) *Metrics {
	if metrics == nil {
		return NewMetrics()
	}
	return metrics
}

func logMetricInitError(name string, err error) {
	if err != nil {
		slog.Warn("failed to create metric", "name", name, "error", err)
	}
}

func safeAddCounter(ctx context.Context, counter metric.Int64Counter, value int64, attrs ...attribute.KeyValue) {
	if counter != nil {
		counter.Add(ctx, value, metric.WithAttributes(attrs...))
	}
}

func safeAddUpDownCounter(ctx context.Context, counter metric.Int64UpDownCounter, value int64, attrs ...attribute.KeyValue) {
	if counter != nil {
		counter.Add(ctx, value, metric.WithAttributes(attrs...))
	}
}

func safeRecordHistogram(ctx context.Context, hist metric.Float64Histogram, value float64, attrs ...attribute.KeyValue) {
	if hist != nil {
		hist.Record(ctx, value, metric.WithAttributes(attrs...))
	}
}

type toolMetrics struct {
	executionCounter  metric.Int64Counter
	errorCounter      metric.Int64Counter
	durationHist      metric.Float64Histogram
	batchCounter      metric.Int64Counter
	batchErrorCounter metric.Int64Counter
	batchDurationHist metric.Float64Histogram
}

func (m *Metrics) getToolMetrics() *toolMetrics {
	m.toolOnce.Do(func() {
		execCounter, err := m.meter.Int64Counter(
			MetricToolsExecutions,
			metric.WithDescription("Tool executions"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricToolsExecutions, err)

		errorCounter, err := m.meter.Int64Counter(
			MetricToolsErrors,
			metric.WithDescription("Tool execution errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricToolsErrors, err)

		durationHist, err := m.meter.Float64Histogram(
			MetricToolsDuration,
			metric.WithDescription("Tool execution duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricToolsDuration, err)

		batchCounter, err := m.meter.Int64Counter(
			MetricToolsBatchExec,
			metric.WithDescription("Tool batch executions"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricToolsBatchExec, err)

		batchErrorCounter, err := m.meter.Int64Counter(
			MetricToolsBatchErrors,
			metric.WithDescription("Tool batch errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricToolsBatchErrors, err)

		batchDurationHist, err := m.meter.Float64Histogram(
			MetricToolsBatchDuration,
			metric.WithDescription("Tool batch duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricToolsBatchDuration, err)

		m.tool = toolMetrics{
			executionCounter:  execCounter,
			errorCounter:      errorCounter,
			durationHist:      durationHist,
			batchCounter:      batchCounter,
			batchErrorCounter: batchErrorCounter,
			batchDurationHist: batchDurationHist,
		}
	})

	return &m.tool
}

func (m *Metrics) RecordToolExecution(ctx context.Context, name string, duration time.Duration, err error) {
	metrics := m.getToolMetrics()
	attrs := []attribute.KeyValue{attribute.String(AttrToolName, name)}
	safeAddCounter(ctx, metrics.executionCounter, 1, attrs...)
	safeRecordHistogram(ctx, metrics.durationHist, float64(duration.Milliseconds()), attrs...)
	if err != nil {
		safeAddCounter(ctx, metrics.errorCounter, 1, attrs...)
	}
}

func (m *Metrics) RecordToolBatch(ctx context.Context, count int, errorCount int, duration time.Duration) {
	metrics := m.getToolMetrics()
	attrs := []attribute.KeyValue{attribute.Int(AttrToolsCount, count)}
	safeAddCounter(ctx, metrics.batchCounter, 1, attrs...)
	safeRecordHistogram(ctx, metrics.batchDurationHist, float64(duration.Milliseconds()), attrs...)
	if errorCount > 0 {
		safeAddCounter(ctx, metrics.batchErrorCounter, int64(errorCount), attrs...)
	}
}

type adapterMetrics struct {
	callCounter  metric.Int64Counter
	errorCounter metric.Int64Counter
	durationHist metric.Float64Histogram
}

func (m *Metrics) getAdapterMetrics() *adapterMetrics {
	m.adapterOnce.Do(func() {
		callCounter, err := m.meter.Int64Counter(
			MetricAdaptersCalls,
			metric.WithDescription("Adapter calls"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricAdaptersCalls, err)

		errorCounter, err := m.meter.Int64Counter(
			MetricAdaptersErrors,
			metric.WithDescription("Adapter call errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricAdaptersErrors, err)

		durationHist, err := m.meter.Float64Histogram(
			MetricAdaptersDuration,
			metric.WithDescription("Adapter call duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricAdaptersDuration, err)

		m.adapter = adapterMetrics{
			callCounter:  callCounter,
			errorCounter: errorCounter,
			durationHist: durationHist,
		}
	})
	return &m.adapter
}

func (m *Metrics) RecordAdapterCall(ctx context.Context, adapterType, operation string, duration time.Duration, err error) {
	metrics := m.getAdapterMetrics()
	attrs := []attribute.KeyValue{
		attribute.String(AttrAdapterType, adapterType),
		attribute.String(AttrAdapterOp, operation),
	}
	safeAddCounter(ctx, metrics.callCounter, 1, attrs...)
	safeRecordHistogram(ctx, metrics.durationHist, float64(duration.Milliseconds()), attrs...)
	if err != nil {
		safeAddCounter(ctx, metrics.errorCounter, 1, attrs...)
	}
}

type graphMetrics struct {
	operationCounter metric.Int64Counter
	errorCounter     metric.Int64Counter
	durationHist     metric.Float64Histogram
}

func (m *Metrics) getGraphMetrics() *graphMetrics {
	m.graphOnce.Do(func() {
		operationCounter, err := m.meter.Int64Counter(
			MetricGraphOperations,
			metric.WithDescription("Graph operations"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricGraphOperations, err)

		errorCounter, err := m.meter.Int64Counter(
			MetricGraphErrors,
			metric.WithDescription("Graph operation errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricGraphErrors, err)

		durationHist, err := m.meter.Float64Histogram(
			MetricGraphDuration,
			metric.WithDescription("Graph operation duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricGraphDuration, err)

		m.graph = graphMetrics{
			operationCounter: operationCounter,
			errorCounter:     errorCounter,
			durationHist:     durationHist,
		}
	})
	return &m.graph
}

func (m *Metrics) RecordGraphOperation(ctx context.Context, operation string, duration time.Duration, err error) {
	metrics := m.getGraphMetrics()
	attrs := []attribute.KeyValue{attribute.String(AttrGraphOp, operation)}
	safeAddCounter(ctx, metrics.operationCounter, 1, attrs...)
	safeRecordHistogram(ctx, metrics.durationHist, float64(duration.Milliseconds()), attrs...)
	if err != nil {
		safeAddCounter(ctx, metrics.errorCounter, 1, attrs...)
	}
}

type dbMetrics struct {
	operationCounter metric.Int64Counter
	errorCounter     metric.Int64Counter
	durationHist     metric.Float64Histogram
}

func (m *Metrics) getDBMetrics() *dbMetrics {
	m.dbOnce.Do(func() {
		operationCounter, err := m.meter.Int64Counter(
			MetricDBOperations,
			metric.WithDescription("Database operations"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricDBOperations, err)

		errorCounter, err := m.meter.Int64Counter(
			MetricDBErrors,
			metric.WithDescription("Database operation errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricDBErrors, err)

		durationHist, err := m.meter.Float64Histogram(
			MetricDBDuration,
			metric.WithDescription("Database operation duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricDBDuration, err)

		m.db = dbMetrics{
			operationCounter: operationCounter,
			errorCounter:     errorCounter,
			durationHist:     durationHist,
		}
	})
	return &m.db
}

func (m *Metrics) RecordDBOperation(ctx context.Context, operation string, duration time.Duration, err error) {
	metrics := m.getDBMetrics()
	attrs := []attribute.KeyValue{attribute.String(AttrDBOp, operation)}
	safeAddCounter(ctx, metrics.operationCounter, 1, attrs...)
	safeRecordHistogram(ctx, metrics.durationHist, float64(duration.Milliseconds()), attrs...)
	if err != nil {
		safeAddCounter(ctx, metrics.errorCounter, 1, attrs...)
	}
}

type coreAgentMetrics struct {
	refreshCounter    metric.Int64Counter
	refreshErrors     metric.Int64Counter
	refreshDuration   metric.Float64Histogram
	discoveryCounter  metric.Int64Counter
	discoveryErrors   metric.Int64Counter
	discoveryDuration metric.Float64Histogram
}

func (m *Metrics) getCoreAgentMetrics() *coreAgentMetrics {
	m.coreAgentOnce.Do(func() {
		refreshCounter, err := m.meter.Int64Counter(
			MetricCoreRefreshRuns,
			metric.WithDescription("Refresh runs"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricCoreRefreshRuns, err)

		refreshErrors, err := m.meter.Int64Counter(
			MetricCoreRefreshErrors,
			metric.WithDescription("Refresh errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricCoreRefreshErrors, err)

		refreshDuration, err := m.meter.Float64Histogram(
			MetricCoreRefreshDuration,
			metric.WithDescription("Refresh duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricCoreRefreshDuration, err)

		discoveryCounter, err := m.meter.Int64Counter(
			MetricCoreDiscoveryInputs,
			metric.WithDescription("Discovery inputs"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricCoreDiscoveryInputs, err)

		discoveryErrors, err := m.meter.Int64Counter(
			MetricCoreDiscoveryErrors,
			metric.WithDescription("Discovery errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricCoreDiscoveryErrors, err)

		discoveryDuration, err := m.meter.Float64Histogram(
			MetricCoreDiscoveryDuration,
			metric.WithDescription("Discovery processing duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricCoreDiscoveryDuration, err)

		m.coreAgent = coreAgentMetrics{
			refreshCounter:    refreshCounter,
			refreshErrors:     refreshErrors,
			refreshDuration:   refreshDuration,
			discoveryCounter:  discoveryCounter,
			discoveryErrors:   discoveryErrors,
			discoveryDuration: discoveryDuration,
		}
	})
	return &m.coreAgent
}

func (m *Metrics) RecordRefreshCycle(ctx context.Context, duration time.Duration, err error) {
	metrics := m.getCoreAgentMetrics()
	safeAddCounter(ctx, metrics.refreshCounter, 1)
	safeRecordHistogram(ctx, metrics.refreshDuration, float64(duration.Milliseconds()))
	if err != nil {
		safeAddCounter(ctx, metrics.refreshErrors, 1)
	}
}

func (m *Metrics) RecordDiscoveryInput(ctx context.Context, duration time.Duration, err error) {
	metrics := m.getCoreAgentMetrics()
	safeAddCounter(ctx, metrics.discoveryCounter, 1)
	safeRecordHistogram(ctx, metrics.discoveryDuration, float64(duration.Milliseconds()))
	if err != nil {
		safeAddCounter(ctx, metrics.discoveryErrors, 1)
	}
}

type agentMetrics struct {
	runCounter   metric.Int64Counter
	errorCounter metric.Int64Counter
	runDuration  metric.Float64Histogram
}

func (m *Metrics) getAgentMetrics() *agentMetrics {
	m.agentOnce.Do(func() {
		runCounter, err := m.meter.Int64Counter(
			MetricUserAgentRuns,
			metric.WithDescription("User agent runs"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricUserAgentRuns, err)

		errorCounter, err := m.meter.Int64Counter(
			MetricUserAgentErrors,
			metric.WithDescription("User agent errors"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricUserAgentErrors, err)

		runDuration, err := m.meter.Float64Histogram(
			MetricUserAgentDuration,
			metric.WithDescription("User agent run duration"),
			metric.WithUnit(metricUnitMS),
		)
		logMetricInitError(MetricUserAgentDuration, err)

		m.agent = agentMetrics{
			runCounter:   runCounter,
			errorCounter: errorCounter,
			runDuration:  runDuration,
		}
	})
	return &m.agent
}

func (m *Metrics) RecordAgentRun(ctx context.Context, duration time.Duration, err error) {
	metrics := m.getAgentMetrics()
	safeAddCounter(ctx, metrics.runCounter, 1)
	safeRecordHistogram(ctx, metrics.runDuration, float64(duration.Milliseconds()))
	if err != nil {
		safeAddCounter(ctx, metrics.errorCounter, 1)
	}
}

type sessionMetrics struct {
	messageCounter metric.Int64Counter
	tokenCounter   metric.Int64Counter
	activeGauge    metric.Int64ObservableGauge
}

func (m *Metrics) getSessionMetrics() *sessionMetrics {
	m.sessionOnce.Do(func() {
		messageCounter, err := m.meter.Int64Counter(
			MetricSessionMessages,
			metric.WithDescription("Session messages"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricSessionMessages, err)

		tokenCounter, err := m.meter.Int64Counter(
			MetricSessionTokens,
			metric.WithDescription("Session tokens"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricSessionTokens, err)

		activeGauge, err := m.meter.Int64ObservableGauge(
			MetricSessionActive,
			metric.WithDescription("Active sessions"),
			metric.WithUnit(metricUnitCount),
		)
		logMetricInitError(MetricSessionActive, err)

		if err == nil {
			registration, regErr := m.meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
				observer.ObserveInt64(activeGauge, m.sessionActiveCount.Load())
				return nil
			}, activeGauge)
			if regErr != nil {
				slog.Warn("failed to register session gauge", "error", regErr)
			} else {
				m.sessionRegistration = registration
			}
		}

		m.session = sessionMetrics{
			messageCounter: messageCounter,
			tokenCounter:   tokenCounter,
			activeGauge:    activeGauge,
		}
	})
	return &m.session
}

func (m *Metrics) RecordSessionStart() {
	m.sessionActiveCount.Add(1)
	m.getSessionMetrics()
}

func (m *Metrics) RecordSessionEnd() {
	m.sessionActiveCount.Add(-1)
}

func (m *Metrics) RecordSessionMessage(ctx context.Context, role string) {
	metrics := m.getSessionMetrics()
	attrs := []attribute.KeyValue{attribute.String(AttrRole, role)}
	safeAddCounter(ctx, metrics.messageCounter, 1, attrs...)
}

func (m *Metrics) RecordSessionTokens(ctx context.Context, total int) {
	metrics := m.getSessionMetrics()
	safeAddCounter(ctx, metrics.tokenCounter, int64(total))
}

// GraphMetricsSummary provides counts for business metrics.
type GraphMetricsSummary struct {
	NodeCount   int
	EdgeCount   int
	NodesByType map[string]int
}

// BuildInfo carries the build-identity labels for the joe_build_info gauge.
// One label-set per running binary; the ui_digest label is what makes a stale
// embed visible across replicas.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
	UIDigest  string
}

// RegisterBuildInfo registers the joe_build_info gauge: a constant value of 1
// whose labels (version, commit, build_time, ui_digest) carry the build
// identity. It is registered once at metrics-setup time beside the business
// gauges, never in a request handler's business path. The returned func
// unregisters the observable callback.
func (m *Metrics) RegisterBuildInfo(info BuildInfo) (func() error, error) {
	// No unit is set deliberately: with the Prometheus exporter, a "1" unit
	// renders a "_ratio" suffix (joe_build_info_ratio). Omitting the unit keeps
	// the conventional name joe_build_info exactly.
	gauge, err := m.meter.Int64ObservableGauge(
		MetricBuildInfo,
		metric.WithDescription("Build identity of the running joe binary; constant 1, identity carried in labels"),
	)
	if err != nil {
		return nil, err
	}

	registration, err := m.meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		observer.ObserveInt64(gauge, 1, metric.WithAttributes(
			attribute.String(AttrBuildVersion, info.Version),
			attribute.String(AttrBuildCommit, info.Commit),
			attribute.String(AttrBuildTime, info.BuildTime),
			attribute.String(AttrBuildUIDigest, info.UIDigest),
		))
		return nil
	}, gauge)
	if err != nil {
		return nil, err
	}

	return registration.Unregister, nil
}

// BusinessMetricsProvider supplies data for business metrics gauges.
type BusinessMetricsProvider struct {
	ComponentsByType func(ctx context.Context) (map[string]int, error)
	GraphSummary     func(ctx context.Context) (GraphMetricsSummary, error)
	AdapterCount     func() int
}

func (m *Metrics) RegisterBusinessMetrics(provider BusinessMetricsProvider) (func() error, error) {
	sourcesGauge, err := m.meter.Int64ObservableGauge(
		MetricSourcesTotal,
		metric.WithDescription("Components by type"),
		metric.WithUnit(metricUnitCount),
	)
	if err != nil {
		return nil, err
	}

	graphNodesGauge, err := m.meter.Int64ObservableGauge(
		MetricGraphNodesTotal,
		metric.WithDescription("Graph nodes by type"),
		metric.WithUnit(metricUnitCount),
	)
	if err != nil {
		return nil, err
	}

	graphEdgesGauge, err := m.meter.Int64ObservableGauge(
		MetricGraphEdgesTotal,
		metric.WithDescription("Graph edges"),
		metric.WithUnit(metricUnitCount),
	)
	if err != nil {
		return nil, err
	}

	adaptersGauge, err := m.meter.Int64ObservableGauge(
		MetricAdaptersConnected,
		metric.WithDescription("Connected adapters"),
		metric.WithUnit(metricUnitCount),
	)
	if err != nil {
		return nil, err
	}

	registration, err := m.meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		if provider.ComponentsByType != nil {
			counts, srcErr := provider.ComponentsByType(ctx)
			if srcErr == nil {
				for sourceType, count := range counts {
					observer.ObserveInt64(sourcesGauge, int64(count), metric.WithAttributes(attribute.String(AttrSourceType, sourceType)))
				}
			}
		}

		if provider.GraphSummary != nil {
			summary, graphErr := provider.GraphSummary(ctx)
			if graphErr == nil {
				observer.ObserveInt64(graphNodesGauge, int64(summary.NodeCount), metric.WithAttributes(attribute.String(AttrSourceType, "all")))
				for nodeType, count := range summary.NodesByType {
					observer.ObserveInt64(graphNodesGauge, int64(count), metric.WithAttributes(attribute.String(AttrSourceType, nodeType)))
				}
				observer.ObserveInt64(graphEdgesGauge, int64(summary.EdgeCount))
			}
		}

		if provider.AdapterCount != nil {
			observer.ObserveInt64(adaptersGauge, int64(provider.AdapterCount()))
		}
		return nil
	}, sourcesGauge, graphNodesGauge, graphEdgesGauge, adaptersGauge)

	if err != nil {
		return nil, err
	}

	return registration.Unregister, nil
}
