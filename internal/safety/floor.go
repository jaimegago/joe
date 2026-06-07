package safety

import "errors"

// FloorReason identifies WHY the write floor is up. It rides out of the single
// write-denial branch as data, so enforcement stays singular while presentation
// differs per reason (D-0018 point 1).
type FloorReason string

const (
	// FloorReasonNone is the reason of a floor that is down.
	FloorReasonNone FloorReason = ""
	// FloorReasonObservation — the floor is up because Joe booted into
	// observation mode (the JOE_MODE=observation env var). A calm, intended
	// read-only resting posture; NOT panic/safe mode.
	FloorReasonObservation FloorReason = "observation"
	// FloorReasonSafeMode — the floor is up because a sticky panic state was
	// present at boot. Recovery requires clearing the panic state and a restart.
	FloorReasonSafeMode FloorReason = "safe_mode"
)

// WriteFloor is the boot-resolved, runtime-immutable write floor (D-0018). It is
// a read-only value: once resolved it exposes ONLY Up()/Reason(). There is no
// method, package function, or setter anywhere in the binary that lowers a
// resolved floor — recovery is restart, never a live down-transition. This
// structural property (the lowering operation not existing) is the immutability
// guarantee, not a permission guard.
type WriteFloor struct {
	up     bool
	reason FloorReason
}

// Up reports whether the floor denies managed-system mutations.
func (f WriteFloor) Up() bool { return f.up }

// Reason reports why the floor is up (FloorReasonNone when down).
func (f WriteFloor) Reason() FloorReason { return f.reason }

// ResolveWriteFloor computes the floor from its two boot inputs. It is PURE — it
// reads no global, no disk, and stores nothing; boot calls it exactly once and
// threads the returned value. Within-floor precedence (D-0018 points 5 & 8): a
// present panic state wins over the observation env var ("panic is sticky and
// wins over the env var"), so a panicked Joe never resolves to the calmer
// observation reason.
func ResolveWriteFloor(panicStatePresent, observationEnvSet bool) WriteFloor {
	switch {
	case panicStatePresent:
		return WriteFloor{up: true, reason: FloorReasonSafeMode}
	case observationEnvSet:
		return WriteFloor{up: true, reason: FloorReasonObservation}
	default:
		return WriteFloor{}
	}
}

// ErrWriteFloor is the floor identity sentinel. The reason-carrying
// WriteFloorError satisfies errors.Is against it, so the api write-failure
// classifier and the executor/classifier tests keep matching after the former
// plain safe-mode sentinel was subsumed into this single reason-carrying error.
var ErrWriteFloor = errors.New(
	"write floor active: Joe is read-only and cannot mutate the managed system",
)

// WriteFloorError is the single error the tool executor returns when the floor
// denies a Mutate (D-0018 point 1). It carries the reason as data;
// errors.Is(err, ErrWriteFloor) is true for every reason, while errors.As lets
// the api layer read the reason to present observation and safe_mode distinctly.
type WriteFloorError struct {
	Reason FloorReason
}

func (e *WriteFloorError) Error() string {
	switch e.Reason {
	case FloorReasonObservation:
		return "observation mode: Joe is read-only by configuration and will not mutate the managed system"
	case FloorReasonSafeMode:
		return "safe mode active: only read-only operations are allowed until the panic state is cleared and Joe is restarted"
	default:
		return ErrWriteFloor.Error()
	}
}

// Is makes every WriteFloorError match the floor identity sentinel, preserving
// the pre-existing errors.Is dependents (the classifier and the two tests).
func (e *WriteFloorError) Is(target error) bool {
	return target == ErrWriteFloor
}
