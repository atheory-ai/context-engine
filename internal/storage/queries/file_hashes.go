package queries

import (
	"context"
	"database/sql"
	"time"
)

// IndexQueries wraps a graph DB for indexer-specific query operations.
type IndexQueries struct {
	db *sql.DB
}

// NewIndexQueries creates an IndexQueries for the given project graph DB.
func NewIndexQueries(db *sql.DB) *IndexQueries {
	return &IndexQueries{db: db}
}

// GetFileHashes returns a map of rel_path → content hash for all indexed
// files in the given project. Used by the incremental indexer.
func (q *IndexQueries) GetFileHashes(ctx context.Context, projectID string) (map[string]string, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT rel_path, hash FROM file_hashes WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hashes := make(map[string]string)
	for rows.Next() {
		var relPath, hash string
		if err := rows.Scan(&relPath, &hash); err != nil {
			return nil, err
		}
		hashes[relPath] = hash
	}
	return hashes, rows.Err()
}

// UpsertFileHash inserts or updates the content hash for a file.
func (q *IndexQueries) UpsertFileHash(ctx context.Context, projectID, relPath, hash string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO file_hashes (project_id, rel_path, hash, indexed_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(project_id, rel_path) DO UPDATE SET
		     hash       = excluded.hash,
		     indexed_at = excluded.indexed_at`,
		projectID, relPath, hash, time.Now().UnixMilli())
	return err
}

// DeleteFileHash removes the hash record for a file (used when a file is deleted).
func (q *IndexQueries) DeleteFileHash(ctx context.Context, projectID, relPath string) error {
	_, err := q.db.ExecContext(ctx,
		`DELETE FROM file_hashes WHERE project_id = ? AND rel_path = ?`,
		projectID, relPath)
	return err
}

// ClearFileHashes removes all hash records for a project (used before a full reindex).
func (q *IndexQueries) ClearFileHashes(ctx context.Context, projectID string) error {
	_, err := q.db.ExecContext(ctx,
		`DELETE FROM file_hashes WHERE project_id = ?`, projectID)
	return err
}
