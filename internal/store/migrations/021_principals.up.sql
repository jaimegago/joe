-- Identity Stage 1: the authoritative identity registry.
--
-- Until now there was NO users/principals/identities table anywhere in the
-- schema (verified in IDENTITY_MODEL_INVESTIGATION.md Step 1). The fact that a
-- user ever signed in was recoverable only as an append-only audit_log
-- 'auth_login' event; there was no MUTABLE per-user record. A user with zero
-- grants who logs in once and does nothing left no row in any identity table.
--
-- This table is that missing registry. It is the single authoritative list of
-- principals the system knows about, with the per-user lifecycle and mutable
-- attributes the audit event stream cannot carry: a status (active|disabled)
-- the per-request gate can consult, the disable provenance (when/by-whom), and
-- mutable display metadata (display_name, last_seen_at) a Users page renders.
--
-- This migration only creates the table. It is NOT populated here:
-- provisioning hooks (the OIDC callback upserting a row on login, and a
-- backfill of the existing audit_log 'auth_login' principals) land in Stage 2.
-- A freshly-migrated system therefore starts with an empty registry, which is
-- correct — no principal is asserted to exist until it is provisioned.
--
-- Column rationale:
--
--   principal    prefix-typed principal string (user:<email> or svc:<name>),
--                the same identifier rbac_policies.principal and
--                admin_principals.principal carry. Primary key — a principal
--                appears at most once in the registry.
--   created_at   RFC3339 UTC string, matching every other timestamp column in
--                the schema. First time the registry learned of this principal.
--   status       lifecycle state. CHECK-constrained to the two states this
--                stage needs ('active', 'disabled'); defaults to 'active' so a
--                provisioned-on-login principal is usable without an extra
--                write. A future suspended/pending state would widen the CHECK
--                via a later migration, mirroring how 020 widened audit_log.kind.
--   disabled_at  RFC3339 UTC string, NULL while active. Stamped when status
--                moves to 'disabled', cleared (back to NULL) on re-enable.
--   disabled_by  the acting principal that disabled this one, NULL while
--                active. Cleared on re-enable. Together with disabled_at this
--                is the disable provenance the audit row also records, kept on
--                the row itself for a cheap point read.
--   display_name mutable human label, NULL until a provisioning hook learns it
--                (e.g. from an OIDC name claim). Per-user metadata, not identity.
--   last_seen_at RFC3339 UTC string, NULL until first seen by a provisioning
--                hook. Mutable; advanced on each login in Stage 2.

CREATE TABLE principals (
    principal    TEXT PRIMARY KEY,
    created_at   TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    disabled_at  TEXT,
    disabled_by  TEXT,
    display_name TEXT,
    last_seen_at TEXT
);

-- The Users-page list query and the per-request status check both filter on
-- status (e.g. "show disabled principals", "is this caller disabled?"). The
-- primary key already covers point lookups by principal id.
CREATE INDEX idx_principals_status ON principals (status);
