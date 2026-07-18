package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// captainHandler exposes the §B captain attach + transfer state machine
// over HTTP. Phase 1 Change 6. See the session-model design (Phase 0) §B and
// the §6-D reachability finding in
// internal/store/migrations/013_captain_reachability.up.sql.
//
// Endpoints (re-homed under /api/v1/sessions/{id}/captain in B005, §12.8 —
// the legacy /agent-sessions namespace was removed):
//   - POST /attach              R-CAP2/R-CAP3 attach
//   - POST /heartbeat           §6-D reachability heartbeat
//   - POST /transfer/begin      §B BeginTransfer (dual initiation)
//   - POST /transfer/confirm    §B ConfirmTransfer
//   - POST /transfer/cancel     §B CancelTransfer
//
// Authorization (§12.7): the captain/regime state machine is OUTSIDE the
// sessionAccess seam vocabulary by design — captaincy is not a session
// read/write/delete action — so these routes are NOT routed through the seam.
// Each carries explicit route-level authorization: every handler rejects an
// unresolved principal (401) before any state change, and the heartbeat /
// transfer handlers additionally enforce captain/party identity at the
// repository layer (RecordCaptainHeartbeat and the transfer service refuse
// non-captain / non-party principals). They remain componentless, so the
// component-keyed guarded accessor never gates them.
type captainHandler struct {
	repo sessionmodel.Repository
	svc  *sessionmodel.CaptainService
	// auditRepo writes the durable record of captain attach and transfer
	// events. Phase F: these histories previously cascade-deleted with the
	// session row (009:62 ON DELETE CASCADE) and were therefore lost on
	// resolve. The audit row is independent of the session — the record
	// survives. May be nil in dev/local runs without a database.
	auditRepo audit.Repository
}

func (s *Server) registerCaptainRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.SessionModel == nil || s.services.CaptainSvc == nil {
		return
	}
	h := &captainHandler{repo: s.services.SessionModel, svc: s.services.CaptainSvc, auditRepo: s.services.Audit}

	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/captain/attach", prefix), h.attach)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/captain/heartbeat", prefix), h.heartbeat)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/captain/transfer/begin", prefix), h.transferBegin)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/captain/transfer/confirm", prefix), h.transferConfirm)
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/captain/transfer/cancel", prefix), h.transferCancel)
}

type attachRequest struct {
	CaptainType string `json:"captain_type,omitempty"` // "human" (default) or "joe" (Change 12 inert seam — refused in Phase 1)
}

func (h *captainHandler) attach(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeBadRequest(w, nil, "captain attach", "missing session id")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}

	var req attachRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBadRequest(w, err, "captain attach", "invalid request body")
			return
		}
	}
	captainType := sessionmodel.CaptainTypeHuman
	if req.CaptainType != "" {
		switch req.CaptainType {
		case "human":
			captainType = sessionmodel.CaptainTypeHuman
		case "joe":
			writeError(w, http.StatusForbidden, "forbidden",
				"captain_type=joe is an inert seam (not yet enabled)")
			return
		default:
			writeBadRequest(w, nil, "captain attach", "captain_type must be 'human' or 'joe'")
			return
		}
	}

	// Phase F: write the durable audit row BEFORE the captain mutation
	// runs. session_captains rows cascade-delete with the session
	// (migration 009:62), so any history that lives only in the captain
	// row is lost on a later resolve+expunge. The audit row is independent
	// of the session and survives.
	if err := h.writeCaptainAudit(r.Context(), principal, audit.ActionCaptainAttach,
		audit.DecisionAllow, "transition_recorded",
		map[string]string{"session_id": sessionID, "captain_type": string(captainType)}); err != nil {
		writeInternalError(w, err, "captain attach audit")
		return
	}

	res, err := h.svc.Attach(r.Context(), sessionID, string(principal), captainType)
	if err != nil {
		if errors.Is(err, sessionmodel.ErrOnlyHumansInPhase1) {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		writeInternalError(w, err, "captain attach")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":     sessionID,
		"principal":      string(principal),
		"became_captain": res.BecameCaptain,
		"captain_id":     res.CaptainID,
	})
}

func (h *captainHandler) heartbeat(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeBadRequest(w, nil, "captain heartbeat", "missing session id")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}

	err := h.repo.RecordCaptainHeartbeat(r.Context(), sessionID, string(principal), time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, sessionmodel.ErrNoActiveCaptain):
			writeError(w, http.StatusConflict, "conflict", "no active captain for this session")
			return
		case errors.Is(err, sessionmodel.ErrCaptainPrincipalMismatch):
			writeError(w, http.StatusForbidden, "forbidden",
				"only the active captain may heartbeat this session")
			return
		}
		writeInternalError(w, err, "captain heartbeat")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"principal":  string(principal),
		"seen_at":    time.Now().UTC().Format(time.RFC3339),
	})
}

type transferBeginRequest struct {
	Initiator         string `json:"initiator"`                    // "outgoing" or "incoming"
	IncomingPrincipal string `json:"incoming_principal,omitempty"` // required for both; for outgoing it's the target, for incoming it's the requesting principal
	RunID             string `json:"run_id"`                       // current run that hosts the decision solicitation
}

