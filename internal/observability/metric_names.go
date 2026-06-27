package observability

const (
	metricsInstrumentationName = "joe/metrics"
	httpTracerName             = "joe/http"

	metricUnitCount = "1"
	metricUnitMS    = "ms"
)

const (
	MetricToolsExecutions       = "tools.executions"
	MetricToolsErrors           = "tools.errors"
	MetricToolsDuration         = "tools.duration"
	MetricToolsBatchExec        = "tools.batch.executions"
	MetricToolsBatchErrors      = "tools.batch.errors"
	MetricToolsBatchDuration    = "tools.batch.duration"
	MetricAdaptersCalls         = "adapters.calls"
	MetricAdaptersErrors        = "adapters.errors"
	MetricAdaptersDuration      = "adapters.duration"
	MetricGraphOperations       = "graph.operations"
	MetricGraphErrors           = "graph.errors"
	MetricGraphDuration         = "graph.duration"
	MetricDBOperations          = "db.operations"
	MetricDBErrors              = "db.errors"
	MetricDBDuration            = "db.duration"
	MetricCoreRefreshRuns       = "coreagent.refresh.runs"
	MetricCoreRefreshErrors     = "coreagent.refresh.errors"
	MetricCoreRefreshDuration   = "coreagent.refresh.duration"
	MetricCoreDiscoveryInputs   = "coreagent.discovery.inputs"
	MetricCoreDiscoveryErrors   = "coreagent.discovery.errors"
	MetricCoreDiscoveryDuration = "coreagent.discovery.duration"
	MetricUserAgentRuns         = "useragent.runs"
	MetricUserAgentErrors       = "useragent.errors"
	MetricUserAgentDuration     = "useragent.duration"
	MetricSessionMessages       = "session.messages"
	MetricSessionTokens         = "session.tokens"
	MetricSessionActive         = "session.active"
	MetricSourcesTotal          = "components.total"
	MetricGraphNodesTotal       = "graph.nodes.total"
	MetricGraphEdgesTotal       = "graph.edges.total"
	MetricAdaptersConnected     = "adapters.connected"
	MetricHTTPRequests          = "http.server.requests"
	MetricHTTPErrors            = "http.server.errors"
	MetricHTTPDuration          = "http.server.duration"
	MetricHTTPInFlight          = "http.server.in_flight"
	// MetricBuildInfo is the build-identity gauge. The dotted name renders as
	// joe_build_info under the Prometheus exporter (dots become underscores);
	// the leading "joe" prefix follows the conventional <namespace>_build_info
	// pattern. Value is a constant 1; the build identity is carried in labels.
	MetricBuildInfo = "joe.build.info"
)

const (
	AttrToolName    = "tool.name"
	AttrToolsCount  = "tools.count"
	AttrAdapterType = "adapter.type"
	AttrAdapterOp   = "adapter.operation"
	AttrGraphOp     = "graph.operation"
	AttrDBOp        = "db.operation"
	AttrRole        = "role"
	AttrHTTPMethod  = "http.method"
	AttrHTTPRoute   = "http.route"
	AttrHTTPStatus  = "http.status_code"
	AttrSourceType  = "type"

	// Labels for the joe_build_info gauge.
	AttrBuildVersion  = "version"
	AttrBuildCommit   = "commit"
	AttrBuildTime     = "build_time"
	AttrBuildUIDigest = "ui_digest"
)
