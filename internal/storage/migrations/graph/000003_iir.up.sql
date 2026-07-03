-- ============================================================
-- IIR — Intermediate Intent Representation per function node.
-- Semantic layer over the structural graph. One row per
-- (function node, kind): 'extracted' from source, or 'intended'
-- as declared/shaped. Verification is a join on node_id.
-- ============================================================

CREATE TABLE IF NOT EXISTS iir (
    id          TEXT    NOT NULL PRIMARY KEY,
    project_id  TEXT    NOT NULL,
    node_id     TEXT    NOT NULL,        -- the function symbol node
    kind        TEXT    NOT NULL,        -- extracted | intended
    language    TEXT    NOT NULL,
    iir         TEXT    NOT NULL,        -- FunctionIntent JSON
    source_hash TEXT,                    -- staleness vs file_hashes
    run_id      TEXT,                    -- index run that produced it
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    UNIQUE (project_id, node_id, kind)
);

CREATE INDEX IF NOT EXISTS idx_iir_node ON iir(project_id, node_id);
CREATE INDEX IF NOT EXISTS idx_iir_kind ON iir(project_id, kind);
