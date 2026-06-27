-- Phase 1 Change 3: Findings + Joe-warnings.
-- See the session-model design (Phase 0) §A4, §E1/§E2, §R3.
--
-- §6-C: every FK to agent_sessions(id) in this migration is ON DELETE
-- CASCADE. Rationale: incident-expunge per the session-model design (Phase 0) §5b-5.
-- joe_warnings.source_investigation_session_id is nullable; deletion of
-- its referenced session cascades the warning row away (the investigation
-- context is part of the warning's identity).

-- findings: §A4 cross-session attribution. A human posts an attributed,
-- non-actionable synthesis message into a target session's timeline,
-- optionally referencing the source investigation session that produced
-- it. Annotation semantic only — no workflow, no queue, no
-- accept/reject state.
CREATE TABLE findings (
    id                                  TEXT PRIMARY KEY,
    source_session_id                   TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    target_session_id                   TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    author_principal                    TEXT NOT NULL,
    body                                TEXT NOT NULL,
    posted_at                           TEXT NOT NULL,
    referenced_investigation_session_id TEXT REFERENCES agent_sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_findings_source ON findings (source_session_id);
CREATE INDEX idx_findings_target ON findings (target_session_id);
CREATE INDEX idx_findings_referenced ON findings (referenced_investigation_session_id);

-- joe_warnings: §E1/§R3 — Joe's append-only list of
-- incident-judgments-it-is-not-authorized-to-act-on. A human reads the
-- list and may choose to declare an incident; the warnings surface is
-- deliberately minimal (not a queue, not state-tracked, not self-
-- escalating, §E2 / R9). The append-only invariant is structurally
-- enforced by the warnings.Repository interface shape; see
-- internal/warnings/repository_test.go.
CREATE TABLE joe_warnings (
    id                              TEXT PRIMARY KEY,
    raised_at                       TEXT NOT NULL,
    signal_reference                TEXT NOT NULL,
    body                            TEXT NOT NULL,
    source_investigation_session_id TEXT REFERENCES agent_sessions(id) ON DELETE CASCADE,
    reviewed_at                     TEXT,
    reviewed_by_principal           TEXT
);

CREATE INDEX idx_joe_warnings_raised_at ON joe_warnings (raised_at);
CREATE INDEX idx_joe_warnings_source_session ON joe_warnings (source_investigation_session_id);
