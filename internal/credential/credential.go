// Package credential implements the credential-provider abstraction from
// D-0026 (docs/project/adr/D-0026-credential-provider-abstraction.md).
//
// A provider selects WHICH credential source a component's adapter consumes
// (launch model: provider-selects-the-source, credential stays adapter-resident).
// Resolution returns one typed result with two structurally separated halves:
//
//   - A serializable Diagnostic half (component identity, provider kind,
//     audience, expiry, the stage reached, a non-sensitive failure reason) that
//     flows freely to logs, traces and the UI (R4 observable-per-stage).
//   - A non-serializable Credential half (the means the adapter consumes) that
//     is structurally incapable of leaking: no JSON tags, every String/marshal/
//     format path returns a fixed redacted constant, and the value is reachable
//     only through an explicit typed accessor (R3 cannot-serialize, R1
//     reference-not-value).
//
// Resolution code never branches on static-vs-refreshing outside the provider
// implementations; that uniformity is the safety property.
package credential

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Kind is the provider-kind discriminator carried by component config under the
// "credential_provider" field. When absent it defaults to KindStatic so existing
// components keep their current behavior (the degenerate case is the default).
type Kind string

const (
	// KindStatic is the static/env-var provider: the credential is a wrapped
	// long-lived value. The degenerate case, not the base case.
	KindStatic Kind = "static"
	// KindStaticBearer is the static-bearer provider: it resolves a long-lived
	// bearer token from one of two locator sources — a named environment variable
	// (call-time lookup, name-only stored) or the pod-mounted service-account
	// token read directly. The adapter applies the token as an Authorization
	// bearer header on a hand-built *rest.Config. Distinct from KindStatic so its
	// in_cluster source stays contained to the kubernetes transport and never
	// leaks onto the single-token HTTP backends (agent-identity-doc-02, D-0060).
	KindStaticBearer Kind = "static-bearer"
	// KindEntraExchange is the Entra-exchange provider: it MINTS a short-lived
	// bearer token via an Azure Entra OAuth2 client-credentials exchange (tenant,
	// client id, audience/scope, and a client-secret reference all from config).
	// Like KindStaticBearer its resolved credential is a bearer token the adapter
	// applies as an Authorization bearer header on a hand-built *rest.Config — the
	// two are consumed through the identical BearerToken accessor. The provider is
	// transport-agnostic (no kubernetes or Azure-SDK binding) so the deferred Azure
	// credential track can reuse it (agent-identity-doc-03, D-0063).
	KindEntraExchange Kind = "entra-exchange"
	// KindNone is the explicit no-credential provider: the component reaches its
	// backend UNAUTHENTICATED. It exists so "this backend needs no credential" is
	// a DELIBERATE, admin-attributed, audited statement made at promotion rather
	// than a defaulted absence — arming a component with it is the operator saying
	// "reach out to this target with no credential", which is a privilege grant in
	// its own right even though no secret is referenced. Its reference carries no
	// locator at all: the discriminator IS the whole reference. Resolve succeeds
	// trivially and yields NO credential, so every typed accessor (StaticValue,
	// BearerToken) reports false and an adapter that requires a credential fails
	// closed rather than silently proceeding.
	KindNone Kind = "none"
)

// bearerKinds is the set of provider Kinds whose resolved credential is a bearer
// token applied through Resolution.BearerToken. It is the single declaration the
// accessor consults so the adapter consume-seam is Kind-agnostic across every
// bearer-bearing provider: a new bearer Kind joins here and rides the same seam
// without the adapter or the accessor's call sites changing.
var bearerKinds = map[Kind]struct{}{
	KindStaticBearer:  {},
	KindEntraExchange: {},
}

// isBearer reports whether a Kind's resolved credential is a bearer token reached
// through the BearerToken accessor.
func isBearer(kind Kind) bool {
	_, ok := bearerKinds[kind]
	return ok
}

// Stage is the diagnostic spine (R4): the ordered states a resolution can reach.
//
//	provider-selected -> mint-attempted -> mint-succeeded -> connectivity-probed
//
// StageMintSucceeded WITHOUT StageConnectivityProbed is a legal terminal success
// (the lazy-connectivity posture). A failure result stops at the stage it failed.
type Stage int

const (
	// StageProviderSelected: a provider was chosen for the component.
	StageProviderSelected Stage = iota
	// StageMintAttempted: minting/selection was attempted (a failure stops here).
	StageMintAttempted
	// StageMintSucceeded: the credential source is ready; a legal terminal
	// success even if connectivity is never probed.
	StageMintSucceeded
	// StageConnectivityProbed: connectivity was proven against the backend.
	StageConnectivityProbed
)

// String returns the canonical hyphenated stage name.
func (s Stage) String() string {
	switch s {
	case StageProviderSelected:
		return "provider-selected"
	case StageMintAttempted:
		return "mint-attempted"
	case StageMintSucceeded:
		return "mint-succeeded"
	case StageConnectivityProbed:
		return "connectivity-probed"
	default:
		return "unknown"
	}
}

