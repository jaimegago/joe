package autorun

import "errors"

var (
	// ErrRunLive is returned by Trigger when a run on the same scope is live.
	// The trigger is refused, not queued, and the refusal is what the
	// activator sees.
	ErrRunLive = errors.New("autorun: a run on this scope is live")
	// ErrEmptyScope is returned when a selection names nothing, or resolves to
	// no component. A run over nothing is not a run.
	ErrEmptyScope = errors.New("autorun: scope resolves to no component")
	// ErrUnknownComponent is returned when a selection names a component the
	// registry does not hold. A scope is a selection of what exists.
	ErrUnknownComponent = errors.New("autorun: selection names an unknown component")
	// ErrZonesUnavailable is returned when a selection names zones and no zone
	// repository is wired (RBAC not configured).
	ErrZonesUnavailable = errors.New("autorun: zones cannot be resolved without RBAC")
	// ErrNotFound is returned by the repository when no row matches.
	ErrNotFound = errors.New("autorun: not found")
	// ErrNotStarted is returned by Trigger before Start has given the service
	// a base context to run under.
	ErrNotStarted = errors.New("autorun: service not started")
)
