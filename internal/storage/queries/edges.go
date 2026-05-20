package queries

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
)

// GetEdge retrieves a single edge by its ID, joining edge_weight for the weight field.
func GetEdge(ctx context.Context, db *sql.DB, id string) (*core.Edge, error) {
	row := db.QueryRowContext(ctx, `
		SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
		       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
		       COALESCE(ew.weight, 1.0)
		FROM edges e
		LEFT JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.id = ?
	`, id)
	return scanEdgeRow(row)
}

// GetEdgesFromNode returns all edges with the given source node.
// Pass edgeType="" to return all edge types.
func GetEdgesFromNode(ctx context.Context, db *sql.DB, nodeID, edgeType string) ([]core.Edge, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if edgeType == "" {
		rows, err = db.QueryContext(ctx, `
			SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
			       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
			       COALESCE(ew.weight, 1.0)
			FROM edges e
			LEFT JOIN edge_weight ew ON ew.edge_id = e.id
			WHERE e.source_id = ?
			ORDER BY COALESCE(ew.weight, 1.0) DESC
		`, nodeID)
	} else {
		rows, err = db.QueryContext(ctx, `
			SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
			       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
			       COALESCE(ew.weight, 1.0)
			FROM edges e
			LEFT JOIN edge_weight ew ON ew.edge_id = e.id
			WHERE e.source_id = ? AND e.type = ?
			ORDER BY COALESCE(ew.weight, 1.0) DESC
		`, nodeID, edgeType)
	}
	if err != nil {
		return nil, fmt.Errorf("get edges from node: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetEdgesToNode returns all edges with the given target node.
// Pass edgeType="" to return all edge types.
func GetEdgesToNode(ctx context.Context, db *sql.DB, nodeID, edgeType string) ([]core.Edge, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if edgeType == "" {
		rows, err = db.QueryContext(ctx, `
			SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
			       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
			       COALESCE(ew.weight, 1.0)
			FROM edges e
			LEFT JOIN edge_weight ew ON ew.edge_id = e.id
			WHERE e.target_id = ?
			ORDER BY COALESCE(ew.weight, 1.0) DESC
		`, nodeID)
	} else {
		rows, err = db.QueryContext(ctx, `
			SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
			       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
			       COALESCE(ew.weight, 1.0)
			FROM edges e
			LEFT JOIN edge_weight ew ON ew.edge_id = e.id
			WHERE e.target_id = ? AND e.type = ?
			ORDER BY COALESCE(ew.weight, 1.0) DESC
		`, nodeID, edgeType)
	}
	if err != nil {
		return nil, fmt.Errorf("get edges to node: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetEdgesFromWithWeight returns outbound edges for a node, joining edge_weight for
// SourceClass and CoActivationCount. Ordered by weight DESC.
func GetEdgesFromWithWeight(ctx context.Context, db *sql.DB, projectID, nodeID string) ([]core.EdgeWithWeight, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
		       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
		       COALESCE(ew.weight, 1.0),
		       COALESCE(ew.source_class, ''),
		       COALESCE(ew.co_activation_count, 0)
		FROM edges e
		LEFT JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.project_id = ? AND e.source_id = ?
		ORDER BY COALESCE(ew.weight, 1.0) DESC
	`, projectID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get edges from with weight: %w", err)
	}
	defer rows.Close()
	return scanEdgesWithWeight(rows)
}

// GetEdgesToWithWeight returns inbound edges for a node, joining edge_weight.
func GetEdgesToWithWeight(ctx context.Context, db *sql.DB, projectID, nodeID string) ([]core.EdgeWithWeight, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
		       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
		       COALESCE(ew.weight, 1.0),
		       COALESCE(ew.source_class, ''),
		       COALESCE(ew.co_activation_count, 0)
		FROM edges e
		LEFT JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.project_id = ? AND e.target_id = ?
		ORDER BY COALESCE(ew.weight, 1.0) DESC
	`, projectID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get edges to with weight: %w", err)
	}
	defer rows.Close()
	return scanEdgesWithWeight(rows)
}

// GetEdgesBetweenWithWeight returns all edges between two nodes in either direction.
func GetEdgesBetweenWithWeight(ctx context.Context, db *sql.DB, projectID, sourceID, targetID string) ([]core.EdgeWithWeight, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
		       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
		       COALESCE(ew.weight, 1.0),
		       COALESCE(ew.source_class, ''),
		       COALESCE(ew.co_activation_count, 0)
		FROM edges e
		LEFT JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.project_id = ?
		  AND ((e.source_id = ? AND e.target_id = ?) OR (e.source_id = ? AND e.target_id = ?))
		ORDER BY COALESCE(ew.weight, 1.0) DESC
	`, projectID, sourceID, targetID, targetID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("get edges between: %w", err)
	}
	defer rows.Close()
	return scanEdgesWithWeight(rows)
}

// DecayEdgeWeightsSQL reduces all edge weights for a project by decayRate.
// Weights that fall below MinEdgeWeight are floored there.
// Runs as a single UPDATE statement — efficient even for large graphs.
func DecayEdgeWeightsSQL(ctx context.Context, db *sql.DB, projectID string, decayRate float64, now int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE edge_weight
		SET weight = MAX(?, weight * (1.0 - ?)),
		    updated_at = ?
		WHERE edge_id IN (
		    SELECT ew.edge_id FROM edge_weight ew
		    JOIN edges e ON e.id = ew.edge_id
		    WHERE e.project_id = ?
		)
	`, core.MinEdgeWeight, decayRate, now, projectID)
	return err
}

// GetActivatedEdgesAbove returns all edges whose source node has activation above threshold.
// This is a hot-path query during activation propagation — uses the composite index.
func GetActivatedEdgesAbove(ctx context.Context, db *sql.DB, projectID string, threshold float64) ([]core.Edge, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.id, e.project_id, e.source_id, e.target_id, e.type, e.source_class,
		       COALESCE(e.plugin_id, ''), e.created_at, e.properties,
		       COALESCE(ew.weight, 1.0)
		FROM edges e
		JOIN edge_weight ew ON ew.edge_id = e.id
		JOIN node_activation na ON na.node_id = e.source_id
		WHERE e.project_id = ? AND na.activation > ?
		ORDER BY ew.weight DESC
	`, projectID, threshold)
	if err != nil {
		return nil, fmt.Errorf("get activated edges: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

func scanEdgeRow(row *sql.Row) (*core.Edge, error) {
	var (
		id, projectID, sourceID, targetID, edgeType, sourceClass, pluginID string
		createdAt                                                          int64
		propertiesJSON                                                     string
		weight                                                             float64
	)
	err := row.Scan(&id, &projectID, &sourceID, &targetID, &edgeType, &sourceClass,
		&pluginID, &createdAt, &propertiesJSON, &weight)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan edge: %w", err)
	}
	e, err := buildEdge(id, projectID, sourceID, targetID, edgeType, sourceClass, pluginID, createdAt, propertiesJSON, weight)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func scanEdges(rows *sql.Rows) ([]core.Edge, error) {
	var edges []core.Edge
	for rows.Next() {
		var (
			id, projectID, sourceID, targetID, edgeType, sourceClass, pluginID string
			createdAt                                                          int64
			propertiesJSON                                                     string
			weight                                                             float64
		)
		if err := rows.Scan(&id, &projectID, &sourceID, &targetID, &edgeType, &sourceClass,
			&pluginID, &createdAt, &propertiesJSON, &weight); err != nil {
			return nil, fmt.Errorf("scan edge row: %w", err)
		}
		e, err := buildEdge(id, projectID, sourceID, targetID, edgeType, sourceClass, pluginID, createdAt, propertiesJSON, weight)
		if err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func scanEdgesWithWeight(rows *sql.Rows) ([]core.EdgeWithWeight, error) {
	var edges []core.EdgeWithWeight
	for rows.Next() {
		var (
			id, projectID, sourceID, targetID, edgeType, sourceClass, pluginID string
			createdAt                                                          int64
			propertiesJSON                                                     string
			weight                                                             float64
			weightSourceClass                                                  string
			coActivationCount                                                  int
		)
		if err := rows.Scan(&id, &projectID, &sourceID, &targetID, &edgeType, &sourceClass,
			&pluginID, &createdAt, &propertiesJSON, &weight, &weightSourceClass, &coActivationCount); err != nil {
			return nil, fmt.Errorf("scan edge with weight row: %w", err)
		}
		e, err := buildEdge(id, projectID, sourceID, targetID, edgeType, sourceClass, pluginID, createdAt, propertiesJSON, weight)
		if err != nil {
			return nil, err
		}
		edges = append(edges, core.EdgeWithWeight{
			Edge:              e,
			Weight:            weight,
			SourceClass:       weightSourceClass,
			CoActivationCount: coActivationCount,
		})
	}
	return edges, rows.Err()
}

func buildEdge(id, projectID, sourceID, targetID, edgeType, sourceClass, pluginID string, createdAt int64, propertiesJSON string, weight float64) (core.Edge, error) {
	var props map[string]any
	if err := json.Unmarshal([]byte(propertiesJSON), &props); err != nil {
		props = make(map[string]any)
	}
	return core.Edge{
		ID:          core.EdgeID(id),
		ProjectID:   core.ProjectID(projectID),
		SourceID:    core.NodeID(sourceID),
		TargetID:    core.NodeID(targetID),
		Type:        edgeType,
		SourceClass: core.SourceClass(sourceClass),
		Weight:      weight,
		PluginID:    core.PluginID(pluginID),
		Properties:  props,
		CreatedAt:   createdAt,
	}, nil
}
