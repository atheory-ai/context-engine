package queries

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

// PruneFileNodes removes the file-local nodes a file no longer produces, so
// incremental indexing doesn't leave stale symbols behind. It deletes nodes
// stamped with source_file == relPath whose id is NOT in keepIDs (the ids the
// current extraction re-emitted), skipping cross-file namespace and concept
// nodes — several files contribute those, and a full reindex reconciles them.
// IIR rows are deleted first (the iir table has no FK to nodes); edges, edge
// weights, and node activation cascade via foreign keys. Passing an empty
// keepIDs removes every file-local node for relPath — used when the file itself
// was deleted from disk. Returns the number of node rows removed.
func (q *IndexQueries) PruneFileNodes(ctx context.Context, projectID, relPath string, keepIDs []string) (int64, error) {
	// The doomed-node predicate, shared by both deletes.
	where := "project_id = ? AND source_file = ? AND type NOT IN ('namespace', 'concept')"
	args := []any{projectID, relPath}
	if len(keepIDs) > 0 {
		where += " AND id NOT IN (" + strings.Repeat("?,", len(keepIDs)-1) + "?)"
		for _, id := range keepIDs {
			args = append(args, id)
		}
	}

	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	// The two queries below are assembled only from string literals and bound "?"
	// placeholders (keepIDs are passed as args, never interpolated), so gosec's
	// SQL-string-concatenation warning (G202) is a false positive here.

	// 1. IIR has no FK cascade — delete rows for the doomed nodes explicitly.
	iirDelete := "DELETE FROM iir WHERE project_id = ? AND node_id IN (SELECT id FROM nodes WHERE " + where + ")" //nolint:gosec // G202: literals + bound placeholders only
	if _, err := tx.ExecContext(ctx, iirDelete, append([]any{projectID}, args...)...); err != nil {
		return 0, fmt.Errorf("prune iir for %s: %w", relPath, err)
	}

	// 2. Delete the nodes; edges, edge_weight and node_activation cascade.
	nodeDelete := "DELETE FROM nodes WHERE " + where //nolint:gosec // G202: literals + bound placeholders only
	res, err := tx.ExecContext(ctx, nodeDelete, args...)
	if err != nil {
		return 0, fmt.Errorf("prune nodes for %s: %w", relPath, err)
	}
	n, _ := res.RowsAffected() //nolint:errcheck // RowsAffected only errors on drivers that don't support it (not sqlite)
	return n, tx.Commit()
}
