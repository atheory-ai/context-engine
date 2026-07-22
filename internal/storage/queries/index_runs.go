package queries

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// FileOutput is the complete, successfully-produced output for one source
// file. Empty slices are meaningful: they replace a file's prior output.
type FileOutput struct {
	Hash    string
	NodeIDs []string
	EdgeIDs []string
	IIRIDs  []string
}

// StagedFileEvent records one discovery or completed file contribution. A nil
// Output is a discovery-only event; a non-nil Output replaces that file's
// staged ownership. Indexing sends these events to a single bounded batcher so
// SQLite sees one transaction for many files instead of one transaction per
// discovered or extracted file.
type StagedFileEvent struct {
	Path    string
	Output  *FileOutput
	Deleted bool
}

// StageWalked records discovery durably. It is deliberately tiny so the
// walker can be ahead of extraction without retaining a corpus-sized path map.
func (q *IndexQueries) StageWalked(ctx context.Context, runID, projectID, path string) error {
	return q.StageFileEvents(ctx, runID, projectID, []StagedFileEvent{{Path: path}})
}

// StageDeleted records a path which was known to the requested indexing scope
// but has disappeared from disk. Reconciliation removes only that file's
// derived contribution and hash.
func (q *IndexQueries) StageDeleted(ctx context.Context, runID, projectID, path string) error {
	return q.StageFileEvents(ctx, runID, projectID, []StagedFileEvent{{Path: path, Deleted: true}})
}

// StageFileOutput persists a complete accepted file contribution. Its graph
// facts are still written by the write buffer; this table is the durable input
// to final ownership reconciliation.
func (q *IndexQueries) StageFileOutput(ctx context.Context, runID, projectID, path string, out FileOutput) error {
	return q.StageFileEvents(ctx, runID, projectID, []StagedFileEvent{{Path: path, Output: &out}})
}

