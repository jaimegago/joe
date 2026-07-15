// Package access is the single guarded seam through which all
// infrastructure-adapter and graph-store access must flow.
//
// Identity & Authentication design (docs/reference/joe-identity-design.md §2.5),
// Phase A: enforcement moves off the HTTP transport into this accessor.
// Every dispatch method evaluates rbac.IsAllowed BEFORE resolving and
// calling the underlying adapter or graph store. On a denied decision the
// accessor returns ErrPermissionDenied and performs no infrastructure
// call; on a permitted decision it delegates and returns the result.
//
// The action for each operation is declared at the call site of the
// generic guard / permit helper, immediately adjacent to the adapter
// method it gates — NOT inferred from an HTTP verb. This keeps the action
// declaration next to the method's own semantics (design §2.8).
//
// Invariant (asserted by internal/access/guard_test.go and
// internal/api/access_guard_test.go): no package other than this one
// resolves an adapter via Registry.Get or calls a graph-store method, so
// there is exactly one path to reach an adapter or the graph store.
//
// The in-process Core Agent refresh ADAPTER READ is no longer an exception:
// since A001-COREGOV CC-05 it resolves every component's adapter through
// access.ResolveAdapter under the agent:core principal, so the read is
// GOVERNED by this seam (and floored by the promote-aware engine) like any
// other adapter access.
//
// The Core Agent's graph WRITE (internal/coreagent ApplyGraphDelta ->
// raw services.Graph.AddNode/AddEdge/Delete*) remains outside this seam by
// intent — NOT a deferred or not-yet-governed gap. It is an internal Tier-3
// knowledge write, governed upstream by the agent:core read floor
// (A001-COREGOV): a component the refresh read denies yields no delta, so no
// write occurs. The write carries the agent:core principal for audit and
// takes no write permit by design. See the allowlist in
// api/access_guard_test.go.
package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// GraphComponentID is the reserved source identifier used when evaluating
// RBAC for graph-store operations. The graph store is not keyed by a real
// infrastructure source, so a stable reserved key is used; like any source
// with no zone assignment it resolves to the "unassigned" zone, which is
// exactly how the previous HTTP transport gated graph paths (their parsed
// path segments were likewise non-source strings resolving to unassigned).
const GraphComponentID = "graph"

// ErrPermissionDenied is returned when the policy engine denies a decision.
// It wraps no infrastructure error because, by contract, no infrastructure
// call is attempted on denial.
var ErrPermissionDenied = errors.New("access denied by RBAC policy")

// PermissionDeniedError is the typed permission-denied error the accessor
// returns on a denied decision. It wraps ErrPermissionDenied (so every existing
// errors.Is(err, ErrPermissionDenied) check keeps working unchanged) and carries
// the structured decision Reason (rbac.ReasonNoGrant, rbac.ReasonActionNotInZone,
// …) alongside the component and action. Transport layers extract it via
// errors.As to surface WHY a request was denied (e.g. in the 403 body) without
// re-parsing the message. This is observability only: the deny OUTCOME and the
// human-readable message are byte-identical to the prior fmt.Errorf form.
type PermissionDeniedError struct {
	Principals rbac.PrincipalSet
	Component  string
	Action     string
	Reason     string
}

// Error reproduces the exact message the prior fmt.Errorf(ErrPermissionDenied,…)
// produced, so callers matching on the text (e.g. the loop's "access denied"
// tool-error surface) are unaffected.
func (e *PermissionDeniedError) Error() string {
	return fmt.Sprintf("%s: principals=%v component=%q action=%q reason=%s",
		ErrPermissionDenied.Error(), e.Principals, e.Component, e.Action, e.Reason)
}

// Unwrap ties the typed error to the ErrPermissionDenied sentinel.
func (e *PermissionDeniedError) Unwrap() error { return ErrPermissionDenied }

// ErrComponentNotFound is returned when no adapter is registered for the
// requested component. It wraps store.ErrComponentNotFound so existing HTTP error
// mapping (errors.Is(err, store.ErrComponentNotFound) → 404) is preserved.
var ErrComponentNotFound = fmt.Errorf("%w", store.ErrComponentNotFound)

// ErrWrongAdapterType is returned when the registered adapter for a component
// does not implement the requested typed interface (e.g. asking for a
// Kubernetes operation on a Git component).
var ErrWrongAdapterType = errors.New("component is not the expected adapter type")

// ErrGraphUnavailable is returned when a graph operation is attempted but no
// graph store is wired (services.Graph == nil).
var ErrGraphUnavailable = errors.New("graph store not available")

