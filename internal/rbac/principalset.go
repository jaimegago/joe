package rbac

// PrincipalSet is the authorization subject evaluated by the policy engine: a
// set of principals assessed together under union-of-grants semantics. A
// decision is permitted if ANY member holds a matching grant for the component's
// zone and the requested action. The model is additive / allow-only — there
// are no deny rules, consistent with the single-principal model it replaces
// and with Kubernetes RBAC (docs/reference/joe-identity-design.md §2.7).
//
// At launch the set is constructed with exactly one member — the caller's own
// principal. The guarded access.Accessor — the single authoritative RBAC gate on
// both the HTTP transport and the in-process agent-loop path — lifts the
// context-derived caller principal into a size-1 set. The set shape exists now so
// that group membership
// (group:<name> principals sourced from an IdP groups claim) drops in later as
// additional members with no change to the evaluation (design §2.7, §6).
//
// A size-1 set reproduces the previous single-principal decision exactly,
// which is the Phase B regression contract.
type PrincipalSet []Principal

// NewPrincipalSet builds a PrincipalSet from the given principals. Order is
// preserved; because the set is evaluated as a union, order does not affect
// the decision. At launch callers pass exactly one principal (the caller's
// own); passing none yields the empty set, which is permitted by nothing.
func NewPrincipalSet(principals ...Principal) PrincipalSet {
	return PrincipalSet(principals)
}
