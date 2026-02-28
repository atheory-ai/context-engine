package orggraph

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// Lifter copies eligible nodes and edges from a project graph (src) into
// the org graph (dst). All work is done in a single transaction.
type Lifter struct {
	projectID core.ProjectID
	src       *sql.DB // project graph.db — read-only source
	dst       *sql.DB // org.db — write destination
}

// Run executes the lift in a transaction.
// Lifts eligible nodes first, then edges (only between lifted nodes).
func (l *Lifter) Run(ctx context.Context) error {
	tx, err := l.dst.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin lift tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := l.liftNodes(ctx, tx); err != nil {
		return fmt.Errorf("lift nodes: %w", err)
	}
	if err := l.liftEdges(ctx, tx); err != nil {
		return fmt.Errorf("lift edges: %w", err)
	}

	return tx.Commit()
}

// liftNodes queries the source project graph for eligible nodes and
// UPSERTs them into the org graph.
//
// Eligible node types:
//   - namespace — package/module structure
//   - symbol with exported=true or kind=interface — public API surfaces
//   - concept — domain vocabulary
func (l *Lifter) liftNodes(ctx context.Context, tx *sql.Tx) error {
	rows, err := l.src.QueryContext(ctx, `
		SELECT id, type, label, canonical_id, source_class,
		       COALESCE(plugin_id, ''), properties, created_at, updated_at
		FROM nodes
		WHERE project_id = ?
		  AND (
		    type = 'namespace'
		    OR type = 'concept'
		    OR (type = 'symbol' AND (
		          json_extract(properties, '$.exported') = 1
		       OR json_extract(properties, '$.exported') = true
		       OR json_extract(properties, '$.kind') = 'interface'
		    ))
		  )
	`, string(l.projectID))
	if err != nil {
		return err
	}
	defer rows.Close()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nodes
		    (id, project_id, type, label, canonical_id,
		     source_class, plugin_id, properties, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		    label        = excluded.label,
		    source_class = excluded.source_class,
		    properties   = excluded.properties,
		    updated_at   = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var (
			id, ntype, label, canonicalID, sourceClass, pluginID string
			propsJSON                                             string
			createdAt, updatedAt                                  int64
		)
		if err := rows.Scan(&id, &ntype, &label, &canonicalID,
			&sourceClass, &pluginID, &propsJSON, &createdAt, &updatedAt); err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			id, string(l.projectID), ntype, label, canonicalID,
			sourceClass, pluginID, propsJSON, createdAt, updatedAt,
		); err != nil {
			return err
		}
	}
	return rows.Err()
}

// liftEdges queries the source project graph for eligible edges and
// UPSERTs them into the org graph — but only for edges where both
// the source and target node were lifted (exist in org.db for this project).
//
// Eligible edge types: imports, implements, extends, concept_of, depends_on.
func (l *Lifter) liftEdges(ctx context.Context, tx *sql.Tx) error {
	rows, err := l.src.QueryContext(ctx, `
		SELECT id, source_id, target_id, type, source_class,
		       COALESCE(plugin_id, ''), properties, created_at
		FROM edges
		WHERE project_id = ?
		  AND type IN ('imports', 'implements', 'extends', 'concept_of', 'depends_on')
	`, string(l.projectID))
	if err != nil {
		return err
	}
	defer rows.Close()

	// Read lifted node IDs for this project so we can filter edges.
	liftedIDs, err := l.getLiftedNodeIDs(ctx, tx)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO edges
		    (id, project_id, source_id, target_id, type,
		     source_class, plugin_id, properties, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		    source_class = excluded.source_class,
		    properties   = excluded.properties
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var (
			id, sourceID, targetID, edgeType, sourceClass, pluginID string
			propsJSON                                                string
			createdAt                                                int64
		)
		if err := rows.Scan(&id, &sourceID, &targetID, &edgeType,
			&sourceClass, &pluginID, &propsJSON, &createdAt); err != nil {
			return err
		}
		// Skip edges where either endpoint was not lifted.
		if !liftedIDs[sourceID] || !liftedIDs[targetID] {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			id, string(l.projectID), sourceID, targetID, edgeType,
			sourceClass, pluginID, propsJSON, createdAt,
		); err != nil {
			return err
		}
	}
	return rows.Err()
}

// getLiftedNodeIDs returns the set of node IDs already in org.db for this project
// (within the current transaction, so it reflects the just-lifted nodes).
func (l *Lifter) getLiftedNodeIDs(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM nodes WHERE project_id = ?`,
		string(l.projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
