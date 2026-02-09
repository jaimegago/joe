package client

import "time"

const (
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 30 * time.Second

	// API endpoints.
	apiStatusPath       = "/api/v1/status"
	apiGraphQueryPath   = "/api/v1/graph/query"
	apiGraphRelatedPath = "/api/v1/graph/related"
	apiGraphSummaryPath = "/api/v1/graph/summary"
	apiSourcesPath      = "/api/v1/sources"
	apiK8sBasePath      = "/api/v1/k8s"
)
