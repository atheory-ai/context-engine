// Package queries provides typed query functions for all CE databases.
// All functions take a *sql.DB and return typed structs.
// Connection management belongs to internal/storage/db — never opened here.
package queries

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// GetNode retrieves a single node by its ID.
func GetNode(ctx context.Context, db *sql.DB, id string) (*core.Node, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, project_id, type, label, canonical_id, source_class,
		       COALESCE(plugin_id, ''), created_at, updated_at, properties
		FROM nodes WHERE id = ?
	`, id)
	return scanNodeRow(row)
}

// GetNodeByCanonical retrieves a node by project ID and canonical identifier.
func GetNodeByCanonical(ctx context.Context, db *sql.DB, projectID, canonicalID string) (*core.Node, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, project_id, type, label, canonical_id, source_class,
		       COALESCE(plugin_id, ''), created_at, updated_at, properties
		FROM nodes
		WHERE project_id = ? AND canonical_id = ?
	`, projectID, canonicalID)
	return scanNodeRow(row)
}

// ListNodesByType returns all nodes of a given type within a project.
func ListNodesByType(ctx context.Context, db *sql.DB, projectID, nodeType string) ([]core.Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, project_id, type, label, canonical_id, source_class,
		       COALESCE(plugin_id, ''), created_at, updated_at, properties
		FROM nodes
		WHERE project_id = ? AND type = ?
		ORDER BY label
	`, projectID, nodeType)
	if err != nil {
		return nil, fmt.Errorf("list nodes by type: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// TopKActivatedNodes returns the top-K nodes by activation level for a project.
// Performs a JOIN against node_activation — only nodes with activation rows are returned.
func TopKActivatedNodes(ctx context.Context, db *sql.DB, projectID string, k int) ([]core.Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties
		FROM nodes n
		JOIN node_activation na ON na.node_id = n.id
		WHERE n.project_id = ?
		ORDER BY na.activation DESC
		LIMIT ?
	`, projectID, k)
	if err != nil {
		return nil, fmt.Errorf("top-k activated nodes: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// ListNodes returns all nodes for a project.
func ListNodes(ctx context.Context, db *sql.DB, projectID string) ([]core.Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, project_id, type, label, canonical_id, source_class,
		       COALESCE(plugin_id, ''), created_at, updated_at, properties
		FROM nodes
		WHERE project_id = ?
		ORDER BY type, label
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// scanNodeRow scans a single node from a *sql.Row.
func scanNodeRow(row *sql.Row) (*core.Node, error) {
	var (
		id, projectID, nodeType, label, canonicalID, sourceClass, pluginID string
		createdAt, updatedAt                                                int64
		propertiesJSON                                                      string
	)
	err := row.Scan(&id, &projectID, &nodeType, &label, &canonicalID, &sourceClass,
		&pluginID, &createdAt, &updatedAt, &propertiesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan node: %w", err)
	}
	n, err := buildNode(id, projectID, nodeType, label, canonicalID, sourceClass, pluginID, createdAt, updatedAt, propertiesJSON)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// scanNodes scans multiple nodes from *sql.Rows.
func scanNodes(rows *sql.Rows) ([]core.Node, error) {
	var nodes []core.Node
	for rows.Next() {
		var (
			id, projectID, nodeType, label, canonicalID, sourceClass, pluginID string
			createdAt, updatedAt                                                int64
			propertiesJSON                                                      string
		)
		if err := rows.Scan(&id, &projectID, &nodeType, &label, &canonicalID, &sourceClass,
			&pluginID, &createdAt, &updatedAt, &propertiesJSON); err != nil {
			return nil, fmt.Errorf("scan node row: %w", err)
		}
		n, err := buildNode(id, projectID, nodeType, label, canonicalID, sourceClass, pluginID, createdAt, updatedAt, propertiesJSON)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func buildNode(id, projectID, nodeType, label, canonicalID, sourceClass, pluginID string, createdAt, updatedAt int64, propertiesJSON string) (core.Node, error) {
	var props map[string]any
	if err := json.Unmarshal([]byte(propertiesJSON), &props); err != nil {
		props = make(map[string]any)
	}
	return core.Node{
		ID:          core.NodeID(id),
		ProjectID:   core.ProjectID(projectID),
		Type:        nodeType,
		Label:       label,
		CanonicalID: canonicalID,
		SourceClass: core.SourceClass(sourceClass),
		PluginID:    core.PluginID(pluginID),
		Properties:  props,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}
