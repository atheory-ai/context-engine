// Package substrate implements SubstrateReader and SubstrateWriter against
// the SQLite property graph databases managed by internal/storage.
package substrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/storage/queries"
	"github.com/atheory/context-engine/internal/storage/writebuffer"
)

// Reader implements core.SubstrateReader against a DBProvider.
// Safe for concurrent use — all reads are stateless SQL queries.
type Reader struct {
	dbProvider writebuffer.DBProvider
}

// NewReader creates a Reader backed by the given DBProvider.
func NewReader(dbProvider writebuffer.DBProvider) *Reader {
	return &Reader{dbProvider: dbProvider}
}

// Node retrieves a single node by ID. Searches all mounted project databases.
// Returns nil if not found in any mounted database.
func (r *Reader) Node(ctx context.Context, id core.NodeID) (*core.Node, error) {
	for _, projectID := range r.allProjectIDs() {
		db, err := r.dbProvider.GraphDB(projectID)
		if err != nil {
			continue // not mounted
		}
		n, err := queries.GetNode(ctx, db, string(id))
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", id, err)
		}
		if n != nil {
			return n, nil
		}
	}
	return nil, nil
}

// NodeInProject retrieves a node by ID from a specific project's database.
func (r *Reader) NodeInProject(ctx context.Context, projectID core.ProjectID, id core.NodeID) (*core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNode(ctx, db, string(id))
}

// Edges returns all edges from a node, optionally filtered by type.
// Searches the project database for the node's project ID.
func (r *Reader) Edges(ctx context.Context, nodeID core.NodeID, edgeType string) ([]core.Edge, error) {
	db, err := r.resolveDB(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("resolve db for node %s: %w", nodeID, err)
	}
	if db == nil {
		return nil, nil // node not found in any mounted DB
	}
	return queries.GetEdgesFromNode(ctx, db, string(nodeID), edgeType)
}

// EdgesTo returns all edges where the given node is the target, optionally filtered by type.
func (r *Reader) EdgesTo(ctx context.Context, nodeID core.NodeID, edgeType string) ([]core.Edge, error) {
	db, err := r.resolveDB(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("resolve db for node %s: %w", nodeID, err)
	}
	if db == nil {
		return nil, nil
	}
	return queries.GetEdgesToNode(ctx, db, string(nodeID), edgeType)
}

// TopK returns the top-K nodes by activation level for a project, with edges loaded.
func (r *Reader) TopK(ctx context.Context, projectID core.ProjectID, k int) ([]core.Anchor, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.TopKAnchors(ctx, db, string(projectID), k)
}

// Query executes a flexible substrate query against a specific project.
func (r *Reader) Query(ctx context.Context, q core.SubstrateQuery) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(q.ProjectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", q.ProjectID, err)
	}
	return r.execQuery(ctx, db, q)
}

// execQuery builds and runs the substrate query against a single database.
func (r *Reader) execQuery(ctx context.Context, db *sql.DB, q core.SubstrateQuery) ([]core.Node, error) {
	var conditions []string
	var args []any

	conditions = append(conditions, "n.project_id = ?")
	args = append(args, string(q.ProjectID))

	if len(q.NodeTypes) > 0 {
		placeholders := make([]string, len(q.NodeTypes))
		for i, t := range q.NodeTypes {
			placeholders[i] = "?"
			args = append(args, t)
		}
		conditions = append(conditions, "n.type IN ("+strings.Join(placeholders, ",")+")")
	}

	if q.MinActivation > 0 {
		conditions = append(conditions, "na.activation >= ?")
		args = append(args, q.MinActivation)
	}

	for key, val := range q.Properties {
		conditions = append(conditions, fmt.Sprintf("json_extract(n.properties, '$.%s') = ?", key))
		args = append(args, val)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = core.DefaultKLimit
	}

	joinType := "LEFT JOIN"
	if q.MinActivation > 0 {
		joinType = "JOIN" // INNER JOIN when activation filter is active
	}

	query := fmt.Sprintf(`
		SELECT n.id, n.project_id, n.type, n.label, n.canonical_id, n.source_class,
		       COALESCE(n.plugin_id, ''), n.created_at, n.updated_at, n.properties
		FROM nodes n
		%s node_activation na ON na.node_id = n.id
		WHERE %s
		ORDER BY COALESCE(na.activation, 0) DESC
		LIMIT ?
	`, joinType, strings.Join(conditions, " AND "))
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("substrate query: %w", err)
	}
	defer rows.Close()

	var nodes []core.Node
	for rows.Next() {
		var (
			id, projectID, nodeType, label, canonicalID, sourceClass, pluginID string
			createdAt, updatedAt                                                int64
			propertiesJSON                                                      string
		)
		if err := rows.Scan(&id, &projectID, &nodeType, &label, &canonicalID,
			&sourceClass, &pluginID, &createdAt, &updatedAt, &propertiesJSON); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		var props map[string]any
		if err := json.Unmarshal([]byte(propertiesJSON), &props); err != nil {
			props = make(map[string]any)
		}
		nodes = append(nodes, core.Node{
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
		})
	}
	return nodes, rows.Err()
}

// resolveDB finds which mounted database contains the given node ID.
func (r *Reader) resolveDB(ctx context.Context, nodeID core.NodeID) (*sql.DB, error) {
	// Try a sequence of common project IDs.
	// In production this list comes from the mounted project registry.
	// This implementation iterates deterministically: org first, then any project DB.
	for _, projectID := range r.allProjectIDs() {
		db, err := r.dbProvider.GraphDB(projectID)
		if err != nil {
			continue
		}
		n, err := queries.GetNode(ctx, db, string(nodeID))
		if err != nil {
			return nil, err
		}
		if n != nil {
			return db, nil
		}
	}
	return nil, nil
}

// projectIDLister is an optional extension of DBProvider that lists mounted projects.
type projectIDLister interface {
	MountedProjectIDs() []string
}

// allProjectIDs returns the set of project IDs to search.
// If the DBProvider implements projectIDLister, use it; otherwise fall back to ["org", "local"].
func (r *Reader) allProjectIDs() []string {
	if pl, ok := r.dbProvider.(projectIDLister); ok {
		return pl.MountedProjectIDs()
	}
	return []string{"org", "local"}
}
