package queries

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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

// GetTopKActivated returns the top-K nodes by activation for a project,
// paired with their activation values. Used by the activation layer.
func GetTopKActivated(ctx context.Context, db *sql.DB, projectID string, k int) ([]core.NodeWithActivation, error) {
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
		return nil, fmt.Errorf("get top-k activated: %w", err)
	}
	defer rows.Close()

	var result []core.NodeWithActivation
	for rows.Next() {
		var (
			id, pid, nodeType, label, canonicalID, sourceClass, pluginID string
			createdAt, updatedAt                                         int64
			propertiesJSON                                               string
			activation                                                   float64
		)
		if err := rows.Scan(&id, &pid, &nodeType, &label, &canonicalID, &sourceClass,
			&pluginID, &createdAt, &updatedAt, &propertiesJSON, &activation); err != nil {
			return nil, fmt.Errorf("scan top-k activated: %w", err)
		}
		n, err := buildNode(id, pid, nodeType, label, canonicalID, sourceClass, pluginID, createdAt, updatedAt, propertiesJSON)
		if err != nil {
			return nil, err
		}
		result = append(result, core.NodeWithActivation{Node: n, Activation: activation})
	}
	return result, rows.Err()
}

// ResetActivationSQL zeroes all activation values for a project.
// peak_activation is NOT reset — it accumulates across queries.
func ResetActivationSQL(ctx context.Context, db *sql.DB, projectID string, now int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE node_activation
		SET activation = 0.0,
		    updated_at = ?
		WHERE node_id IN (
		    SELECT na.node_id FROM node_activation na
		    JOIN nodes n ON n.id = na.node_id
		    WHERE n.project_id = ?
		      AND na.activation > 0.0
		)
	`, now, projectID)
	return err
}

// GetConceptSeeds returns all concept seeds for a project from the concept_seeds table.
func GetConceptSeeds(ctx context.Context, db *sql.DB, projectID string) ([]core.ConceptSeed, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT term, COALESCE(definition, ''), related, synonyms
		FROM concept_seeds
		WHERE scope = 'project' OR project_id = ?
		ORDER BY term
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("get concept seeds: %w", err)
	}
	defer rows.Close()
	return scanConceptSeeds(rows)
}

// GetOrgConceptSeeds returns all org-level concept seeds.
func GetOrgConceptSeeds(ctx context.Context, db *sql.DB) ([]core.ConceptSeed, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT term, COALESCE(definition, ''), related, synonyms
		FROM concept_seeds
		WHERE scope = 'org'
		ORDER BY term
	`)
	if err != nil {
		return nil, fmt.Errorf("get org concept seeds: %w", err)
	}
	defer rows.Close()
	return scanConceptSeeds(rows)
}

func scanConceptSeeds(rows *sql.Rows) ([]core.ConceptSeed, error) {
	var seeds []core.ConceptSeed
	for rows.Next() {
		var (
			term, definition, related, synonyms string
		)
		if err := rows.Scan(&term, &definition, &related, &synonyms); err != nil {
			return nil, fmt.Errorf("scan concept seed: %w", err)
		}
		seeds = append(seeds, core.ConceptSeed{
			Term:       term,
			Definition: definition,
			Related:    splitJSONArray(related),
			Synonyms:   splitJSONArray(synonyms),
		})
	}
	return seeds, rows.Err()
}

// splitJSONArray parses a simple JSON string array like ["a","b","c"].
// Returns nil on empty or invalid input.
func splitJSONArray(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	// Simple JSON array parsing — strip brackets and split on commas.
	// Handles the format produced by marshalStringSlice in ontology.go.
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, `"`)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
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
			createdAt, updatedAt                                         int64
			propertiesJSON                                               string
			activation                                                   float64
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

	// Step 2: for each node, load outbound edges with weight.
	anchors := make([]core.Anchor, 0, len(nwa))
	for _, item := range nwa {
		projectID := string(item.node.ProjectID)
		edges, err := GetEdgesFromWithWeight(ctx, db, projectID, string(item.node.ID))
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
