CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

-- ============================================================
-- Project Registry
-- Primary key: git remote URL (normalized).
-- Filesystem path is not the identity.
-- ============================================================

CREATE TABLE IF NOT EXISTS projects (
    id              TEXT PRIMARY KEY,
    git_url         TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'unindexed',
    graph_path      TEXT,
    config_hash     TEXT,
    base_prompt     TEXT,
    arch_prompt     TEXT,
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER NOT NULL,
    last_indexed_at INTEGER,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_projects_git_url ON projects(git_url);
CREATE INDEX IF NOT EXISTS idx_projects_status  ON projects(status);

-- Known local filesystem paths for this project.
-- A project may exist at multiple paths (worktrees, multiple machines via sync).
CREATE TABLE IF NOT EXISTS project_paths (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    last_seen   INTEGER NOT NULL,
    PRIMARY KEY (project_id, path)
);

CREATE INDEX IF NOT EXISTS idx_project_paths_path ON project_paths(path);

-- ============================================================
-- Token Store
-- Local token management. No external auth service.
-- ============================================================

CREATE TABLE IF NOT EXISTS tokens (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER,
    last_used   INTEGER,
    revoked     INTEGER NOT NULL DEFAULT 0,
    revoked_at  INTEGER,
    properties  TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_tokens_scope   ON tokens(scope);
CREATE INDEX IF NOT EXISTS idx_tokens_revoked ON tokens(revoked);