// StageFileEvents persists a bounded batch of file-discovery and file-output
// events atomically. It is intentionally separate from graph writes: graph
// writes still go through the substrate write buffer, while these rows provide
// the durable manifest used to publish an all-or-nothing index run.
func (q *IndexQueries) StageFileEvents(ctx context.Context, runID, projectID string, events []StagedFileEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, event := range events {
		if event.Path == "" {
			return fmt.Errorf("stage file event: empty path")
		}
		if event.Deleted && event.Output != nil {
			return fmt.Errorf("stage file event: deleted path %s has output", event.Path)
		}
		if event.Deleted {
			if _, err = tx.ExecContext(ctx, `INSERT INTO index_staging_files (run_id, project_id, rel_path, source_hash, status) VALUES (?, ?, ?, '', 'deleted') ON CONFLICT(run_id, rel_path) DO UPDATE SET source_hash='',status='deleted'`, runID, projectID, event.Path); err != nil {
				return err
			}
			continue
		}
		if event.Output == nil {
			if _, err = tx.ExecContext(ctx, `INSERT INTO index_staging_files (run_id, project_id, rel_path, source_hash, status) VALUES (?, ?, ?, '', 'walked') ON CONFLICT(run_id, rel_path) DO NOTHING`, runID, projectID, event.Path); err != nil {
				return err
			}
			continue
		}
		if err := stageFileOutputTx(ctx, tx, runID, projectID, event.Path, *event.Output); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func stageFileOutputTx(ctx context.Context, tx *sql.Tx, runID, projectID, path string, out FileOutput) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO index_staging_files (run_id, project_id, rel_path, source_hash, status) VALUES (?, ?, ?, ?, 'indexed') ON CONFLICT(run_id, rel_path) DO UPDATE SET source_hash=excluded.source_hash,status='indexed'`, runID, projectID, path, out.Hash); err != nil {
		return err
	}
	for _, entry := range []struct {
		ids         []string
		deleteQuery string
		insertQuery string
	}{
		{out.NodeIDs, `DELETE FROM index_staging_file_nodes WHERE run_id=? AND rel_path=?`, `INSERT INTO index_staging_file_nodes (run_id,rel_path,node_id) VALUES (?,?,?)`},
		{out.EdgeIDs, `DELETE FROM index_staging_file_edges WHERE run_id=? AND rel_path=?`, `INSERT INTO index_staging_file_edges (run_id,rel_path,edge_id) VALUES (?,?,?)`},
		{out.IIRIDs, `DELETE FROM index_staging_file_iir WHERE run_id=? AND rel_path=?`, `INSERT INTO index_staging_file_iir (run_id,rel_path,iir_id) VALUES (?,?,?)`},
	} {
		if _, err := tx.ExecContext(ctx, entry.deleteQuery, runID, path); err != nil {
			return err
		}
		for _, id := range uniqueStrings(entry.ids) {
			if _, err := tx.ExecContext(ctx, entry.insertQuery, runID, path, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// StartIndexRun records an attempt before it can write graph data. A failed
// run is retained for diagnosis and never advances file hashes or ownership.
func (q *IndexQueries) StartIndexRun(ctx context.Context, id, projectID string, pluginIDs []string, extractorFingerprint string, startedAt int64) error {
	pluginsJSON, err := json.Marshal(pluginIDs)
	if err != nil {
		return fmt.Errorf("marshal plugin ids: %w", err)
	}
	_, err = q.db.ExecContext(ctx, `INSERT INTO index_runs (id, project_id, plugin_ids, extractor_fingerprint, started_at, status) VALUES (?, ?, ?, ?, ?, 'running')`, id, projectID, string(pluginsJSON), extractorFingerprint, startedAt)
	if err != nil {
		return fmt.Errorf("start index run: %w", err)
	}
	return nil
}

// LatestCompletedExtractorFingerprint returns the inputs that produced the
// current derived graph. An empty result means the project must be reindexed.
func (q *IndexQueries) LatestCompletedExtractorFingerprint(ctx context.Context, projectID string) (string, error) {
	var fingerprint string
	err := q.db.QueryRowContext(ctx, `SELECT extractor_fingerprint FROM index_runs WHERE project_id=? AND status='completed' ORDER BY completed_at DESC LIMIT 1`, projectID).Scan(&fingerprint)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return fingerprint, err
}

// FailIndexRun records that this run did not become authoritative.
func (q *IndexQueries) FailIndexRun(ctx context.Context, id string, completedAt int64, runErr error) error {
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failed index cleanup: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	for _, entry := range []struct {
		name  string
		query string
	}{
		{"staged file IIR", `DELETE FROM index_staging_file_iir WHERE run_id = ?`},
		{"staged file edges", `DELETE FROM index_staging_file_edges WHERE run_id = ?`},
		{"staged file nodes", `DELETE FROM index_staging_file_nodes WHERE run_id = ?`},
		{"staged files", `DELETE FROM index_staging_files WHERE run_id = ?`},
	} {
		if _, err := tx.ExecContext(ctx, entry.query, id); err != nil {
			return fmt.Errorf("clear failed %s: %w", entry.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM index_staging_edges WHERE run_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM index_staging_nodes WHERE run_id = ?`, id); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE index_runs SET status = 'failed', completed_at = ?, error_message = ? WHERE id = ?`, completedAt, message, id)
	if err != nil {
		return fmt.Errorf("fail index run: %w", err)
	}
	return tx.Commit()
}

// ReconcileIndexRun is the commit point for an index. It replaces ownership
// for the files this run processed, removes orphaned prior output, reconciles
// hashes, and marks the run complete in one SQLite transaction.
func (q *IndexQueries) ReconcileIndexRun(ctx context.Context, projectID, runID string, outputs map[string]FileOutput, walked map[string]struct{}, full bool, filesProcessed, nodesWritten, edgesWritten int, completedAt int64) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin index reconciliation: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	if err := promoteStagedIndexOutput(ctx, tx, projectID, runID); err != nil {
		return err
	}

	paths := make(map[string]struct{}, len(outputs))
	for path, out := range outputs {
		paths[path] = struct{}{}
		if err := upsertMemberships(ctx, tx, projectID, path, runID, out); err != nil {
			return err
		}
	}
	if !full {
		rows, err := tx.QueryContext(ctx, `SELECT rel_path FROM file_hashes WHERE project_id = ?`, projectID)
		if err != nil {
			return fmt.Errorf("list indexed paths: %w", err)
		}
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				rows.Close()
				return err
			}
			if _, seen := walked[path]; !seen {
				paths[path] = struct{}{}
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}

	if err := reconcileMemberships(ctx, tx, projectID, runID, mapKeys(paths), full); err != nil {
		return err
	}
	if full {
		// Membership was introduced after earlier indexers had already written
		// data. This sweep makes the first successful full run authoritative.
		if err := deleteLegacyManaged(ctx, tx, projectID, runID); err != nil {
			return err
		}
	}

	if full {
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_hashes WHERE project_id = ?`, projectID); err != nil {
			return fmt.Errorf("clear file hashes: %w", err)
		}
	}
	for path, out := range outputs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO file_hashes (project_id, rel_path, hash, indexed_at) VALUES (?, ?, ?, ?) ON CONFLICT(project_id, rel_path) DO UPDATE SET hash = excluded.hash, indexed_at = excluded.indexed_at`, projectID, path, out.Hash, completedAt); err != nil {
			return fmt.Errorf("upsert hash %s: %w", path, err)
		}
	}
	if !full {
		for path := range paths {
			if _, ok := outputs[path]; ok {
				continue
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM file_hashes WHERE project_id = ? AND rel_path = ?`, projectID, path); err != nil {
				return fmt.Errorf("delete hash %s: %w", path, err)
			}
		}
	}

	_, err = tx.ExecContext(ctx, `UPDATE index_runs SET status = 'completed', completed_at = ?, files_processed = ?, nodes_created = ?, edges_created = ? WHERE id = ?`, completedAt, filesProcessed, nodesWritten, edgesWritten, runID)
	if err != nil {
		return fmt.Errorf("complete index run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM index_staging_edges WHERE run_id = ?`, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM index_staging_nodes WHERE run_id = ?`, runID); err != nil {
		return err
	}
	for _, entry := range []struct {
		name  string
		query string
	}{
		{"staged file IIR", `DELETE FROM index_staging_file_iir WHERE run_id = ?`},
		{"staged file edges", `DELETE FROM index_staging_file_edges WHERE run_id = ?`},
		{"staged file nodes", `DELETE FROM index_staging_file_nodes WHERE run_id = ?`},
		{"staged files", `DELETE FROM index_staging_files WHERE run_id = ?`},
	} {
		if _, err := tx.ExecContext(ctx, entry.query, runID); err != nil {
			return fmt.Errorf("clear completed %s: %w", entry.name, err)
		}
	}
	return tx.Commit()
}

