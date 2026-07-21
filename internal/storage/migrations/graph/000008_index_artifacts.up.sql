-- Durable state between the bounded parse and extraction stages. Source is
-- stored once per content hash; CST payloads must be compact offset-based
-- representations, never the text-expanded plugin JSON envelope.
CREATE TABLE index_source_artifacts (
    project_id TEXT NOT NULL, source_hash TEXT NOT NULL, content BLOB NOT NULL,
    byte_length INTEGER NOT NULL, created_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, source_hash)
);
CREATE TABLE index_parse_artifacts (
    project_id TEXT NOT NULL, source_hash TEXT NOT NULL, parser_version TEXT NOT NULL,
    language TEXT NOT NULL, cst BLOB NOT NULL, created_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, source_hash, parser_version, language),
    FOREIGN KEY (project_id, source_hash) REFERENCES index_source_artifacts(project_id, source_hash) ON DELETE CASCADE
);
CREATE TABLE index_staging_files (
    run_id TEXT NOT NULL, project_id TEXT NOT NULL, rel_path TEXT NOT NULL,
    source_hash TEXT NOT NULL, status TEXT NOT NULL,
    PRIMARY KEY (run_id, rel_path)
);
CREATE INDEX idx_index_staging_files_run ON index_staging_files(run_id, project_id, status);
CREATE TABLE index_staging_file_nodes (run_id TEXT NOT NULL, rel_path TEXT NOT NULL, node_id TEXT NOT NULL, PRIMARY KEY (run_id, rel_path, node_id));
CREATE TABLE index_staging_file_edges (run_id TEXT NOT NULL, rel_path TEXT NOT NULL, edge_id TEXT NOT NULL, PRIMARY KEY (run_id, rel_path, edge_id));
CREATE TABLE index_staging_file_iir (run_id TEXT NOT NULL, rel_path TEXT NOT NULL, iir_id TEXT NOT NULL, PRIMARY KEY (run_id, rel_path, iir_id));
