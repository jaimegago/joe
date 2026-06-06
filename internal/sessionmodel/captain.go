package sessionmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/seams"
)

// CaptainService implements the §B captain attach + transfer state machine
// on top of sessionmodel.Repository and runmodel.Repository. The Repository
// owns raw row I/O; this service composes them into the business rules
// from PHASE-0-SESSION-MODEL.md §B and the R-CAP{1,2,3} attach rules.
//
// Phase 1 scope: human captains only. Joe-type captain is a Change 12
// inert seam (compile-time const false in internal/seams/).
//
// R-OVR (B-OVR force-yield) marker: when the current captain is type
// joe and an RBAC-authorized human requests command, the §B state
// machine is structurally force-overridden to immediate
// transfer_confirmed (no decline branch). Change 12 wires this; the
// TODO below marks the call-site where Change 12 will land the
// override branch.
//
// Reachability source (§6-D NET-NEW): IsCaptainReachable on the
// Repository, backed by session_captains.last_seen_at. See migration
// 013 and the captain heartbeat endpoint.
type CaptainService struct {
	repo                  Repository
	runRepo               runmodel.Repository
	reachableThresholdSec int
}

// NewCaptainService constructs a CaptainService. thresholdSeconds is the
// §6-D reachability window — captains heard from more recently than this
// are reachable. Production callers in cmd/joe/server.go pass a
// config-driven value; tests pass small values to exercise the
// unreachable branch.
func NewCaptainService(repo Repository, runRepo runmodel.Repository, thresholdSeconds int) *CaptainService {
	if thresholdSeconds <= 0 {
		thresholdSeconds = 90 // default 90s — small enough to feel a missing heartbeat, generous enough to absorb network jitter.
	}
	return &CaptainService{repo: repo, runRepo: runRepo, reachableThresholdSec: thresholdSeconds}
}

// ReachableThresholdSeconds returns the configured reachability window.
// Test helpers use this to drive the test clock past the threshold.
func (s *CaptainService) ReachableThresholdSeconds() int {
	return s.reachableThresholdSec
}

// Errors specific to the captain state machine.
var (
	ErrAttachToNonIncident     = errors.New("sessionmodel: attach only meaningful in incident regime per §B4")
	ErrAlreadyCaptainAttached  = errors.New("sessionmodel: session already has an active captain")
	ErrTransferAlreadyInFlight = errors.New("sessionmodel: transfer already in flight on this session")
	ErrNoTransferInFlight      = errors.New("sessionmodel: no transfer in flight on this session")
	ErrOnlyHumansInPhase1      = errors.New("sessionmodel: only captain_type=human is implemented in Phase 1; joe is a Change 12 inert seam")
	// ErrNotSolicitedIncoming is returned when a principal that is not the
	// solicited incoming principal named in the in-flight record tries to
	// confirm a transfer. Confirm is reserved to that one principal — the
	// outgoing captain cannot confirm in their place. Sibling of
	// ErrCaptainPrincipalMismatch; the HTTP layer maps it to 403.
	ErrNotSolicitedIncoming = errors.New("sessionmodel: only the solicited incoming principal may confirm this transfer")
	// ErrNotTransferParty is returned when a principal that is neither party
	// to the handshake (neither the soliciting/outgoing captain nor the
	// solicited incoming principal) tries to cancel a transfer. The HTTP
	// layer maps it to 403.
	ErrNotTransferParty = errors.New("sessionmodel: only a party to the transfer may cancel it")
)

// AttachResult describes the outcome of an Attach call. R-CAP{1,2,3}
// distinguishes whether the incoming attach becomes captain (R-CAP1
// declare-and-captain or R-CAP2 first-authorized-human-on-pending) or
// is purely informational (an observer in non-incident regime, or a
// non-captain attach for an existing-captain session).
type AttachResult struct {
	BecameCaptain bool
	CaptainID     string // populated when BecameCaptain
}

