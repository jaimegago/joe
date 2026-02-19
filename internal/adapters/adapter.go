package adapters

import (
	"context"

	"github.com/jaimegago/joe/internal/store"
)

// Adapter is the common interface for infrastructure adapters
type Adapter interface {
	// Connect establishes a connection to the source.
	// The context should carry deadlines, cancellation, and tracing from the caller.
	Connect(ctx context.Context, source store.Source) error

	// Disconnect closes the connection
	Disconnect() error

	// Status returns the current connection status
	Status() Status
}

// Status represents the connection status
type Status struct {
	Connected bool
	Message   string
}
