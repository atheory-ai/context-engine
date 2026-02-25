package queries

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// ActivationRow is the raw activation record from node_activation.
type ActivationRow struct {
	NodeID         string
	Activation     float64
	PeakActivation float64
	UpdatedAt      int64
}

// GetActivation returns the current activation level for a node.
// Returns 0.0 if no activation row exists.
func GetActivation(ctx context.Context, db *sql.DB, nodeID string) (float64, error) {
	var activation float64
	err := db.QueryRowContext(ctx,
		`SELECT activation FROM node_activation WHERE node_id = ?`, nodeID,
	).Scan(&activation)
	if err == sql.ErrNoRows {
		return 0.0, nil
	}
	if err != nil {
		return 0.0, fmt.Errorf("get activation: %w", err)
	}
	return activation, nil
}

// GetActivationRow returns the full activation record for a node.
// Returns nil if no activation row exists.
func GetActivationRow(ctx context.Context, db *sql.DB, nodeID string) (*ActivationRow, error) {
	var row ActivationRow
	err := db.QueryRowContext(ctx, `
		SELECT node_id, activation, peak_activation, updated_at
		FROM node_activation WHERE node_id = ?
	`, nodeID).Scan(&row.NodeID, &row.Activation, &row.PeakActivation, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get activation row: %w", err)
	}
	return &row, nil
}

// TopKAnchors returns the top-K activated nodes as Anchors, with edges loaded.
// This is the hot-path query during activation propagation.
// Uses the idx_node_activation_level index for the ORDER BY.
func TopKAnchors(ctx context.Context, db *sql.DB, projectID string, k int) ([]core.Anchor, error) {
	// Step 1: top-K nodes by activation (uses index).
	rows, err := db.QueryContext(ctx, `
		SELECT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties,
		       na.activation
		FROM nodes n
		JOIN node_activation na ON na.node_id = n.id
		WHERE n.project_id = ?
		ORDER BY na.activation DESC
		LIMIT ?
	`, projectID, k)
	if err != nil {
		return nil, fmt.Errorf("top-k anchors: %w", err)
	}
	defer rows.Close()

	type nodeWithActivation struct {
		node       core.Node
		activation float64
	}
	var nwa []nodeWithActivation

	for rows.Next() {
		var (
			id, pid, nodeType, label, canonicalID, sourceClass, pluginID string
			createdAt, updatedAt                                          int64
			propertiesJSON                                                string
			activation                                                    float64
		)
		if err := rows.Scan(&id, &pid, &nodeType, &label, &canonicalID, &sourceClass,
			&pluginID, &createdAt, &updatedAt, &propertiesJSON, &activation); err != nil {
			return nil, fmt.Errorf("scan anchor node: %w", err)
		}
		n, err := buildNode(id, pid, nodeType, label, canonicalID, sourceClass, pluginID, createdAt, updatedAt, propertiesJSON)
		if err != nil {
			return nil, err
		}
		nwa = append(nwa, nodeWithActivation{node: n, activation: activation})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Step 2: for each node, load outbound edges.
	anchors := make([]core.Anchor, 0, len(nwa))
	for _, item := range nwa {
		edges, err := GetEdgesFromNode(ctx, db, string(item.node.ID), "")
		if err != nil {
			return nil, fmt.Errorf("load edges for anchor %s: %w", item.node.ID, err)
		}
		n := item.node
		anchors = append(anchors, core.Anchor{
			Ref: core.AnchorRef{
				Type: item.node.Type,
				ID:   item.node.CanonicalID,
			},
			Node:       &n,
			Edges:      edges,
			Activation: item.activation,
		})
	}
	return anchors, nil
}