func (h *captainHandler) transferBegin(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeBadRequest(w, nil, "transfer begin", "missing session id")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}
	var req transferBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "transfer begin", "invalid request body")
		return
	}
	if req.RunID == "" {
		writeBadRequest(w, nil, "transfer begin",
			"run_id is required (captain transfer needs an active run to host the decision)")
		return
	}
	var initiator sessionmodel.TransferInitiator
	switch req.Initiator {
	case "outgoing":
		initiator = sessionmodel.TransferInitiatorOutgoing
	case "incoming":
		initiator = sessionmodel.TransferInitiatorIncoming
	default:
		writeBadRequest(w, nil, "transfer begin", "initiator must be 'outgoing' or 'incoming'")
		return
	}

	// For incoming-initiated the requesting principal IS the incoming candidate;
	// for outgoing-initiated the body's incoming_principal names the target.
	incoming := req.IncomingPrincipal
	if initiator == sessionmodel.TransferInitiatorIncoming {
		incoming = string(principal)
	}
	if incoming == "" {
		writeBadRequest(w, nil, "transfer begin",
			"incoming_principal is required (target of outgoing-initiated or requester for incoming-initiated — derived from auth)")
		return
	}

	if err := h.writeCaptainAudit(r.Context(), principal, audit.ActionCaptainTransferBegin,
		audit.DecisionAllow, "transition_recorded",
		map[string]string{
			"session_id":         sessionID,
			"initiator":          string(initiator),
			"incoming_principal": incoming,
			"run_id":             req.RunID,
		}); err != nil {
		writeInternalError(w, err, "captain transfer begin audit")
		return
	}

	res, err := h.svc.BeginTransfer(r.Context(), sessionID, initiator,
		string(principal), incoming, req.RunID)
	if err != nil {
		switch {
		case errors.Is(err, sessionmodel.ErrNoActiveCaptain):
			writeError(w, http.StatusConflict, "conflict", "no active captain for this session")
			return
		case errors.Is(err, sessionmodel.ErrTransferAlreadyInFlight):
			writeError(w, http.StatusConflict, "conflict", "transfer already in flight")
			return
		}
		writeInternalError(w, err, "transfer begin")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":      sessionID,
		"state":           string(res.State),
		"solicitation_id": res.SolicitationID,
		"new_captain_id":  res.NewCaptainID,
	})
}

func (h *captainHandler) transferConfirm(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeBadRequest(w, nil, "transfer confirm", "missing session id")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}
	if err := h.writeCaptainAudit(r.Context(), principal, audit.ActionCaptainTransferConfirm,
		audit.DecisionAllow, "transition_recorded",
		map[string]string{"session_id": sessionID}); err != nil {
		writeInternalError(w, err, "captain transfer confirm audit")
		return
	}
	newID, err := h.svc.ConfirmTransfer(r.Context(), sessionID, string(principal))
	if err != nil {
		switch {
		case errors.Is(err, sessionmodel.ErrNoActiveCaptain):
			writeError(w, http.StatusConflict, "conflict", "no active captain")
			return
		case errors.Is(err, sessionmodel.ErrNoTransferInFlight):
			writeError(w, http.StatusConflict, "conflict", "no transfer in flight")
			return
		case errors.Is(err, sessionmodel.ErrNotSolicitedIncoming):
			writeError(w, http.StatusForbidden, "forbidden",
				"only the solicited incoming principal may confirm this transfer")
			return
		}
		writeInternalError(w, err, "transfer confirm")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":     sessionID,
		"new_captain_id": newID,
	})
}

func (h *captainHandler) transferCancel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeBadRequest(w, nil, "transfer cancel", "missing session id")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}
	if err := h.writeCaptainAudit(r.Context(), principal, audit.ActionCaptainTransferCancel,
		audit.DecisionAllow, "transition_recorded",
		map[string]string{"session_id": sessionID}); err != nil {
		writeInternalError(w, err, "captain transfer cancel audit")
		return
	}
	if err := h.svc.CancelTransfer(r.Context(), sessionID, string(principal)); err != nil {
		switch {
		case errors.Is(err, sessionmodel.ErrNoActiveCaptain):
			writeError(w, http.StatusConflict, "conflict", "no active captain")
			return
		case errors.Is(err, sessionmodel.ErrNoTransferInFlight):
			writeError(w, http.StatusConflict, "conflict", "no transfer in flight")
			return
		case errors.Is(err, sessionmodel.ErrNotTransferParty):
			writeError(w, http.StatusForbidden, "forbidden",
				"only a party to the transfer may cancel it")
			return
		}
		writeInternalError(w, err, "transfer cancel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": sessionID, "status": "cancelled"})
}

// writeCaptainAudit records one captain-transition row in the append-only
// audit log. Same shape as regimeHandler.writeRegimeAudit: nil-safe when
// the audit repository is not wired (dev/local), fail-closed on a
// repository error so the captain mutation does not proceed without its
// durable record. The audit row is independent of session_captains and
// therefore survives the cascade-delete on session deletion.
func (h *captainHandler) writeCaptainAudit(ctx context.Context, principal rbac.Principal, action string, decision audit.Decision, reason string, extra map[string]string) error {
	if h.auditRepo == nil {
		return nil
	}
	ctxJSON := "{}"
	if len(extra) > 0 {
		b, err := json.Marshal(extra)
		if err == nil {
			ctxJSON = string(b)
		}
	}
	err := h.auditRepo.Insert(ctx, audit.Event{
		Principal: string(principal),
		Action:    action,
		Decision:  decision,
		Reason:    reason,
		Kind:      audit.KindCaptainTransition,
		Context:   ctxJSON,
	})
	return audit.FailurePosture(ctx, action, err, "captain:"+action, audit.FailClosed)
}
