package queries

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// GetCallees returns nodes that the given node calls, up to maxDepth hops.
// Uses a recursive CTE to traverse "calls" edges forward.
func GetCallees(ctx context.Context, db *sql.DB, projectID, nodeID string, maxDepth int) ([]core.NodeWithActivation, error) {
	rows, err := db.QueryContext(ctx, `
		WITH RECURSIVE call_chain(node_id, depth) AS (
			SELECT e.target_id, 1
			FROM edges e
			JOIN edge_weight ew ON ew.edge_id = e.id
			WHERE e.source_id = ? AND e.type = 'calls' AND e.project_id = ? AND ew.weight > 0.05
			UNION ALL
			SELECT e.target_id, cc.depth + 1
			FROM edges e
			JOIN edge_weight ew ON ew.edge_id = e.id
			JOIN call_chain cc ON cc.node_id = e.source_id
			WHERE e.type = 'calls' AND e.project_id = ? AND ew.weight > 0.05 AND cc.depth < ?
		)
		SELECT DISTINCT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties,
		       COALESCE(na.activation, 0.0)
		FROM call_chain cc
		JOIN nodes n ON n.id = cc.node_id
		LEFT JOIN node_activation na ON na.node_id = n.id
		ORDER BY COALESCE(na.activation, 0.0) DESC
		LIMIT 50
	`, nodeID, projectID, projectID, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("get callees: %w", err)
	}
	defer rows.Close()
	return scanNodesWithActivation(rows)
}

// GetCallers returns nodes that call the given node, up to maxDepth hops.
// Traverses "calls" edges in reverse direction.
func GetCallers(ctx context.Context, db *sql.DB, projectID, nodeID string, maxDepth int) ([]core.NodeWithActivation, error) {
	rows, err := db.QueryContext(ctx, `
		WITH RECURSIVE call_chain(node_id, depth) AS (
			SELECT e.source_id, 1
			FROM edges e
			JOIN edge_weight ew ON ew.edge_id = e.id
			WHERE e.target_id = ? AND e.type = 'calls' AND e.project_id = ? AND ew.weight > 0.05
			UNION ALL
			SELECT e.source_id, cc.depth + 1
			FROM edges e
			JOIN edge_weight ew ON ew.edge_id = e.id
			JOIN call_chain cc ON cc.node_id = e.target_id
			WHERE e.type = 'calls' AND e.project_id = ? AND ew.weight > 0.05 AND cc.depth < ?
		)
		SELECT DISTINCT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties,
		       COALESCE(na.activation, 0.0)
		FROM call_chain cc
		JOIN nodes n ON n.id = cc.node_id
		LEFT JOIN node_activation na ON na.node_id = n.id
		ORDER BY COALESCE(na.activation, 0.0) DESC
		LIMIT 50
	`, nodeID, projectID, projectID, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("get callers: %w", err)
	}
	defer rows.Close()
	return scanNodesWithActivation(rows)
}

// GetReferences returns all nodes that reference the given node, with edge type and weight.
func GetReferences(ctx context.Context, db *sql.DB, projectID, nodeID string) ([]core.ReferenceResult, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties,
		       COALESCE(na.activation, 0.0),
		       e.type, COALESCE(ew.weight, 1.0)
		FROM edges e
		JOIN nodes n ON n.id = e.source_id
		JOIN edge_weight ew ON ew.edge_id = e.id
		LEFT JOIN node_activation na ON na.node_id = n.id
		WHERE e.target_id = ? AND e.project_id = ? AND ew.weight > 0.05
		ORDER BY ew.weight DESC, COALESCE(na.activation, 0.0) DESC
		LIMIT 100
	`, nodeID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get references: %w", err)
	}
	defer rows.Close()

	var refs []core.ReferenceResult
	for rows.Next() {
		var (
			id, pid, nodeType, label, canonicalID, sourceClass, pluginID string
			createdAt, updatedAt                                         int64
			propertiesJSON                                               string
			activation                                                   float64
			edgeType                                                     string
			weight                                                       float64
		)
		if err := rows.Scan(&id, &pid, &nodeType, &label, &canonicalID, &sourceClass,
			&pluginID, &createdAt, &updatedAt, &propertiesJSON,
			&activation, &edgeType, &weight); err != nil {
			return nil, fmt.Errorf("scan reference: %w", err)
		}
		n, err := buildNode(id, pid, nodeType, label, canonicalID, sourceClass, pluginID, createdAt, updatedAt, propertiesJSON)
		if err != nil {
			return nil, err
		}
		refs = append(refs, core.ReferenceResult{
			Node:     core.NodeWithActivation{Node: n, Activation: activation},
			EdgeType: edgeType,
			Weight:   weight,
		})
	}
	return refs, rows.Err()
}

// GetConceptImplementors returns nodes that implement or relate to a concept node.
func GetConceptImplementors(ctx context.Context, db *sql.DB, projectID, conceptNodeID string) ([]core.NodeWithActivation, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties,
		       COALESCE(na.activation, 0.0)
		FROM edges e
		JOIN nodes n ON n.id = e.source_id
		JOIN edge_weight ew ON ew.edge_id = e.id
		LEFT JOIN node_activation na ON na.node_id = n.id
		WHERE e.target_id = ? AND e.project_id = ?
		  AND e.type IN ('implements', 'annotates', 'co_activates')
		  AND ew.weight > 0.05
		ORDER BY COALESCE(na.activation, 0.0) DESC
		LIMIT 50
	`, conceptNodeID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get concept implementors: %w", err)
	}
	defer rows.Close()
	return scanNodesWithActivation(rows)
}