// ReconcileStagedIndexRun is the bounded-memory publication path. File
// membership and hashes have already been persisted by workers, so this never
// reconstructs a Go map of the corpus at the end of a run.
func (q *IndexQueries) ReconcileStagedIndexRun(ctx context.Context, projectID, runID string, full bool, filesProcessed, nodesWritten, edgesWritten int, completedAt int64) error {
	return q.reconcileStagedIndexRun(ctx, projectID, runID, full, false, filesProcessed, nodesWritten, edgesWritten, completedAt)
}

// ReconcileStagedIndexRunPaths publishes a path-scoped index run. Unlike a
// normal incremental directory walk, absence from this run does not mean a
// path was deleted: only explicitly staged indexed/deleted paths are replaced.
func (q *IndexQueries) ReconcileStagedIndexRunPaths(ctx context.Context, projectID, runID string, filesProcessed, nodesWritten, edgesWritten int, completedAt int64) error {
	return q.reconcileStagedIndexRun(ctx, projectID, runID, false, true, filesProcessed, nodesWritten, edgesWritten, completedAt)
}

func (q *IndexQueries) reconcileStagedIndexRun(ctx context.Context, projectID, runID string, full, pathScoped bool, filesProcessed, nodesWritten, edgesWritten int, completedAt int64) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin index reconciliation: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	if err := promoteStagedIndexOutput(ctx, tx, projectID, runID); err != nil {
		return err
	}
	for _, e := range []struct {
		name  string
		query string
	}{
		{"nodes", `INSERT INTO index_file_nodes (project_id,rel_path,node_id,last_seen_run_id) SELECT f.project_id,s.rel_path,s.node_id,s.run_id FROM index_staging_file_nodes s JOIN index_staging_files f ON f.run_id=s.run_id AND f.rel_path=s.rel_path WHERE s.run_id=? AND f.project_id=? AND f.status='indexed' ON CONFLICT(project_id,rel_path,node_id) DO UPDATE SET last_seen_run_id=excluded.last_seen_run_id`},
		{"edges", `INSERT INTO index_file_edges (project_id,rel_path,edge_id,last_seen_run_id) SELECT f.project_id,s.rel_path,s.edge_id,s.run_id FROM index_staging_file_edges s JOIN index_staging_files f ON f.run_id=s.run_id AND f.rel_path=s.rel_path WHERE s.run_id=? AND f.project_id=? AND f.status='indexed' ON CONFLICT(project_id,rel_path,edge_id) DO UPDATE SET last_seen_run_id=excluded.last_seen_run_id`},
		{"IIR", `INSERT INTO index_file_iir (project_id,rel_path,iir_id,last_seen_run_id) SELECT f.project_id,s.rel_path,s.iir_id,s.run_id FROM index_staging_file_iir s JOIN index_staging_files f ON f.run_id=s.run_id AND f.rel_path=s.rel_path WHERE s.run_id=? AND f.project_id=? AND f.status='indexed' ON CONFLICT(project_id,rel_path,iir_id) DO UPDATE SET last_seen_run_id=excluded.last_seen_run_id`},
	} {
		if _, err := tx.ExecContext(ctx, e.query, runID, projectID); err != nil {
			return fmt.Errorf("stage memberships %s: %w", e.name, err)
		}
	}
	if err := reconcileStagedMemberships(ctx, tx, projectID, runID, full, pathScoped); err != nil {
		return err
	}
	if full {
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_hashes WHERE project_id=?`, projectID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO file_hashes (project_id,rel_path,hash,indexed_at) SELECT project_id,rel_path,source_hash,? FROM index_staging_files WHERE run_id=? AND project_id=? AND status='indexed' ON CONFLICT(project_id,rel_path) DO UPDATE SET hash=excluded.hash,indexed_at=excluded.indexed_at`, completedAt, runID, projectID); err != nil {
		return err
	}
	if pathScoped {
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_hashes WHERE project_id=? AND rel_path IN (SELECT rel_path FROM index_staging_files WHERE run_id=? AND project_id=? AND status='deleted')`, projectID, runID, projectID); err != nil {
			return err
		}
	} else if !full {
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_hashes WHERE project_id=? AND rel_path NOT IN (SELECT rel_path FROM index_staging_files WHERE run_id=? AND project_id=?)`, projectID, runID, projectID); err != nil {
			return err
		}
	}
	if full {
		if err := deleteLegacyManaged(ctx, tx, projectID, runID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE index_runs SET status='completed',completed_at=?,files_processed=?,nodes_created=?,edges_created=? WHERE id=?`, completedAt, filesProcessed, nodesWritten, edgesWritten, runID); err != nil {
		return err
	}
	for _, query := range []string{
		`DELETE FROM index_staging_edges WHERE run_id=?`,
		`DELETE FROM index_staging_nodes WHERE run_id=?`,
		`DELETE FROM index_staging_file_iir WHERE run_id=?`,
		`DELETE FROM index_staging_file_edges WHERE run_id=?`,
		`DELETE FROM index_staging_file_nodes WHERE run_id=?`,
		`DELETE FROM index_staging_files WHERE run_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, query, runID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func promoteStagedIndexOutput(ctx context.Context, tx *sql.Tx, projectID, runID string) error {
	// Promotion must remain in ReconcileIndexRun's transaction so a completed
	// index run becomes query-visible as one atomic graph replacement. Copying
	// inside SQLite avoids N+1 writes and preserves nullable provenance fields.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO nodes (id, project_id, type, label, canonical_id, source_class, plugin_id, source_file, index_managed, last_index_run_id, created_at, updated_at, properties)
		SELECT id, project_id, type, label, canonical_id, source_class, plugin_id, COALESCE(source_file, ''), index_managed, last_index_run_id, created_at, updated_at, properties
		FROM index_staging_nodes
		WHERE run_id = ? AND project_id = ?
		ON CONFLICT(id) DO UPDATE SET
			label = excluded.label,
			source_class = excluded.source_class,
			plugin_id = excluded.plugin_id,
			source_file = excluded.source_file,
			index_managed = excluded.index_managed,
			last_index_run_id = excluded.last_index_run_id,
			updated_at = excluded.updated_at,
			properties = excluded.properties
	`, runID, projectID); err != nil {
		return fmt.Errorf("promote staged nodes: %w", err)
	}
	if err := validateStagedEdgeEndpoints(ctx, tx, projectID, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO edges (id, project_id, source_id, target_id, type, source_class, plugin_id, index_managed, last_index_run_id, created_at, properties)
		SELECT id, project_id, source_id, target_id, type, source_class, plugin_id, index_managed, last_index_run_id, created_at, properties
		FROM index_staging_edges
		WHERE run_id = ? AND project_id = ?
		ON CONFLICT(id) DO UPDATE SET
			source_class = excluded.source_class,
			plugin_id = excluded.plugin_id,
			index_managed = excluded.index_managed,
			last_index_run_id = excluded.last_index_run_id,
			properties = excluded.properties
	`, runID, projectID); err != nil {
		return fmt.Errorf("promote staged edges: %w", err)
	}
	return nil
}

// validateStagedEdgeEndpoints makes the atomic publication failure actionable.
// Edges may point to a node emitted by another file in the same run, so both
// staging and the already-published graph are valid endpoint sources. Any
// other reference is a broken contribution and must never reach the live
// foreign-key constrained edges table.
func validateStagedEdgeEndpoints(ctx context.Context, tx *sql.Tx, projectID, runID string) error {
	const query = `
		SELECT e.id, e.plugin_id, e.source_id, e.target_id,
			CASE WHEN staged_source.id IS NULL AND live_source.id IS NULL THEN 1 ELSE 0 END,
			CASE WHEN staged_target.id IS NULL AND live_target.id IS NULL THEN 1 ELSE 0 END
		FROM index_staging_edges e
		LEFT JOIN index_staging_nodes staged_source
			ON staged_source.run_id=e.run_id AND staged_source.project_id=e.project_id AND staged_source.id=e.source_id
		LEFT JOIN nodes live_source ON live_source.project_id=e.project_id AND live_source.id=e.source_id
		LEFT JOIN index_staging_nodes staged_target
			ON staged_target.run_id=e.run_id AND staged_target.project_id=e.project_id AND staged_target.id=e.target_id
		LEFT JOIN nodes live_target ON live_target.project_id=e.project_id AND live_target.id=e.target_id
		WHERE e.run_id=? AND e.project_id=?
		  AND ((staged_source.id IS NULL AND live_source.id IS NULL)
		    OR (staged_target.id IS NULL AND live_target.id IS NULL))
		ORDER BY e.id
		LIMIT 1`
	var edgeID, pluginID, sourceID, targetID string
	var missingSource, missingTarget bool
	err := tx.QueryRowContext(ctx, query, runID, projectID).Scan(&edgeID, &pluginID, &sourceID, &targetID, &missingSource, &missingTarget)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("validate staged edge endpoints: %w", err)
	}
	missing := make([]string, 0, 2)
	if missingSource {
		missing = append(missing, "source="+sourceID)
	}
	if missingTarget {
		missing = append(missing, "target="+targetID)
	}
	return fmt.Errorf("staged edge %s from plugin %q has missing endpoint(s): %s", edgeID, pluginID, strings.Join(missing, ", "))
}

