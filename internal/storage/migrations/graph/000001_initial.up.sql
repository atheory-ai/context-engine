CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL,
    applied_at  INTEGER NOT NULL
);

INSERT INTO schema_version (version, applied_at) VALUES (1, unixepoch() * 1000);

-- ============================================================
-- Nodes
-- Immutable after indexing (except activation via separate table).
-- Node ID: sha256(project_id:type:canonical_id)[:16] hex-encoded.
-- ============================================================

CREATE TABLE IF NOT EXISTS nodes (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    type            TEXT NOT NULL,
    label           TEXT NOT NULL,
    canonical_id    TEXT NOT NULL,
    source_class    TEXT NOT NULL DEFAULT 'structural',
    plugin_id       TEXT,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_nodes_project   ON nodes(project_id);
CREATE INDEX IF NOT EXISTS idx_nodes_type      ON nodes(type);
CREATE INDEX IF NOT EXISTS idx_nodes_canonical ON nodes(project_id, canonical_id);
CREATE INDEX IF NOT EXISTS idx_nodes_label     ON nodes(label);
CREATE INDEX IF NOT EXISTS idx_nodes_source_class ON nodes(source_class);
CREATE INDEX IF NOT EXISTS idx_nodes_plugin    ON nodes(plugin_id);

-- ============================================================
-- Node Activation
-- Separated from node rows — write buffer primary target.
-- High-frequency writes during cognitive loop.
-- ============================================================

CREATE TABLE IF NOT EXISTS node_activation (
    node_id         TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    activation      REAL NOT NULL DEFAULT 0.0,
    peak_activation REAL NOT NULL DEFAULT 0.0,
    updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_node_activation_level
    ON node_activation(activation DESC);

-- ============================================================
-- Edges
-- Immutable after indexing (except weight via separate table).
-- Edge ID: sha256(source_id:type:target_id)[:16] hex-encoded.
-- ============================================================

CREATE TABLE IF NOT EXISTS edges (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    source_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,
    source_class    TEXT NOT NULL DEFAULT 'structural',
    plugin_id       TEXT,
    created_at      INTEGER NOT NULL,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_edges_project     ON edges(project_id);
CREATE INDEX IF NOT EXISTS idx_edges_source      ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target      ON edges(target_id);
CREATE INDEX IF NOT EXISTS idx_edges_type        ON edges(type);
CREATE INDEX IF NOT EXISTS idx_edges_source_class ON edges(source_class);
CREATE INDEX IF NOT EXISTS idx_edges_source_type ON edges(source_id, type);
CREATE INDEX IF NOT EXISTS idx_edges_target_type ON edges(target_id, type);

-- ============================================================
-- Edge Weight
-- Separated from edge rows — write buffer primary target.
-- Updated by Hebbian learning during cognitive loops.
-- ============================================================

CREATE TABLE IF NOT EXISTS edge_weight (
    edge_id             TEXT PRIMARY KEY REFERENCES edges(id) ON DELETE CASCADE,
    weight              REAL    NOT NULL DEFAULT 1.0,
    source_class        TEXT    NOT NULL DEFAULT 'structural',
    co_activation_count INTEGER NOT NULL DEFAULT 0,
    last_co_activation  INTEGER,
    updated_at          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_edge_weight_weight
    ON edge_weight(weight DESC);
CREATE INDEX IF NOT EXISTS idx_edge_weight_source_class
    ON edge_weight(source_class);

-- ============================================================
-- Concept Seeds
-- Org-level and project-level ontology entries.
-- ============================================================

CREATE TABLE IF NOT EXISTS concept_seeds (
    id          TEXT PRIMARY KEY,
    term        TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'project',
    definition  TEXT,
    related     TEXT NOT NULL DEFAULT '[]',
    synonyms    TEXT NOT NULL DEFAULT '[]',
    source      TEXT NOT NULL DEFAULT 'manual',
    plugin_id   TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_concept_seeds_term
    ON concept_seeds(scope, term);
CREATE INDEX IF NOT EXISTS idx_concept_seeds_source
    ON concept_seeds(source);

-- ============================================================
-- Index Runs
-- Record of each indexing pass for a project.
-- ============================================================

CREATE TABLE IF NOT EXISTS index_runs (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    plugin_ids      TEXT NOT NULL DEFAULT '[]',
    started_at      INTEGER NOT NULL,
    completed_at    INTEGER,
    status          TEXT NOT NULL DEFAULT 'running',
    nodes_created   INTEGER NOT NULL DEFAULT 0,
    nodes_updated   INTEGER NOT NULL DEFAULT 0,
    edges_created   INTEGER NOT NULL DEFAULT 0,
    files_processed INTEGER NOT NULL DEFAULT 0,
    error_message   TEXT,
    properties      TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_index_runs_project ON index_runs(project_id);
CREATE INDEX IF NOT EXISTS idx_index_runs_status  ON index_runs(status);
CREATE INDEX IF NOT EXISTS idx_index_runs_started ON index_runs(started_at);

-- ============================================================
-- Enrichments
-- Substrate changes made by the Reviewer during cognitive loops.
-- ============================================================

CREATE TABLE IF NOT EXISTS enrichments (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL,
    turn_id         TEXT NOT NULL,
    loop_index      INTEGER NOT NULL,
    entity_type     TEXT NOT NULL,
    entity_id       TEXT NOT NULL,
    action          TEXT NOT NULL,
    before_state    TEXT,
    after_state     TEXT NOT NULL,
    rationale       TEXT,
    created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_enrichments_run    ON enrichments(run_id);
CREATE INDEX IF NOT EXISTS idx_enrichments_entity ON enrichments(entity_id);
CREATE INDEX IF NOT EXISTS idx_enrichments_turn   ON enrichments(turn_id);