// Accessor is the guarded seam in front of the adapter registry and the
// graph store. Construct it with New; it is safe for concurrent use (the
// registry and graph store it wraps are themselves concurrency-safe).
type Accessor struct {
	registry *adapters.Registry
	graph    graph.GraphStore
	// engine is the RBAC policy engine, constructed once at the composition
	// root (cmd/joe/server.go) and injected via api.New. A nil engine means
	// RBAC is disabled (auth not configured) and every decision is permitted;
	// production always injects a non-nil governance-wired engine downstream of
	// cmd/joe's refuse-to-start guard.
	engine *rbac.PolicyEngine
	// auditRepo is the append-only audit trail (Identity Phase F,
	// docs/reference/joe-identity-design.md §2.6). Every decision the accessor
	// makes — allow and deny alike — writes ONE row here at the decision
	// point. A nil auditRepo is treated as "audit disabled" (used by
	// dev/local runs without a database); cmd/joe/server.go always
	// wires a real one. The failure split (§4) is fail-CLOSED for
	// mutating actions and fail-OPEN for reads (see audit.FailurePosture).
	auditRepo audit.Repository
}

// New builds an Accessor over the given registry, graph store, policy
// engine, and audit repository. engine may be nil (RBAC disabled); auditRepo
// may be nil (audit disabled — for tests and dev runs without a DB).
// Production wiring in cmd/joe/server.go and internal/api/server.go
// always supplies a real audit.Repository.
func New(registry *adapters.Registry, graphStore graph.GraphStore, engine *rbac.PolicyEngine, auditRepo audit.Repository) *Accessor {
	return &Accessor{registry: registry, graph: graphStore, engine: engine, auditRepo: auditRepo}
}

// permit is the single enforcement chokepoint. It evaluates the policy engine
// for (principals, sourceID, action) — the authorization subject is a SET of
// principals, permitted if ANY member holds a matching grant (union of grants;
// docs/reference/joe-identity-design.md §2.7) — and writes exactly ONE audit row to the
// append-only audit log capturing the decision (Phase F, design §2.6). On a
// denied decision it returns ErrPermissionDenied.
//
// Audit-write failures honour the §4 failure split:
//   - Mutating actions (ActionMutate, ActionDelete) fail CLOSED: if the audit
//     row cannot be written, permit returns the audit error and the caller
//     does not invoke the adapter or graph method.
//   - Reads (ActionRead, ActionQuery) fail OPEN: a missing audit row is
//     logged loudly but does not block the action.
//
// A nil engine permits everything (RBAC disabled) — but the audit row is
// still written, with reason "rbac_disabled", so the trail is complete even
// in unauthenticated dev mode. A nil auditRepo skips the audit write
// entirely (test/dev only; cmd/joe/server.go always wires a real repo).
//
// At launch the set has exactly one member — the caller's own context-derived
// principal — formed by permitForPrincipal below; the set shape is built now so
// group: members can be added later with no change here.
//
// permit performs NO infrastructure access: callers must invoke it before
// resolving or calling any adapter or graph method, so that a denial never
// touches infrastructure.
func (a *Accessor) permit(ctx context.Context, principals rbac.PrincipalSet, sourceID string, action rbac.Action) error {
	// Determine the decision (allow/deny) and the structured details
	// captured in the audit row.
	var (
		allowed bool
		zone    string
		reason  string
	)
	if a.engine == nil {
		allowed = true
		zone = ""
		reason = "rbac_disabled"
	} else {
		d := a.engine.Decide(ctx, principals, sourceID, action)
		allowed = d.Allowed
		zone = d.Zone
		reason = d.Reason
	}

	// Write the audit row before returning. Single primary principal for
	// the row is the first member of the size-1 set; the design keeps the
	// set size 1 in v1, and the column is just one principal.
	primaryPrincipal := ""
	if len(principals) > 0 {
		primaryPrincipal = string(principals[0])
	}
	decision := audit.DecisionDeny
	if allowed {
		decision = audit.DecisionAllow
	}
	if a.auditRepo != nil {
		auditErr := a.auditRepo.Insert(ctx, audit.Event{
			Principal:   primaryPrincipal,
			Action:      string(action),
			Zone:        zone,
			ComponentID: sourceID,
			Decision:    decision,
			Reason:      reason,
			Kind:        audit.KindInfraAccess,
		})
		if auditErr != nil {
			// Fail-closed for mutate/delete; fail-open for reads.
			if blocking := audit.FailurePosture(ctx, string(action), auditErr, "accessor", audit.PostureForAction(string(action))); blocking != nil {
				return fmt.Errorf("audit write failed for mutating action: %w", blocking)
			}
		}
	}

	// One structured request-log line per decision at the single chokepoint
	// (observability only — the outcome is unchanged): deny at Warn so an
	// operator sees the reason a request was refused without reading the audit
	// table; allow at Debug so the reason (e.g. team_flat_read) is available on
	// demand without flooding steady-state logs.
	if allowed {
		slog.Debug("rbac decision",
			"decision", "allow", "principal", primaryPrincipal,
			"component", sourceID, "action", string(action), "zone", zone, "reason", reason)
	} else {
		slog.Warn("rbac decision",
			"decision", "deny", "principal", primaryPrincipal,
			"component", sourceID, "action", string(action), "zone", zone, "reason", reason)
	}

	if !allowed {
		return &PermissionDeniedError{Principals: principals, Component: sourceID, Action: string(action), Reason: reason}
	}
	return nil
}