// Attach handles R-CAP1 / R-CAP2 / R-CAP3 / §B4. Phase 1 only supports
// the human-attach paths; Joe-attach is a Change 12 seam.
//
// Rules:
//   - Non-incident regime: attach is informational (no captain semantics
//     outside incident regime, §B4). Returns BecameCaptain=false.
//   - Incident regime, no active captain (pending_captain state, R-CAP2):
//     the attaching principal becomes captain. Authorization checks
//     (R-CAP3 "RBAC-authorized human") are the caller's responsibility
//     — this method assumes the caller has authorized the attach.
//   - Incident regime, captain already attached: attach is informational
//     (additional human is an observer, §A3). Returns
//     BecameCaptain=false.
//
// R-CAP1 (declare-and-captain) is NOT this method — DeclareIncidentRegime
// in regime_transitions.go is the atomic declare+captain path. This
// method is for subsequent attaches.
func (s *CaptainService) Attach(ctx context.Context, sessionID, principal string, captainType CaptainType) (*AttachResult, error) {
	if captainType == "" {
		captainType = CaptainTypeHuman
	}
	switch captainType {
	case CaptainTypeHuman:
		// proceed
	case CaptainTypeJoe:
		// Joe-attach is the Change 12 inert seam gated on
		// seams.JoeCaptainTypeEnabled. Phase 1: the constant is false —
		// refuse. The seam-enabled paired test exercises the
		// fall-through.
		//
		// See seams.JoeCaptainTypeEnabled's doc comment for the §B R-OVR
		// global-blunt-unlock limitation that bounds future enablement.
		if !seams.JoeCaptainTypeEnabled {
			return nil, ErrOnlyHumansInPhase1
		}
	default:
		return nil, ErrOnlyHumansInPhase1
	}
	if principal == "" {
		return nil, fmt.Errorf("attach: principal required")
	}

	sess, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, fmt.Errorf("attach: session %q not found", sessionID)
	}
	if sess.Type != SessionTypeIncident {
		// §B4: captain only exists in incident regime. Attach is
		// informational — no captain row written.
		return &AttachResult{BecameCaptain: false}, nil
	}

	existing, err := s.repo.GetActiveCaptain(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Captain already attached. This human is an observer (§A3);
		// no captain row written.
		return &AttachResult{BecameCaptain: false}, nil
	}

	// pending_captain → first authorized human becomes captain (R-CAP2).
	id := uuid.NewString()
	active := TransferStateActive
	now := time.Now().UTC()
	_, err = s.repo.AttachCaptain(ctx, Captain{
		ID:            id,
		SessionID:     sessionID,
		CaptainType:   captainType,
		Principal:     principal,
		AttachedAt:    now,
		TransferState: &active,
		LastSeenAt:    &now,
	})
	if err != nil {
		return nil, err
	}
	return &AttachResult{BecameCaptain: true, CaptainID: id}, nil
}

// TransferBeginResult describes the outcome of BeginTransfer. Either the
// transfer is pending a decision (a solicitation was opened on the
// current run) or it has already completed (the unreachable-captain
// shortcut path).
type TransferBeginResult struct {
	// State after BeginTransfer: either TransferStateTransferRequested
	// (waiting on a solicitation resolution or outgoing-side
	// finish-or-cancel) or TransferStateTransferConfirmed (direct
	// transfer due to outgoing-unreachable, or a future B-OVR
	// force-yield).
	State TransferState

	// SolicitationID is populated iff State == transfer_requested and
	// the implementation opened a decision solicitation on the run.
	// (B3 finish-or-cancel for outgoing-initiated; incoming-when-
	// reachable approve/decline for incoming-initiated.)
	SolicitationID string

	// NewCaptainID is populated iff State == transfer_confirmed. The
	// new active captain row.
	NewCaptainID string
}

