package client

import "time"

const (
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 30 * time.Second

	// API endpoints.
	apiStatusPath = "/api/v1/status"
)
