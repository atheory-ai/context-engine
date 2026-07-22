ALTER TABLE index_runs DROP COLUMN extractor_fingerprint;

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