func upsertMemberships(ctx context.Context, tx *sql.Tx, projectID, path, runID string, out FileOutput) error {
	for _, tableAndIDs := range []struct {
		table string
		ids   []string
		query string
	}{
		{"index_file_nodes", out.NodeIDs, `INSERT INTO index_file_nodes (project_id, rel_path, node_id, last_seen_run_id) VALUES (?, ?, ?, ?) ON CONFLICT(project_id, rel_path, node_id) DO UPDATE SET last_seen_run_id = excluded.last_seen_run_id`},
		{"index_file_edges", out.EdgeIDs, `INSERT INTO index_file_edges (project_id, rel_path, edge_id, last_seen_run_id) VALUES (?, ?, ?, ?) ON CONFLICT(project_id, rel_path, edge_id) DO UPDATE SET last_seen_run_id = excluded.last_seen_run_id`},
		{"index_file_iir", out.IIRIDs, `INSERT INTO index_file_iir (project_id, rel_path, iir_id, last_seen_run_id) VALUES (?, ?, ?, ?) ON CONFLICT(project_id, rel_path, iir_id) DO UPDATE SET last_seen_run_id = excluded.last_seen_run_id`},
	} {
		for _, id := range uniqueStrings(tableAndIDs.ids) {
			if _, err := tx.ExecContext(ctx, tableAndIDs.query, projectID, path, id, runID); err != nil {
				return fmt.Errorf("record %s ownership for %s: %w", tableAndIDs.table, path, err)
			}
		}
	}
	return nil
}

