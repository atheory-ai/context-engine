-- ============================================================
-- Track the source file each node was extracted from, so incremental
-- indexing can prune a changed/deleted file's stale symbols without a
-- full reindex. Empty ('') for nodes written before this migration and
-- for cross-file namespace/concept nodes (which incremental prune skips
-- by design); a full reindex backfills it.
-- ============================================================

ALTER TABLE nodes ADD COLUMN source_file TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_nodes_source_file ON nodes(project_id, source_file);
