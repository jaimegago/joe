-- Phase 10: Code Review Integration — review job queue.
-- Jobs are idempotent by event_id (deduplication key).

CREATE TABLE review_jobs (
    id          TEXT PRIMARY KEY,
    -- Deduplication key: "<platform>:<owner>/<repo>#<pr_number>:<head_sha>"
    event_id    TEXT NOT NULL UNIQUE,
    platform    TEXT NOT NULL CHECK(platform IN ('github', 'gitlab')),
    source_id   TEXT NOT NULL,
    owner       TEXT NOT NULL,
    repo        TEXT NOT NULL,
    pr_number   INTEGER NOT NULL,
    head_sha    TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK(status IN ('pending','running','done','failed','skipped')),
    review_body TEXT,
    error       TEXT,
    created_at  TEXT NOT NULL,
    started_at  TEXT,
    finished_at TEXT
);

CREATE INDEX idx_review_jobs_status   ON review_jobs(status);
CREATE INDEX idx_review_jobs_event_id ON review_jobs(event_id);
CREATE INDEX idx_review_jobs_platform ON review_jobs(platform, owner, repo, pr_number);