func reconcileMemberships(ctx context.Context, tx *sql.Tx, projectID, runID string, paths []string, full bool) error {
	for _, entry := range []struct{ table, column, entity string }{{"index_file_iir", "iir_id", "iir"}, {"index_file_edges", "edge_id", "edges"}, {"index_file_nodes", "node_id", "nodes"}} {
		ids, err := staleMembershipIDs(ctx, tx, entry.table, entry.column, projectID, runID, paths, full)
		if err != nil {
			return err
		}
		if err := deleteStaleMemberships(ctx, tx, entry.table, projectID, runID, paths, full); err != nil {
			return err
		}
		for _, id := range ids {
			var owned int
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = ?`, entry.table, entry.column), id).Scan(&owned); err != nil {
				return err
			}
			if owned != 0 {
				continue
			}
			if entry.entity == "nodes" {
				if _, err := tx.ExecContext(ctx, `UPDATE semantic_artifacts SET stale_at = ?, unit_node_id = NULL WHERE unit_node_id = ? AND stale_at IS NULL`, time.Now().UnixMilli(), id); err != nil {
					return fmt.Errorf("mark node artifact stale: %w", err)
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM iir WHERE node_id = ? AND kind = 'extracted'`, id); err != nil {
					return fmt.Errorf("delete node iir: %w", err)
				}
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, entry.entity), id); err != nil {
				return fmt.Errorf("delete stale %s: %w", entry.entity, err)
			}
		}
	}
	return nil
}