// permitForPrincipal lifts the context-derived caller principal into a size-1
// authorization subject and delegates to the set-shaped permit. This is the
// single seam where the accessor forms the subject from the caller principal;
// it is where group: members (from an IdP groups claim) will be added in a
// later phase without touching every dispatch method. It performs no
// infrastructure access (see permit).
func (a *Accessor) permitForPrincipal(ctx context.Context, principal rbac.Principal, sourceID string, action rbac.Action) error {
	return a.permit(ctx, rbac.NewPrincipalSet(principal), sourceID, action)
}

// ResolveAdapter enforces (principal, sourceID, action) and, on a permitted
// decision, resolves and returns the base adapter for sourceID untyped. It is
// the guarded entry point for callers that perform their own concrete-type
// dispatch on the resolved adapter (the in-process Core Agent refresh path,
// A001-COREGOV CC-05): the autonomous refresh type-switches the adapter per
// component type, so it cannot use the generic, type-parameterised guard.
//
// Like guard and observeResolve, permit runs BEFORE a.registry.Get, so an
// ungranted/unpromoted component is DENIED with ErrPermissionDenied before its
// adapter — and thus its credential — is ever resolved. The action is declared
// by the caller (refresh passes rbac.ActionRead) so it stays adjacent to the
// semantics of the call (design §2.8). This method performs no infrastructure
// access beyond resolving the adapter handle; the caller invokes adapter
// methods itself.
func (a *Accessor) ResolveAdapter(ctx context.Context, principal rbac.Principal, sourceID string, action rbac.Action) (adapters.Adapter, error) {
	// This seam resolves adapters for the autonomous refresh path, which is
	// read-only on customer infrastructure by invariant (api/access_guard_test.go
	// VERDICT-A). Declaring rbac.ActionRead here — adjacent to the gated resolve,
	// per design §2.8 — pins that contract: a caller that asks this method to
	// resolve an adapter for a mutating action is a programming error, not a
	// silent privilege escalation, so it is rejected before any permit/resolve.
	if action != rbac.ActionRead && action != rbac.ActionQuery {
		return nil, fmt.Errorf("%w: ResolveAdapter is a read-only seam, refusing action %q", ErrPermissionDenied, action)
	}
	if err := a.permitForPrincipal(ctx, principal, sourceID, action); err != nil {
		return nil, err
	}
	adapter, err := a.registry.Get(sourceID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrComponentNotFound, sourceID)
		}
		return nil, err
	}
	return adapter, nil
}

// guard enforces (principal, sourceID, action), then resolves the adapter
// for sourceID and type-asserts it to T. It is the generic resolve path for
// every typed adapter dispatch method. On a denied decision it returns
// ErrPermissionDenied and never calls Registry.Get, so no infrastructure
// adapter is touched.
//
// guard is a package-level function (Go methods cannot be type-parameterised)
// but it is the ONLY code that calls a.registry.Get — the static guard test
// relies on that. typeName is a human-readable adapter kind used only for the
// ErrWrongAdapterType message.
func guard[T any](
	a *Accessor,
	ctx context.Context,
	principal rbac.Principal,
	sourceID string,
	action rbac.Action,
	typeName string,
) (T, error) {
	var zero T
	if err := a.permitForPrincipal(ctx, principal, sourceID, action); err != nil {
		return zero, err
	}
	adapter, err := a.registry.Get(sourceID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return zero, fmt.Errorf("%w: %s", ErrComponentNotFound, sourceID)
		}
		return zero, err
	}
	typed, ok := any(adapter).(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrWrongAdapterType, typeName)
	}
	return typed, nil
}
