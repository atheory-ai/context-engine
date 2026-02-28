-- Org-specific tables for org.db only.
-- org.db also gets the graph/ migrations (nodes, edges, edge_weight, etc.)
-- via RunGraph(). These tables extend org.db with org-only constructs.

-- Org-level concept seeds — vocabulary shared across all projects.
-- Managed via `ce config org-concepts`.
CREATE TABLE IF NOT EXISTS org_concept_seeds (
    term        TEXT NOT NULL PRIMARY KEY,
    definition  TEXT,
    related     TEXT NOT NULL DEFAULT '[]',
    synonyms    TEXT NOT NULL DEFAULT '[]',
    source      TEXT NOT NULL DEFAULT 'manual',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- Cross-project relationship edges.
-- Links nodes from different projects that share dependencies or mirror interfaces.
-- Kept separate from the regular edges table to avoid polluting per-project edge queries.
CREATE TABLE IF NOT EXISTS cross_project_edges (
    id              TEXT NOT NULL PRIMARY KEY,
    source_node_id  TEXT NOT NULL,
    source_project  TEXT NOT NULL,
    target_node_id  TEXT NOT NULL,
    target_project  TEXT NOT NULL,
    type            TEXT NOT NULL,
    source_class    TEXT NOT NULL DEFAULT 'speculative',
    weight          REAL NOT NULL DEFAULT 0.3,
    properties      TEXT NOT NULL DEFAULT '{}',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cross_edges_source
    ON cross_project_edges(source_node_id, source_project);
CREATE INDEX IF NOT EXISTS idx_cross_edges_target
    ON cross_project_edges(target_node_id, target_project);
CREATE INDEX IF NOT EXISTS idx_cross_edges_type
    ON cross_project_edges(type);
