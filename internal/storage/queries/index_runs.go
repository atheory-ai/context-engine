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

// StartIndexRun records an attempt before it can write graph data. A failed
// run is retained for diagnosis and never advances file hashes or ownership.
func (q *IndexQueries) StartIndexRun(ctx context.Context, id, projectID string, pluginIDs []string, startedAt int64) error {
	pluginsJSON, err := json.Marshal(pluginIDs)
	if err != nil {
		return fmt.Errorf("marshal plugin ids: %w", err)
	}
	_, err = q.db.ExecContext(ctx, `INSERT INTO index_runs (id, project_id, plugin_ids, started_at, status) VALUES (?, ?, ?, ?, 'running')`, id, projectID, string(pluginsJSON), startedAt)
	if err != nil {
		return fmt.Errorf("start index run: %w", err)
	}
	return nil
}

// FailIndexRun records that this run did not become authoritative.
func (q *IndexQueries) FailIndexRun(ctx context.Context, id string, completedAt int64, runErr error) error {
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	_, err := q.db.ExecContext(ctx, `UPDATE index_runs SET status = 'failed', completed_at = ?, error_message = ? WHERE id = ?`, completedAt, message, id)
	if err != nil {
		return fmt.Errorf("fail index run: %w", err)
	}
	return nil
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
	return tx.Commit()
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
