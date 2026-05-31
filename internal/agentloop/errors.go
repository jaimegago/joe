package agentloop

import "errors"

// Terminal-error sentinels for the agentic loop.
//
// These are wrapped into the concrete error values returned by Agent.Run
// so downstream code (the streaming task path's status classifier, the
// non-streaming /tasks handler, future operator dashboards) can branch
// via errors.Is without depending on the message text. The pre-G3
// classifier used a 15-character substring match; the typed sentinels
// retire that.
//
// One sentinel has a return site today (ErrMaxIterations, returned from
// Agent.Run when the iteration cap is reached). The other is declared
// for the G3 enforcement phase and has no return site yet — the runaway
// gate that will return it lives in a later change.
var (
	// ErrMaxIterations is wrapped by the error Agent.Run returns when it
	// exits without a final response because the iteration cap (see
	// SetMaxIterations / DefaultMaxIterations) was hit. The wrapping
	// fmt.Errorf preserves the existing descriptive text so log readers
	// see the same prefix; callers use errors.Is(err, ErrMaxIterations).
	ErrMaxIterations = errors.New("agentloop: max iterations reached")

	// ErrSessionTokenCeiling is reserved for a later phase: the session
	// token ceiling check inserted just after AddTokenUsage in Agent.Run.
	// No code path returns this sentinel yet. Declaring it now lets the
	// downstream classifier add a single errors.Is case when the gate
	// lands, without re-architecting the classifier.
	ErrSessionTokenCeiling = errors.New("agentloop: session token ceiling reached")
)