// BeginTransfer enforces the §B state machine. dual initiation:
//   - outgoing-initiated: the current captain hands off → records a
//     decision solicitation on the current run asking finish-or-cancel.
//     Stays active until they decide.
//   - incoming-initiated, current captain reachable: records a decision
//     solicitation on the run asking the outgoing captain
//     approve/decline. Stays active.
//   - incoming-initiated, current captain unreachable: proceeds directly
//     to transfer_confirmed via the §6-D reachability signal.
//
// R-OVR (B-OVR): when the current captain is type joe and the incoming
// human is RBAC-authorized, the §B state machine is structurally
// force-overridden to immediate transfer_confirmed. Change 12 wires
// this; the TODO below marks where the override branch lands. The
// current captain in Phase 1 is always type human (Joe-captain is an
// inert seam), so no override path fires here yet.
//
// Pre-existing transfer_state on the active captain → ErrTransferAlreadyInFlight.
// No active captain → ErrNoActiveCaptain.
func (s *CaptainService) BeginTransfer(
	ctx context.Context,
	sessionID string,
	initiator TransferInitiator,
	requestingPrincipal string,
	incomingPrincipal string,
	runID string, // current run id; required to open the decision solicitation
) (*TransferBeginResult, error) {
	if requestingPrincipal == "" || incomingPrincipal == "" {
		return nil, fmt.Errorf("begin transfer: requesting and incoming principal required")
	}

	current, err := s.repo.GetActiveCaptain(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrNoActiveCaptain
	}
	if current.TransferState != nil && *current.TransferState != TransferStateActive {
		return nil, ErrTransferAlreadyInFlight
	}

	// R-OVR (B-OVR force-yield): when the current captain is type joe
	// and the incoming request is incoming-initiated from an
	// RBAC-authorized human, the §B state machine is STRUCTURALLY
	// force-overridden to immediate transfer_confirmed. Joe-captain
	// cannot decline or delay human takeover. Compiled in — not behind
	// a seam flag, not configurable, not subject to Joe's judgment.
	//
	// The branch routes ONLY to completeTransfer; there is no joe-
	// declines code path. The B-OVR named structural guard in
	// internal/sessionmodel/captain_bovr_test.go enforces this property
	// via go/ast: any switch case or if-branch in BeginTransfer that
	// inspects CaptainTypeJoe must route to completeTransfer.
	//
	// Phase 1 has no joe-type captain in production code (Attach refuses
	// captain_type=joe via the seams.JoeCaptainTypeEnabled gate), so
	// this branch is unreachable through normal flows. The B-OVR test
	// exercises it by directly inserting a session_captains row with
	// captain_type='joe' through the repository, bypassing Attach.
	if current.CaptainType == CaptainTypeJoe && initiator == TransferInitiatorIncoming {
		newCaptainID, err := s.completeTransfer(ctx, current, incomingPrincipal)
		if err != nil {
			return nil, err
		}
		return &TransferBeginResult{State: TransferStateTransferConfirmed, NewCaptainID: newCaptainID}, nil
	}

	switch initiator {
	case TransferInitiatorOutgoing:
		// Outgoing-initiated: only the current captain may initiate via
		// this side of the API. Reject mismatched principals to keep
		// the contract explicit.
		if current.Principal != requestingPrincipal {
			return nil, fmt.Errorf("begin transfer: outgoing initiator must be current captain (%q), got %q",
				current.Principal, requestingPrincipal)
		}
		solicitationID, err := s.openTransferDecisionSolicitation(ctx, runID, "outgoing_finish_or_cancel", current.Principal, incomingPrincipal)
		if err != nil {
			return nil, err
		}
		state := TransferStateTransferRequested
		initiatorVal := initiator
		incoming := incomingPrincipal
		if err := s.repo.UpdateCaptainTransferState(ctx, current.ID, &state, &incoming, &initiatorVal); err != nil {
			return nil, err
		}
		return &TransferBeginResult{State: state, SolicitationID: solicitationID}, nil

	case TransferInitiatorIncoming:
		reachable, err := s.repo.IsCaptainReachable(ctx, sessionID, s.reachableThresholdSec)
		if err != nil {
			return nil, err
		}
		if reachable {
			// Ask outgoing captain approve/decline via a decision
			// solicitation on the run. State -> transfer_requested.
			solicitationID, err := s.openTransferDecisionSolicitation(ctx, runID, "incoming_request_approve_decline", current.Principal, incomingPrincipal)
			if err != nil {
				return nil, err
			}
			state := TransferStateTransferRequested
			initiatorVal := initiator
			incoming := incomingPrincipal
			if err := s.repo.UpdateCaptainTransferState(ctx, current.ID, &state, &incoming, &initiatorVal); err != nil {
				return nil, err
			}
			return &TransferBeginResult{State: state, SolicitationID: solicitationID}, nil
		}
		// Outgoing unreachable per the §6-D signal → direct
		// transfer_confirmed (single sanctioned timeout exception per §B3).
		newCaptainID, err := s.completeTransfer(ctx, current, incomingPrincipal)
		if err != nil {
			return nil, err
		}
		return &TransferBeginResult{State: TransferStateTransferConfirmed, NewCaptainID: newCaptainID}, nil

	default:
		return nil, fmt.Errorf("begin transfer: unknown initiator %q (want outgoing or incoming)", initiator)
	}
}

