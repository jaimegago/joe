// Package sessionauthz is the dedicated, single-decision-per-resource-class
// authorization seam for sessions (DESIGN-CHAT-SESSIONS.md §12.7, ledger node
// B003).
//
// It is deliberately SEPARATE from the component RBAC accessor
// (internal/access + internal/rbac). Session-relationship logic never touches
// zones or policies, and component RBAC never learns session ownership. This
// package therefore imports NOTHING from internal/access, internal/rbac,
// internal/graph, or internal/adapters — its only shared dependency with the
// existing authorization stack is the dynamic admin capability check (D-0011),
// which it REUSES through the AdminChecker interface rather than reimplements.
// The "no zone/policy reach" property is asserted structurally by
// TestSeam_ReachesNoZoneOrPolicyMachinery in this package's test.
//
// Design name mapping: §12.7 specifies "one function,
// sessionAccess(principal, sessionID, action) -> decision". In Go that surface
// is the exported (*Seam).SessionAccess method; the session resolver and admin
// checker dependencies are injected once at construction (New) so the per-call
// signature stays faithful to the design — (principal, sessionID, action) — with
// ctx and an error added as the Go-mandated extras (the error lets a caller
// distinguish a deny from an undecidable dependency failure and fail closed).
//
// Decision model (§12.7, as amended 2026-06-21 for team-wide read):
//   - read                — any authenticated principal (owner, team member, or
//     admin). Reading is the default, not a grant; there is no per-session read
//     restriction.
//   - owner-mutate        — write (rename/link/send-message), soft_delete,
//     restore: the session creator, OR an admin (admin is cross-tenant mutation
//     per §12.1, and §12.5 specifies owner/admin restore).
//   - admin-govern        — purge, archive, unarchive, configure_retention:
//     admin only (§12.7 "admin-only").
//   - unauthenticated / nil principal and any unrecognized action: deny.
//
// The creator comparison is trustworthy because B002 made creator_principal
// context-derived and removed the client-supplied path, so the spoofable-creator
// defect is structurally impossible (§12.1).
package sessionauthz

import "context"

// Action is the session action vocabulary gated by the seam (§12.7). The
// `share` action was removed in the 2026-06-21 amendment (no visibility toggle).
type Action string

const (
	// ActionRead — read session metadata or transcript. Team-wide.
	ActionRead Action = "read"
	// ActionWrite — rename, link-to-incident, send-message (owner-mutate).
	ActionWrite Action = "write"
	// ActionSoftDelete — owner soft-delete to trash (owner-mutate).
	ActionSoftDelete Action = "soft_delete"
	// ActionRestore — restore from trash; owner OR admin (§12.5).
	ActionRestore Action = "restore"
	// ActionPurge — irreversible hard delete (admin-govern).
	ActionPurge Action = "purge"
	// ActionArchive — move to cold storage (admin-govern).
	ActionArchive Action = "archive"
	// ActionUnarchive — restore from archive (admin-govern).
	ActionUnarchive Action = "unarchive"
	// ActionConfigureRetention — edit the retention policy (admin-govern).
	ActionConfigureRetention Action = "configure_retention"
)

// Relationship is the principal's resolved relationship to a session (§12.7).
type Relationship string

const (
	// RelationshipNone — unauthenticated; no access.
	RelationshipNone Relationship = "none"
	// RelationshipTeamMember — authenticated, not owner, not admin. Read-only
	// on any session (read is the default, not a grant).
	RelationshipTeamMember Relationship = "team_member"
	// RelationshipOwner — creator_principal equals the calling principal.
	RelationshipOwner Relationship = "owner"
	// RelationshipAdmin — carries the D-0011 dynamic admin capability;
	// cross-tenant governance and mutation, audited at the route layer.
	RelationshipAdmin Relationship = "admin"
)

// Decision is the seam's result: allow/deny plus the resolved relationship, so
// handlers do not re-query ownership (§12.7).
type Decision struct {
	Allowed      bool
	Relationship Relationship
}