// GetConceptSeedByTerm returns a single concept seed for the given term.
// Returns nil if the term is not found in the concept_seeds table.
func GetConceptSeedByTerm(ctx context.Context, db *sql.DB, projectID, term string) (*core.ConceptSeed, error) {
	row := db.QueryRowContext(ctx, `
		SELECT term, COALESCE(definition, ''), related, synonyms
		FROM concept_seeds
		WHERE term = ? AND (project_id = ? OR scope = 'project' OR scope = 'org')
		LIMIT 1
	`, term, projectID)
	var t, def, related, synonyms string
	if err := row.Scan(&t, &def, &related, &synonyms); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("get concept seed by term: %w", err)
	}
	return &core.ConceptSeed{
		Term:       t,
		Definition: def,
		Related:    splitJSONArray(related),
		Synonyms:   splitJSONArray(synonyms),
	}, nil
}

// GetFileNode returns the file-type node for the given file path (canonical ID).
func GetFileNode(ctx context.Context, db *sql.DB, projectID, filePath string) (*core.Node, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, project_id, type, label, canonical_id, source_class,
		       COALESCE(plugin_id, ''), created_at, updated_at, properties
		FROM nodes
		WHERE project_id = ? AND canonical_id = ? AND type = 'file'
		LIMIT 1
	`, projectID, filePath)
	return scanNodeRow(row)
}

// GetFileImports returns nodes imported by the given file node.
func GetFileImports(ctx context.Context, db *sql.DB, projectID, fileNodeID string) ([]core.Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties
		FROM edges e
		JOIN nodes n ON n.id = e.target_id
		JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.source_id = ? AND e.project_id = ? AND e.type = 'imports'
		ORDER BY n.canonical_id
		LIMIT 100
	`, fileNodeID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get file imports: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// GetNamespaceMembers returns all nodes directly contained in or defined by a namespace node.
func GetNamespaceMembers(ctx context.Context, db *sql.DB, projectID, namespaceNodeID string) ([]core.Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties
		FROM edges e
		JOIN nodes n ON n.id = e.target_id
		JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.source_id = ? AND e.project_id = ? AND e.type IN ('contains', 'defines')
		ORDER BY n.type, n.label
		LIMIT 500
	`, namespaceNodeID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get namespace members: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// GetNamespaceDependencies returns namespaces/files that this namespace imports.
func GetNamespaceDependencies(ctx context.Context, db *sql.DB, projectID, namespaceNodeID string) ([]core.Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties
		FROM edges e
		JOIN nodes n ON n.id = e.target_id
		JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.source_id = ? AND e.project_id = ? AND e.type = 'imports'
		  AND n.type IN ('namespace', 'file')
		ORDER BY n.canonical_id
		LIMIT 100
	`, namespaceNodeID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get namespace dependencies: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// GetNamespaceDependents returns namespaces/files that import this namespace.
func GetNamespaceDependents(ctx context.Context, db *sql.DB, projectID, namespaceNodeID string) ([]core.Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties
		FROM edges e
		JOIN nodes n ON n.id = e.source_id
		JOIN edge_weight ew ON ew.edge_id = e.id
		WHERE e.target_id = ? AND e.project_id = ? AND e.type = 'imports'
		  AND n.type IN ('namespace', 'file')
		ORDER BY n.canonical_id
		LIMIT 100
	`, namespaceNodeID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get namespace dependents: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// FindInOrgGraph searches the org graph for nodes matching the given canonical ID.
// Uses two strategies:
//   - Strategy 1 (exact): canonical_id = canonicalID → Similarity 1.0
//   - Strategy 2 (suffix): canonical_id LIKE '%:' + canonicalID → Similarity 0.8
//     (handles bare label lookups like "ProcessPayment" → "billing:ProcessPayment")
//
// Results are deduplicated by node ID before returning.
// If nodeType is non-empty, restricts both strategies to that node type.
func FindInOrgGraph(ctx context.Context, db *sql.DB, canonicalID, nodeType string) ([]core.OrgMatch, error) {
	seen := make(map[string]bool)
	var results []core.OrgMatch

	// Strategy 1: exact canonical ID match.
	exactNodes, err := orgGraphQuery(ctx, db, `canonical_id = ?`, nodeType, canonicalID, 20)
	if err != nil {
		return nil, fmt.Errorf("find in org graph (exact): %w", err)
	}
	for _, n := range exactNodes {
		if !seen[string(n.ID)] {
			seen[string(n.ID)] = true
			results = append(results, core.OrgMatch{
				Node:        n,
				ProjectID:   n.ProjectID,
				ProjectName: string(n.ProjectID),
				Similarity:  1.0,
			})
		}
	}

	// Strategy 2: suffix match — only when canonicalID has no separator,
	// meaning the caller passed a bare symbol name without a namespace prefix.
	if !containsColon(canonicalID) {
		suffix := "%:" + canonicalID
		suffixNodes, err := orgGraphQuery(ctx, db, `canonical_id LIKE ?`, nodeType, suffix, 20)
		if err != nil {
			return nil, fmt.Errorf("find in org graph (suffix): %w", err)
		}
		for _, n := range suffixNodes {
			if !seen[string(n.ID)] {
				seen[string(n.ID)] = true
				results = append(results, core.OrgMatch{
					Node:        n,
					ProjectID:   n.ProjectID,
					ProjectName: string(n.ProjectID),
					Similarity:  0.8,
				})
			}
		}
	}

	return results, nil
}

// orgGraphQuery executes a nodes query against org.db using the given WHERE condition.
// If nodeType is non-empty, an additional type filter is applied.
func orgGraphQuery(ctx context.Context, db *sql.DB, cond, nodeType, arg string, limit int) ([]core.Node, error) {
	var q string
	var args []any
	if nodeType == "" {
		q = `SELECT id, project_id, type, label, canonical_id, source_class,
		            COALESCE(plugin_id, ''), created_at, updated_at, properties
		     FROM nodes WHERE ` + cond + ` ORDER BY label LIMIT ?`
		args = []any{arg, limit}
	} else {
		q = `SELECT id, project_id, type, label, canonical_id, source_class,
		            COALESCE(plugin_id, ''), created_at, updated_at, properties
		     FROM nodes WHERE ` + cond + ` AND type = ? ORDER BY label LIMIT ?`
		args = []any{arg, nodeType, limit}
	}

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// containsColon returns true if s contains a ':' character.
// Used to distinguish bare symbol names from fully-qualified canonical IDs.
func containsColon(s string) bool {
	for _, c := range s {
		if c == ':' {
			return true
		}
	}
	return false
}

// scanNodesWithActivation scans rows of (node columns..., activation float64).
// Used by GetCallees, GetCallers, GetConceptImplementors.
func scanNodesWithActivation(rows *sql.Rows) ([]core.NodeWithActivation, error) {
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
			return nil, fmt.Errorf("scan node with activation: %w", err)
		}
		n, err := buildNode(id, pid, nodeType, label, canonicalID, sourceClass, pluginID, createdAt, updatedAt, propertiesJSON)
		if err != nil {
			return nil, err
		}
		result = append(result, core.NodeWithActivation{Node: n, Activation: activation})
	}
	return result, rows.Err()
}