// MarshalJSON renders the stage as its canonical name so diagnostic JSON is
// human-readable and stable.
func (s Stage) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// Diagnostic is the freely-serializable half of a resolution. It carries no
// credential material — only identity and observable outcome — and is safe to
// emit to logs, traces, audit rows and the UI.
type Diagnostic struct {
	ComponentID string     `json:"component_id"`
	Provider    Kind       `json:"provider"`
	Audience    string     `json:"audience,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Stage       Stage      `json:"stage"`
	OK          bool       `json:"ok"`
	// Reason is a non-sensitive failure reason; it NEVER contains raw plugin
	// stderr or credential material.
	Reason string `json:"reason,omitempty"`
}

const redactedCredential = "[REDACTED CREDENTIAL]"

// Credential is the non-serializable half of a resolution: the means the adapter
// consumes. It has no exported fields and no JSON tags, and every String/marshal/
// format path returns a fixed redacted constant, so it cannot leak into
// responses, audit rows, errors, logs or prompt payloads (R3). The underlying
// means is reachable ONLY through the typed accessors on Resolution
// (StaticValue / BearerToken / CapturedStderr).
type Credential struct {
	kind   Kind
	static string // KindStatic: the wrapped value; KindStaticBearer/KindEntraExchange: the resolved bearer token
	stderr string // captured provider stderr (human-facing accessor only; vestigial — see CapturedStderr)
}

// String returns the redacted constant. Never the credential.
func (Credential) String() string { return redactedCredential }

// GoString returns the redacted constant for %#v. Never the credential.
func (Credential) GoString() string { return redactedCredential }

// MarshalJSON returns the redacted constant. The credential never serializes.
func (Credential) MarshalJSON() ([]byte, error) {
	return json.Marshal(redactedCredential)
}

// Format makes Credential a fmt.Formatter, so EVERY verb (%v, %+v, %#v, %s, %q,
// ...) renders the redacted constant rather than the underlying fields.
func (Credential) Format(f fmt.State, _ rune) {
	io.WriteString(f, redactedCredential)
}

// Resolution is the typed staged result of a Resolve/Probe call. Its Diagnostic
// half is freely serializable; its credential half is reachable only through the
// typed accessors. Resolution itself fully overrides every serialization and
// formatting path to emit the diagnostic half only, so the credential cannot leak
// even through the enclosing value.
type Resolution struct {
	// Diagnostic is the serializable half.
	Diagnostic Diagnostic
	// cred is the non-serializable half (unexported, no JSON tag).
	cred Credential
}

// StaticValue returns the wrapped static credential value and true when this is a
// static resolution. This is the deliberate typed accessor the seam/adapter calls.
func (r *Resolution) StaticValue() (string, bool) {
	if r.cred.kind != KindStatic {
		return "", false
	}
	return r.cred.static, true
}

// BearerToken returns the resolved bearer token and true when this is a
// bearer-bearing resolution (any Kind in bearerKinds: static-bearer or
// entra-exchange today). This is the deliberate typed accessor the kubernetes
// adapter calls to apply the token as an Authorization bearer header; like
// StaticValue it is the ONLY path to the underlying value, and it is Kind-agnostic
// across bearer providers so a minted Entra token rides the same seam as a
// static-bearer token with no adapter change.
func (r *Resolution) BearerToken() (string, bool) {
	if !isBearer(r.cred.kind) {
		return "", false
	}
	return r.cred.static, true
}

// CapturedStderr returns the raw provider stderr captured on a mint failure —
// the deliberate human-facing "paste this" affordance, the ONLY path to the
// captured text, never in the diagnostic half or any structured-log rendering.
// VESTIGIAL as of agent-identity-doc-04: the kubeconfig-exec provider was the
// only producer of captured stderr and has been deleted, so no provider sets it
// today and this always returns "". The accessor and its admin/UI consumers are
// left in place pending a separate teardown (docs/backlog/credential-stderr-surface-teardown.md).
func (r *Resolution) CapturedStderr() string { return r.cred.stderr }

// String renders the diagnostic half only — never the credential or stderr.
func (r Resolution) String() string {
	d := r.Diagnostic
	return fmt.Sprintf("Resolution{component=%s provider=%s stage=%s ok=%t reason=%q credential=%s}",
		d.ComponentID, d.Provider, d.Stage, d.OK, d.Reason, redactedCredential)
}

// GoString renders the diagnostic half only (for %#v).
func (r Resolution) GoString() string { return r.String() }

// Format makes Resolution a fmt.Formatter so every verb routes through String,
// guaranteeing the credential half is never reached by any fmt path.
func (r Resolution) Format(f fmt.State, _ rune) {
	io.WriteString(f, r.String())
}

// MarshalJSON serializes the diagnostic half only; the credential half never
// reaches JSON.
func (r Resolution) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.Diagnostic)
}

// MarshalText serializes the diagnostic half only, covering slog's TextHandler
// path; the credential half never reaches text rendering.
func (r Resolution) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}
