CREATE TABLE IF NOT EXISTS file_hashes (
    project_id  TEXT    NOT NULL,
    rel_path    TEXT    NOT NULL,
    hash        TEXT    NOT NULL,
    indexed_at  INTEGER NOT NULL,
    PRIMARY KEY (project_id, rel_path)
);

CREATE INDEX IF NOT EXISTS idx_file_hashes_project
    ON file_hashes(project_id);