// ConfirmTransfer finalizes a transfer that's in transfer_requested.
// Used by both:
//   - outgoing-initiated B3 finish-or-cancel after outgoing resolves the
//     solicitation with "finish" or "cancel" (caller resolves the
//     solicitation first and then calls this if confirm).
//   - incoming-initiated approve-by-current-captain.
//
// Returns the new active captain's ID. §B1 principal-threading: the
// new captain's row is the canonical source for CurrentCaptainPrincipal
// from this point forward.
func (s *CaptainService) ConfirmTransfer(ctx context.Context, sessionID, callerPrincipal string) (string, error) {
	current, err := s.repo.GetActiveCaptain(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if current == nil {
		return "", ErrNoActiveCaptain
	}
	if current.TransferState == nil || *current.TransferState != TransferStateTransferRequested {
		return "", ErrNoTransferInFlight
	}
	if current.IncomingPrincipal == nil {
		return "", fmt.Errorf("confirm transfer: no incoming_principal recorded on active captain")
	}
	// §B authorization binding: confirm is reserved to the solicited
	// incoming principal named in the in-flight record (begin persists it as
	// incoming_principal on the active captain row, scoped to this session).
	// The outgoing captain cannot confirm in their place, and an unrelated
	// authenticated principal cannot confirm at all. Without this check the
	// caller is authenticated but not bound to the handshake.
	if callerPrincipal != *current.IncomingPrincipal {
		return "", ErrNotSolicitedIncoming
	}
	return s.completeTransfer(ctx, current, *current.IncomingPrincipal)
}

// CancelTransfer aborts a transfer in transfer_requested, leaving the
// current captain active.
func (s *CaptainService) CancelTransfer(ctx context.Context, sessionID, callerPrincipal string) error {
	current, err := s.repo.GetActiveCaptain(ctx, sessionID)
	if err != nil {
		return err
	}
	if current == nil {
		return ErrNoActiveCaptain
	}
	if current.TransferState == nil || *current.TransferState != TransferStateTransferRequested {
		return ErrNoTransferInFlight
	}
	// §B authorization binding: cancel is open to EITHER party to the
	// handshake — the soliciting/outgoing captain (current.Principal) or the
	// solicited incoming principal (current.IncomingPrincipal) — but no
	// third principal may abort a transfer it is not part of. Both parties
	// are persisted on the active captain row by begin, scoped to this
	// session.
	incoming := ""
	if current.IncomingPrincipal != nil {
		incoming = *current.IncomingPrincipal
	}
	if callerPrincipal != current.Principal && callerPrincipal != incoming {
		return ErrNotTransferParty
	}
	active := TransferStateActive
	return s.repo.UpdateCaptainTransferState(ctx, current.ID, &active, nil, nil)
}

// completeTransfer is the shared step that detaches the current captain
// and inserts the new captain row. Called by:
//   - BeginTransfer's incoming-unreachable shortcut.
//   - ConfirmTransfer for the normal approve path.
//
// Phase 1 implementation note: the two writes happen sequentially
// against the repository (no shared tx). A failure between them leaves
// the session captain-less, which a subsequent Attach would heal
// (R-CAP2 pending-captain path). Tightening this to a single tx is a
// later cleanup once the repository exposes a transactional facade.
func (s *CaptainService) completeTransfer(ctx context.Context, outgoing *Captain, incomingPrincipal string) (string, error) {
	now := time.Now().UTC()
	if err := s.repo.MarkCaptainDetached(ctx, outgoing.ID, now); err != nil {
		return "", err
	}
	newID := uuid.NewString()
	active := TransferStateActive
	_, err := s.repo.AttachCaptain(ctx, Captain{
		ID:            newID,
		SessionID:     outgoing.SessionID,
		CaptainType:   CaptainTypeHuman,
		Principal:     incomingPrincipal,
		AttachedAt:    now,
		TransferState: &active,
		LastSeenAt:    &now,
	})
	if err != nil {
		return "", err
	}
	return newID, nil
}

// openTransferDecisionSolicitation creates a decision-kind solicitation
// on runID with a payload describing the transfer. Used by B3
// outgoing-initiated finish-or-cancel and incoming-when-reachable
// approve-decline branches. Phase 1 keeps the payload as a JSON blob
// the HTTP layer round-trips; the §D taxonomy distinguishes solicitation
// types by kind, not by payload schema.
func (s *CaptainService) openTransferDecisionSolicitation(
	ctx context.Context,
	runID string,
	reason string,
	outgoingPrincipal string,
	incomingPrincipal string,
) (string, error) {
	if runID == "" {
		return "", fmt.Errorf("open transfer solicitation: runID required (captain transfer needs an active run to host the decision)")
	}
	payload, err := json.Marshal(map[string]string{
		"kind":               "captain_transfer",
		"reason":             reason,
		"outgoing_principal": outgoingPrincipal,
		"incoming_principal": incomingPrincipal,
	})
	if err != nil {
		return "", fmt.Errorf("open transfer solicitation: marshal payload: %w", err)
	}
	id := uuid.NewString()
	_, err = s.runRepo.OpenSolicitation(ctx, runmodel.Solicitation{
		ID:      id,
		RunID:   runID,
		Kind:    runmodel.SolicitationKindDecision,
		Payload: string(payload),
	})
	if err != nil {
		return "", err
	}
	return id, nil
}
