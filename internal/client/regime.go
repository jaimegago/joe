package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const (
	apiRegimePath        = "/api/v1/regime"
	apiRegimeDeclarePath = "/api/v1/regime/declare"
	apiRegimeResolvePath = "/api/v1/regime/resolve"
)

// Regime mirrors the system_regime record returned by GET /api/v1/regime.
// The server marshals sessionmodel.Regime with no JSON tags, so the wire
// keys are the exported Go field names (Mode, DeclaredAt, ...); the field
// names here match so decoding round-trips without per-field tags.
// DeclaredAt/DeclaredByPrincipal/DeclaredKind are nil when the mode is
// normal (no active incident).
type Regime struct {
	Mode                string
	DeclaredAt          *time.Time
	DeclaredByPrincipal *string
	DeclaredKind        *string
}

// IsIncident reports whether an incident regime is currently active.
func (r *Regime) IsIncident() bool {
	return r != nil && r.Mode == "incident"
}

// DeclareResult is the POST /api/v1/regime/declare response.
type DeclareResult struct {
	SessionID  string `json:"session_id"`
	CaptainID  string `json:"captain_id"`
	DeclaredBy string `json:"declared_by"`
}

// ResolveResult is the POST /api/v1/regime/resolve response.
type ResolveResult struct {
	SessionID  string `json:"session_id"`
	ResolvedBy string `json:"resolved_by"`
}

// GetRegime reads the current system regime (active incident or normal).
func (c *Client) GetRegime(ctx context.Context) (*Regime, error) {
	var reg Regime
	if err := c.doJSON(ctx, "GET", c.baseURL+apiRegimePath, nil, http.StatusOK, &reg, "get regime"); err != nil {
		return nil, err
	}
	return &reg, nil
}

// DeclareIncident declares an incident regime. kind is the regime
// declared_kind ("human"; "joe" is an inert Phase 1 seam the server
// refuses). reason is forwarded for forensic context; the current server
// records declared_kind only, so an empty reason is harmless. The caller's
// principal (the declaring human) is resolved server-side from the
// credential and echoed back in DeclareResult.DeclaredBy.
func (c *Client) DeclareIncident(ctx context.Context, kind, reason string) (*DeclareResult, error) {
	payload := map[string]string{}
	if kind != "" {
		payload["declared_kind"] = kind
	}
	if reason != "" {
		payload["reason"] = reason
	}
	body, _ := json.Marshal(payload)
	var res DeclareResult
	if err := c.doJSON(ctx, "POST", c.baseURL+apiRegimeDeclarePath, bytes.NewReader(body), http.StatusCreated, &res, "declare incident"); err != nil {
		return nil, err
	}
	return &res, nil
}

// ResolveIncident resolves the active incident regime back to normal. reason
// is forwarded for forensic context (the current server records the
// transition without it). The caller's principal is resolved server-side and
// echoed back in ResolveResult.ResolvedBy.
func (c *Client) ResolveIncident(ctx context.Context, reason string) (*ResolveResult, error) {
	payload := map[string]string{}
	if reason != "" {
		payload["reason"] = reason
	}
	body, _ := json.Marshal(payload)
	var res ResolveResult
	if err := c.doJSON(ctx, "POST", c.baseURL+apiRegimeResolvePath, bytes.NewReader(body), http.StatusOK, &res, "resolve incident"); err != nil {
		return nil, err
	}
	return &res, nil
}
