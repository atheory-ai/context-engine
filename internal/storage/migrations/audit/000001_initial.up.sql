CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

-- ============================================================
-- Sessions
-- Every interaction belongs to a session.
-- ============================================================

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    actor_id        TEXT NOT NULL,
    token_id        TEXT,
    surface         TEXT NOT NULL,
    started_at      INTEGER NOT NULL,
    last_active_at  INTEGER NOT NULL,
    ended_at        INTEGER,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_sessions_actor   ON sessions(actor_id);
CREATE INDEX IF NOT EXISTS idx_sessions_surface ON sessions(surface);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at);

-- ============================================================
-- Turns
-- One complete user interaction: input → final answer.
-- All engine actions within a turn share a turn_id.
-- ============================================================

CREATE TABLE IF NOT EXISTS turns (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    query       TEXT,
    started_at  INTEGER NOT NULL,
    ended_at    INTEGER,
    loop_count  INTEGER,
    status      TEXT NOT NULL DEFAULT 'active',
    properties  TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id);
CREATE INDEX IF NOT EXISTS idx_turns_started ON turns(started_at);
CREATE INDEX IF NOT EXISTS idx_turns_status  ON turns(status);

-- ============================================================
-- Audit Entries
-- Every engine action recorded. Append-only.
-- ============================================================

CREATE TABLE IF NOT EXISTS audit_entries (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    turn_id         TEXT,
    actor_id        TEXT NOT NULL,
    token_id        TEXT,
    on_behalf_of    TEXT,
    surface         TEXT NOT NULL,
    action          TEXT NOT NULL,
    project_ids     TEXT,
    scope           TEXT,
    status          TEXT NOT NULL DEFAULT 'ok',
    error_message   TEXT,
    timestamp       INTEGER NOT NULL,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_audit_session   ON audit_entries(session_id);
CREATE INDEX IF NOT EXISTS idx_audit_turn      ON audit_entries(turn_id);
CREATE INDEX IF NOT EXISTS idx_audit_actor     ON audit_entries(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_action    ON audit_entries(action);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_entries(timestamp);