// SessionResolver resolves the owning principal of a session. Kept as a tiny
// local interface (rather than importing sessionmodel) so the seam stays
// decoupled from the session store's concrete types.
type SessionResolver interface {
	// SessionCreator returns the creator principal of sessionID and whether the
	// session exists. A non-existent session is (",", false, nil), not an error.
	SessionCreator(ctx context.Context, sessionID string) (creator string, found bool, err error)
}

// AdminChecker is the REUSED D-0011 dynamic admin capability check (§12.7
// "reuse the D-0011 check, do not reimplement"). rbac.Repository satisfies it
// structurally via IsAdmin; the seam never imports rbac.
type AdminChecker interface {
	IsAdmin(ctx context.Context, principal string) (bool, error)
}

// Seam is the single session-authorization enforcement point. Construct with
// New. Safe for concurrent use if its dependencies are.
type Seam struct {
	sessions SessionResolver
	admin    AdminChecker
}

// New builds the seam from its two dependencies: a session resolver (for
// ownership) and the reused dynamic admin checker (for governance).
func New(sessions SessionResolver, admin AdminChecker) *Seam {
	return &Seam{sessions: sessions, admin: admin}
}

type category int

const (
	catUnknown category = iota
	catRead
	catOwnerMutate
	catAdminGovern
)

func categorize(a Action) category {
	switch a {
	case ActionRead:
		return catRead
	case ActionWrite, ActionSoftDelete, ActionRestore:
		return catOwnerMutate
	case ActionPurge, ActionArchive, ActionUnarchive, ActionConfigureRetention:
		return catAdminGovern
	}
	return catUnknown
}

// SessionAccess is the §12.7 decision function. It resolves the principal's
// relationship to the session once, returns allow/deny plus that relationship,
// and never consults zones or RBAC policies. On a dependency failure (admin
// store or session store) it fails CLOSED: the returned Decision denies and the
// error is non-nil so a caller can surface a 500 distinct from a clean deny.
func (s *Seam) SessionAccess(ctx context.Context, principal, sessionID string, action Action) (Decision, error) {
	// Unauthenticated / nil principal denies regardless of action. The api
	// layer normalizes rbac.Unknown to "" before calling, so "" is the single
	// unauthenticated sentinel here.
	if principal == "" {
		return Decision{Allowed: false, Relationship: RelationshipNone}, nil
	}
	// Unrecognized action denies.
	cat := categorize(action)
	if cat == catUnknown {
		return Decision{Allowed: false, Relationship: RelationshipNone}, nil
	}

	rel, err := s.resolve(ctx, principal, sessionID)
	if err != nil {
		return Decision{Allowed: false, Relationship: RelationshipNone}, err
	}
	return Decision{Allowed: decide(rel, cat), Relationship: rel}, nil
}

// resolve derives the single relationship with admin precedence: an admin is
// admin even on a session they happen to own (so they retain governance over
// it); a non-admin creator is owner; any other authenticated principal is a
// team member. None is reserved for the unauthenticated early-return above.
func (s *Seam) resolve(ctx context.Context, principal, sessionID string) (Relationship, error) {
	isAdmin, err := s.admin.IsAdmin(ctx, principal)
	if err != nil {
		return RelationshipNone, err
	}
	if isAdmin {
		return RelationshipAdmin, nil
	}
	if sessionID != "" {
		creator, found, err := s.sessions.SessionCreator(ctx, sessionID)
		if err != nil {
			return RelationshipNone, err
		}
		if found && creator == principal {
			return RelationshipOwner, nil
		}
	}
	return RelationshipTeamMember, nil
}

func decide(rel Relationship, cat category) bool {
	switch cat {
	case catRead:
		// Any authenticated principal may read (team-wide read, §12.7).
		return rel == RelationshipOwner || rel == RelationshipTeamMember || rel == RelationshipAdmin
	case catOwnerMutate:
		// Owner, or admin (cross-tenant mutation / owner-admin restore).
		return rel == RelationshipOwner || rel == RelationshipAdmin
	case catAdminGovern:
		// Admin only.
		return rel == RelationshipAdmin
	}
	return false
}