func reconcileStagedMemberships(ctx context.Context, tx *sql.Tx, projectID, runID string, full, pathScoped bool) error {
	for _, entry := range []struct{ table, column, entity string }{{"index_file_iir", "iir_id", "iir"}, {"index_file_edges", "edge_id", "edges"}, {"index_file_nodes", "node_id", "nodes"}} {
		where, args := stagedMembershipScope(projectID, runID, full, pathScoped)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT DISTINCT %s FROM %s WHERE %s`, entry.column, entry.table, where), args...)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s`, entry.table, where), args...); err != nil {
			return err
		}
		for _, id := range ids {
			var owned int
			if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s=?`, entry.table, entry.column), id).Scan(&owned); err != nil {
				return err
			}
			if owned != 0 {
				continue
			}
			if entry.entity == "nodes" {
				if _, err := tx.ExecContext(ctx, `DELETE FROM iir WHERE node_id=? AND kind='extracted'`, id); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id=?`, entry.entity), id); err != nil {
				return err
			}
		}
	}
	return nil
}

func stagedMembershipScope(projectID, runID string, full, pathScoped bool) (string, []any) {
	if full {
		return "project_id=? AND last_seen_run_id<>?", []any{projectID, runID}
	}
	if pathScoped {
		return `project_id=? AND rel_path IN (SELECT rel_path FROM index_staging_files WHERE run_id=? AND project_id=? AND status IN ('indexed','deleted')) AND last_seen_run_id<>?`, []any{projectID, runID, projectID, runID}
	}
	// A path is in scope when it was walked this run, or when a formerly indexed
	// path vanished from the walk and must have its old contribution removed.
	return `project_id=? AND (rel_path IN (SELECT rel_path FROM index_staging_files WHERE run_id=? AND project_id=? AND status='indexed') OR rel_path IN (SELECT h.rel_path FROM file_hashes h WHERE h.project_id=? AND NOT EXISTS (SELECT 1 FROM index_staging_files s WHERE s.run_id=? AND s.project_id=? AND s.rel_path=h.rel_path))) AND last_seen_run_id<>?`, []any{projectID, runID, projectID, projectID, runID, projectID, runID}
}

