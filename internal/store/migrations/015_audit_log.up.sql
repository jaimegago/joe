-- Identity Phase F: append-only audit log.
-- See docs/reference/joe-identity-design.md §2.6 (one append-only table, written at the
-- decision point) and §4 (failure posture: fail-CLOSED for mutations,
-- fail-OPEN for reads). Decision: docs/project/DECISIONS.md D-0009.
--
-- ONE table records every authorization decision the accessor makes (every
-- infra-adapter and graph-store allow/deny) PLUS the regime/captain
-- transition events that previously had no durable home. Transitions live
-- here instead of in the mutable system_regime / session_captains rows that
-- get cleared on resolve (bug #3: incident history erased on resolve).
--
-- Column rationale (kept minimal; columns are nullable where the kind does
-- not produce them):
--
--   id              opaque autoincrement, sole identity, no other column is
--                   unique (a principal+action+ts could legitimately repeat).
--                   INTEGER PRIMARY KEY AUTOINCREMENT matches rbac_policies
--                   (006) and review_jobs (007).
--   created_at      RFC3339 UTC string, matching every other timestamp column
--                   in the schema (RFC3339 TEXT — 009, 010, 011, 014).
--   principal       string per the prefix-typed principal model
--                   (docs/reference/joe-identity-design.md §2.2). NULL only for the
--                   coreagent-style transition rows that have no caller
--                   principal (declared by no-one) — should not arise for
--                   accessor-written rows.
--   action          rbac.Action string for infra rows (read|query|mutate|
--                   delete) or a transition verb for transition rows
--                   (declare_incident, resolve_incident, captain_attach,
--                   captain_transfer_begin, captain_transfer_confirm,
--                   captain_transfer_cancel).
--   zone            RBAC zone resolved for the decision. NULL when the kind
--                   has no zone (transition events that did not pass through
--                   HasZoneAccess). For accessor rows this is best-effort:
--                   the accessor records the source's resolved zone if
--                   available, else NULL.
--   source          infrastructure source id for adapter rows;
--                   GraphSourceID ('graph') for graph rows; NULL for
--                   sourceless rows (regime/captain transitions).
--   decision        'allow' or 'deny'. CHECK-enforced. Transition rows are
--                   always 'allow' (the row records that the transition
--                   happened); rejected transitions are recorded as 'deny'
--                   when audit is being written for the policy decision.
--   reason          free-text. For deny: the reason the decision function
--                   produced. For allow: a minimal reason/marker
--                   ('policy_allow', 'rbac_disabled', 'transition_recorded').
--   kind            discriminator. 'infra_access' for accessor decisions,
--                   'regime_transition' or 'captain_transition' for
--                   sourceless events. CHECK-enforced.
--   context         JSON blob carrying kind-specific specifics. For
--                   transitions: declared_kind, session_id, captain_id,
--                   transfer_initiator, etc. Empty JSON '{}' is the
--                   default. The accessor uses it for the rare extra
--                   discriminator (e.g. graph subkind).
--
-- Append-only is enforced TWO ways:
--   (1) Code: internal/audit exposes only an Insert path; there is no
--       Update or Delete method (asserted by audit_append_only_test.go).
--   (2) Database: the triggers below RAISE(ABORT,...) on any UPDATE or
--       DELETE against this table. The triggers fire on the audit_log
--       table only — every other table is unaffected.
--
-- Retention/rotation is deliberately out of scope for v1 (see Phase F
-- prompt scope fences). The table grows monotonically.

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL,
    principal  TEXT,
    action     TEXT NOT NULL,
    zone       TEXT,
    source     TEXT,
    decision   TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    kind       TEXT NOT NULL,
    context    TEXT NOT NULL DEFAULT '{}',
    CHECK (decision IN ('allow', 'deny')),
    CHECK (kind IN ('infra_access', 'regime_transition', 'captain_transition'))
);

CREATE INDEX idx_audit_log_created_at ON audit_log (created_at);
CREATE INDEX idx_audit_log_principal  ON audit_log (principal);
CREATE INDEX idx_audit_log_kind       ON audit_log (kind);

-- Database-level append-only guarantee. These triggers are the
-- belt-and-suspenders to the code-level insert-only repository in
-- internal/audit. They are intentionally redundant: any future caller that
-- bypasses the repository — or any operator with raw SQL access — still
-- cannot rewrite or erase audit history.

CREATE TRIGGER audit_log_no_update
BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only: UPDATE is not permitted');
END;

CREATE TRIGGER audit_log_no_delete
BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only: DELETE is not permitted');
END;
