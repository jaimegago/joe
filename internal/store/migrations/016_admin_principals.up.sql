-- Identity Phase H: admin status as a dynamic capability.
-- See docs/reference/joe-identity-design.md §2.9 (admin bootstrap) and §5 Invariant 2.
-- Decision: docs/project/DECISIONS.md D-0011.
--
-- Phase C (D-0006) defined "admin authority" as a snapshot of grants — an
-- rbac_policies row on every security zone that existed AT BOOTSTRAP. That
-- is a day-100 correctness gap: a zone created AFTER bootstrap is not
-- covered, and the admin silently lacks access to it with no error
-- explaining why.
--
-- Phase H replaces that snapshot with a DYNAMIC admin capability evaluated
-- at the authorization decision point: a principal that is an admin is
-- allowed on any zone+action (subject to the zone's own allowed_actions
-- still being meaningful — see D-0011 for the stricter-interpretation
-- justification) without holding per-zone grant rows. A zone created after
-- designation is covered automatically.
--
-- This table is the single source of truth for admin status. The
-- authorization decision in rbac.PolicyEngine reads it. Its writer set is
-- closed and machine-checked: see
-- internal/rbac/admin_writers_guard_test.go, which enumerates the sanctioned
-- writers and fails on any call site outside them. The
-- previous bootstrap behaviour (writing a grant per zone in rbac_policies)
-- is removed in the same change; the AddAdmin code path also deletes any
-- leftover rbac_policies rows for the principal so admin authority has
-- exactly one source of truth (the static-structural assertion in
-- internal/auth/provision_test.go::TestPhaseH_NoLeftoverSnapshotGrants).
--
-- Column rationale:
--
--   principal  prefix-typed principal string (user:<email> or svc:<name>),
--              the same identifier rbac_policies.principal carries. Primary
--              key — a principal is either admin or not; there is no
--              additional discriminator.
--   granted_at RFC3339 UTC string, matching every other timestamp column in
--              the schema (rbac_policies.created_at, auth_sessions, audit_log).
--   granted_by who/what designated this admin. For the configured
--              admin_email bootstrap path (and the admin REST surface,
--              which shares its grant helper): 'bootstrap_admin_email'.
--              For the offline first-admin CLI (`joe admin bootstrap`):
--              'cli'. Operators reading the table can distinguish the
--              paths without parsing the reason field.
--   reason     free-text justification. The bootstrap path stores
--              'auth.admin_email match'; the CLI stores a fixed string
--              naming itself — it takes no --reason flag, because it can
--              run in exactly one circumstance (an empty admin roster)
--              and there is nothing an operator could add that the row
--              does not already imply. Kept TEXT and
--              defaulted to '' to match the audit_log.reason convention
--              from migration 015.

CREATE TABLE admin_principals (
    principal  TEXT PRIMARY KEY,
    granted_at TEXT NOT NULL,
    granted_by TEXT NOT NULL DEFAULT '',
    reason     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_admin_principals_granted_at ON admin_principals (granted_at);
