-- Index output is written in bounded WAL batches before it is published.
-- These tables are deliberately not query-visible; ReconcileIndexRun promotes
-- a completed run into the live graph in its final transaction.
CREATE TABLE IF NOT EXISTS index_staging_nodes (
    run_id TEXT NOT NULL, id TEXT NOT NULL, project_id TEXT NOT NULL,
    type TEXT NOT NULL, label TEXT NOT NULL, canonical_id TEXT NOT NULL,
    source_class TEXT NOT NULL, plugin_id TEXT, source_file TEXT,
    index_managed INTEGER NOT NULL, last_index_run_id TEXT,
    created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, properties TEXT NOT NULL,
    PRIMARY KEY (run_id, id)
);
CREATE INDEX IF NOT EXISTS idx_index_staging_nodes_run ON index_staging_nodes(run_id, project_id);
CREATE TABLE IF NOT EXISTS index_staging_edges (
    run_id TEXT NOT NULL, id TEXT NOT NULL, project_id TEXT NOT NULL,
    source_id TEXT NOT NULL, target_id TEXT NOT NULL, type TEXT NOT NULL,
    source_class TEXT NOT NULL, plugin_id TEXT, index_managed INTEGER NOT NULL,
    last_index_run_id TEXT, created_at INTEGER NOT NULL, properties TEXT NOT NULL,
    PRIMARY KEY (run_id, id)
);
CREATE INDEX IF NOT EXISTS idx_index_staging_edges_run ON index_staging_edges(run_id, project_id);
