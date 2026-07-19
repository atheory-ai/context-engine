-- Index ownership makes a successful index run authoritative for the files it
-- processed.  The membership tables deliberately live beside the graph: they
-- are the only safe way to delete a file's old output when IDs change (for
-- example, after a symbol moves to a different source offset).

ALTER TABLE nodes ADD COLUMN index_managed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN last_index_run_id TEXT;
ALTER TABLE edges ADD COLUMN index_managed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE edges ADD COLUMN last_index_run_id TEXT;

-- Existing plugin output predates explicit ownership. Treat it as managed so
-- the next authoritative full reindex can replace it; rows without a plugin
-- remain user/agent-owned and are never swept by the indexer.
UPDATE nodes SET index_managed = 1 WHERE plugin_id IS NOT NULL;
UPDATE edges SET index_managed = 1 WHERE plugin_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_nodes_index_run ON nodes(project_id, index_managed, last_index_run_id);
CREATE INDEX IF NOT EXISTS idx_edges_index_run ON edges(project_id, index_managed, last_index_run_id);

CREATE TABLE IF NOT EXISTS index_file_nodes (
    project_id       TEXT NOT NULL,
    rel_path         TEXT NOT NULL,
    node_id          TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    last_seen_run_id TEXT NOT NULL,
    PRIMARY KEY (project_id, rel_path, node_id)
);
CREATE INDEX IF NOT EXISTS idx_index_file_nodes_path ON index_file_nodes(project_id, rel_path, last_seen_run_id);
CREATE INDEX IF NOT EXISTS idx_index_file_nodes_node ON index_file_nodes(node_id);

CREATE TABLE IF NOT EXISTS index_file_edges (
    project_id       TEXT NOT NULL,
    rel_path         TEXT NOT NULL,
    edge_id          TEXT NOT NULL REFERENCES edges(id) ON DELETE CASCADE,
    last_seen_run_id TEXT NOT NULL,
    PRIMARY KEY (project_id, rel_path, edge_id)
);
CREATE INDEX IF NOT EXISTS idx_index_file_edges_path ON index_file_edges(project_id, rel_path, last_seen_run_id);
CREATE INDEX IF NOT EXISTS idx_index_file_edges_edge ON index_file_edges(edge_id);

CREATE TABLE IF NOT EXISTS index_file_iir (
    project_id       TEXT NOT NULL,
    rel_path         TEXT NOT NULL,
    iir_id           TEXT NOT NULL REFERENCES iir(id) ON DELETE CASCADE,
    last_seen_run_id TEXT NOT NULL,
    PRIMARY KEY (project_id, rel_path, iir_id)
);
CREATE INDEX IF NOT EXISTS idx_index_file_iir_path ON index_file_iir(project_id, rel_path, last_seen_run_id);
CREATE INDEX IF NOT EXISTS idx_index_file_iir_iir ON index_file_iir(iir_id);