func staleMembershipIDs(ctx context.Context, tx *sql.Tx, table, column, projectID, runID string, paths []string, full bool) ([]string, error) {
	where, args := membershipScope(projectID, runID, paths, full)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT DISTINCT %s FROM %s WHERE %s`, column, table, where), args...)
	if err != nil {
		return nil, fmt.Errorf("find stale %s: %w", table, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func deleteStaleMemberships(ctx context.Context, tx *sql.Tx, table, projectID, runID string, paths []string, full bool) error {
	where, args := membershipScope(projectID, runID, paths, full)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s`, table, where), args...); err != nil {
		return fmt.Errorf("drop stale %s: %w", table, err)
	}
	return nil
}

func membershipScope(projectID, runID string, paths []string, full bool) (string, []any) {
	if full {
		return "project_id = ? AND last_seen_run_id <> ?", []any{projectID, runID}
	}
	if len(paths) == 0 {
		return "1 = 0", nil
	}
	marks := strings.TrimRight(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, 0, len(paths)+2)
	args = append(args, projectID)
	for _, p := range paths {
		args = append(args, p)
	}
	args = append(args, runID)
	return "project_id = ? AND rel_path IN (" + marks + ") AND last_seen_run_id <> ?", args
}

func deleteLegacyManaged(ctx context.Context, tx *sql.Tx, projectID, runID string) error {
	// Delete stale graph facts first; deleting nodes later also removes their
	// graph-local edges. Never touch facts that were not explicitly index-owned.
	if _, err := tx.ExecContext(ctx, `DELETE FROM edges WHERE project_id = ? AND index_managed = 1 AND (last_index_run_id IS NULL OR last_index_run_id <> ?)`, projectID, runID); err != nil {
		return fmt.Errorf("sweep stale edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE semantic_artifacts SET stale_at = ?, unit_node_id = NULL WHERE unit_node_id IN (SELECT id FROM nodes WHERE project_id = ? AND index_managed = 1 AND (last_index_run_id IS NULL OR last_index_run_id <> ?)) AND stale_at IS NULL`, time.Now().UnixMilli(), projectID, runID); err != nil {
		return fmt.Errorf("mark legacy node artifacts stale: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM iir WHERE project_id = ? AND kind = 'extracted' AND node_id IN (SELECT id FROM nodes WHERE project_id = ? AND index_managed = 1 AND (last_index_run_id IS NULL OR last_index_run_id <> ?))`, projectID, projectID, runID); err != nil {
		return fmt.Errorf("sweep stale iir: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE project_id = ? AND index_managed = 1 AND (last_index_run_id IS NULL OR last_index_run_id <> ?)`, projectID, runID); err != nil {
		return fmt.Errorf("sweep stale nodes: %w", err)
	}
	return nil
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			seen[v] = struct{}{}
		}
	}
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
