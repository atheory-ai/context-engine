// Package orggraph manages the org-level substrate graph (org.db).
// The org graph is a single graph spanning all indexed projects,
// populated by lifting nodes and edges upward from project graphs.
// It enables cross-project queries (crossproject tool) and org-wide
// concept seeds managed via `ce config org-concepts`.
package orggraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/storage/db"
	"github.com/atheory/context-engine/internal/storage/migrations"
)

// OrgGraph manages the org-level substrate.
// Holds an open connection to org.db.
// Safe for concurrent read access; writes are serialized by SQLite.
type OrgGraph struct {
	db      *sql.DB
	ownedDB bool // true if this OrgGraph opened the db and should close it
}

// Open opens org.db from the given data directory, runs graph + org migrations,
// and returns a ready-to-use OrgGraph. Call Close() when done.
// Used by CLI commands that need standalone org graph access.
func Open(dataDir string) (*OrgGraph, error) {
	orgPath := filepath.Join(dataDir, "graphs", "org.db")
	orgDB, err := db.Open(orgPath)
	if err != nil {
		return nil, fmt.Errorf("open org.db: %w", err)
	}
	if err := migrations.RunGraph(orgDB); err != nil {
		orgDB.Close()
		return nil, fmt.Errorf("migrate org.db (graph): %w", err)
	}
	if err := migrations.RunOrg(orgDB); err != nil {
		orgDB.Close()
		return nil, fmt.Errorf("migrate org.db (org): %w", err)
	}
	return &OrgGraph{db: orgDB, ownedDB: true}, nil
}

// OpenFromDB wraps an already-open, already-migrated *sql.DB as an OrgGraph.
// Used by the runner, which opens and migrates org.db itself.
// Close() on the returned OrgGraph is a no-op (the runner owns the connection).
func OpenFromDB(orgDB *sql.DB) *OrgGraph {
	return &OrgGraph{db: orgDB, ownedDB: false}
}

// Close closes the org.db connection if this OrgGraph owns it.
// No-op if the connection was provided via OpenFromDB.
func (g *OrgGraph) Close() error {
	if g.ownedDB && g.db != nil {
		return g.db.Close()
	}
	return nil
}

// Lift copies eligible nodes and edges from a project graph into the org graph.
// Called after every successful index run. Idempotent — safe to call multiple times.
func (g *OrgGraph) Lift(ctx context.Context, projectID core.ProjectID, projectDB *sql.DB) error {
	l := &Lifter{
		projectID: projectID,
		src:       projectDB,
		dst:       g.db,
	}
	return l.Run(ctx)
}

// DetectCrossProjectEdges finds cross-project relationships for the given project.
// Runs after Lift() completes. Idempotent — uses ON CONFLICT DO NOTHING.
func (g *OrgGraph) DetectCrossProjectEdges(ctx context.Context, projectID core.ProjectID) error {
	if err := g.detectSharedDependencies(ctx, projectID); err != nil {
		return fmt.Errorf("shared dependencies: %w", err)
	}
	if err := g.detectSharedInterfaces(ctx, projectID); err != nil {
		return fmt.Errorf("shared interfaces: %w", err)
	}
	return nil
}

// detectSharedDependencies creates cross_project_edges rows for projects that
// share imports to the same target namespace node.
func (g *OrgGraph) detectSharedDependencies(ctx context.Context, projectID core.ProjectID) error {
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO cross_project_edges
		    (id, source_node_id, source_project, target_node_id, target_project,
		     type, source_class, weight, properties, created_at, updated_at)
		SELECT
		    hex(randomblob(16)),
		    e1.source_id,
		    e1.project_id,
		    e2.source_id,
		    e2.project_id,
		    'shares_dependency',
		    'speculative',
		    0.3,
		    json_object('shared_namespace', n.canonical_id),
		    unixepoch() * 1000,
		    unixepoch() * 1000
		FROM edges e1
		JOIN edges e2 ON e2.target_id = e1.target_id
		               AND e2.project_id != e1.project_id
		JOIN nodes n ON n.id = e1.target_id
		WHERE e1.project_id = ?
		  AND e1.type = 'imports'
		  AND e2.type = 'imports'
		ON CONFLICT DO NOTHING
	`, string(projectID))
	return err
}

// detectSharedInterfaces creates cross_project_edges rows for interface nodes
// with the same label in different projects (label-based matching).
func (g *OrgGraph) detectSharedInterfaces(ctx context.Context, projectID core.ProjectID) error {
	// Fetch interface nodes for this project from org.db.
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, label, properties
		FROM nodes
		WHERE project_id = ?
		  AND type = 'symbol'
		  AND (json_extract(properties, '$.kind') = 'interface'
		       OR type = 'interface')
	`, string(projectID))
	if err != nil {
		return fmt.Errorf("fetch interfaces: %w", err)
	}
	defer rows.Close()

	type iface struct {
		id         string
		label      string
		properties string
	}
	var interfaces []iface
	for rows.Next() {
		var i iface
		if err := rows.Scan(&i.id, &i.label, &i.properties); err != nil {
			return fmt.Errorf("scan interface: %w", err)
		}
		interfaces = append(interfaces, i)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	now := time.Now().UnixMilli()

	for _, iface := range interfaces {
		// Find nodes with the same label in other projects.
		similar, err := g.db.QueryContext(ctx, `
			SELECT id, project_id
			FROM nodes
			WHERE label = ?
			  AND project_id != ?
			  AND (json_extract(properties, '$.kind') = 'interface'
			       OR type = 'interface')
			LIMIT 20
		`, iface.label, string(projectID))
		if err != nil {
			return fmt.Errorf("find similar interfaces: %w", err)
		}

		var matchIDs [][2]string // [nodeID, projectID]
		for similar.Next() {
			var matchID, matchProject string
			if err := similar.Scan(&matchID, &matchProject); err != nil {
				similar.Close()
				return fmt.Errorf("scan similar: %w", err)
			}
			matchIDs = append(matchIDs, [2]string{matchID, matchProject})
		}
		similar.Close()
		if err := similar.Err(); err != nil {
			return err
		}

		for _, match := range matchIDs {
			edgeID := core.MakeEdgeID(iface.id, "mirrors", match[0])
			props := fmt.Sprintf(`{"interface":%q}`, iface.label)
			_, err := g.db.ExecContext(ctx, `
				INSERT INTO cross_project_edges
				    (id, source_node_id, source_project, target_node_id, target_project,
				     type, source_class, weight, properties, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, 'mirrors', 'speculative', 0.5, ?, ?, ?)
				ON CONFLICT DO NOTHING
			`, edgeID, iface.id, string(projectID), match[0], match[1], props, now, now)
			if err != nil {
				return fmt.Errorf("insert mirror edge: %w", err)
			}
		}
	}
	return nil
}

// FindSimilar finds nodes in the org graph matching the given canonical ID and type.
// Uses two strategies: exact canonical ID match (similarity 1.0) and
// suffix match — e.g. "ProcessPayment" matches "billing:ProcessPayment" (similarity 0.8).
// Deduplicates results by node ID before returning.
func (g *OrgGraph) FindSimilar(ctx context.Context, canonicalID, nodeType string, limit int) ([]core.OrgMatch, error) {
	if limit <= 0 {
		limit = 20
	}

	seen := make(map[string]bool)
	var results []core.OrgMatch

	// Strategy 1: exact canonical ID match.
	exactRows, err := g.queryNodes(ctx, `canonical_id = ?`, nodeType, canonicalID, limit)
	if err != nil {
		return nil, fmt.Errorf("exact match: %w", err)
	}
	for _, n := range exactRows {
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

	// Strategy 2: suffix match — canonical_id ends with ':' + canonicalID
	// (handles bare label lookups like "ProcessPayment" → "billing:ProcessPayment").
	if !strings.Contains(canonicalID, ":") {
		suffix := "%:" + canonicalID
		suffixRows, err := g.queryNodes(ctx, `canonical_id LIKE ?`, nodeType, suffix, limit)
		if err != nil {
			return nil, fmt.Errorf("suffix match: %w", err)
		}
		for _, n := range suffixRows {
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

// queryNodes executes a node query against org.db, filtered by a WHERE condition
// and optionally by nodeType.
func (g *OrgGraph) queryNodes(ctx context.Context, cond, nodeType, arg string, limit int) ([]core.Node, error) {
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

	rows, err := g.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []core.Node
	for rows.Next() {
		var (
			id, pid, ntype, label, canonicalID, sourceClass, pluginID string
			createdAt, updatedAt                                       int64
			propsJSON                                                  string
		)
		if err := rows.Scan(&id, &pid, &ntype, &label, &canonicalID,
			&sourceClass, &pluginID, &createdAt, &updatedAt, &propsJSON); err != nil {
			return nil, err
		}
		var props map[string]any
		if err := json.Unmarshal([]byte(propsJSON), &props); err != nil {
			props = make(map[string]any)
		}
		nodes = append(nodes, core.Node{
			ID:          core.NodeID(id),
			ProjectID:   core.ProjectID(pid),
			Type:        ntype,
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

// GetOrgConceptSeeds returns all org-level concept seeds from org_concept_seeds table.
func (g *OrgGraph) GetOrgConceptSeeds(ctx context.Context) ([]core.ConceptSeed, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT term, COALESCE(definition, ''), COALESCE(related, '[]'), COALESCE(synonyms, '[]')
		FROM org_concept_seeds
		ORDER BY term
	`)
	if err != nil {
		return nil, fmt.Errorf("get org concept seeds: %w", err)
	}
	defer rows.Close()

	var seeds []core.ConceptSeed
	for rows.Next() {
		var term, definition, related, synonyms string
		if err := rows.Scan(&term, &definition, &related, &synonyms); err != nil {
			return nil, fmt.Errorf("scan org concept seed: %w", err)
		}
		seeds = append(seeds, core.ConceptSeed{
			Term:       term,
			Definition: definition,
			Related:    parseJSONArray(related),
			Synonyms:   parseJSONArray(synonyms),
		})
	}
	return seeds, rows.Err()
}

// AddOrgConceptSeed inserts or updates an org-level concept seed.
func (g *OrgGraph) AddOrgConceptSeed(ctx context.Context, seed core.ConceptSeed) error {
	relatedJSON, _ := json.Marshal(seed.Related)
	synonymsJSON, _ := json.Marshal(seed.Synonyms)
	now := time.Now().UnixMilli()
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO org_concept_seeds (term, definition, related, synonyms, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'manual', ?, ?)
		ON CONFLICT(term) DO UPDATE SET
		    definition = excluded.definition,
		    related    = excluded.related,
		    synonyms   = excluded.synonyms,
		    updated_at = excluded.updated_at
	`, seed.Term, seed.Definition, string(relatedJSON), string(synonymsJSON), now, now)
	if err != nil {
		return fmt.Errorf("add org concept seed: %w", err)
	}
	return nil
}

// RemoveOrgConceptSeed deletes the org-level concept seed with the given term.
func (g *OrgGraph) RemoveOrgConceptSeed(ctx context.Context, term string) error {
	_, err := g.db.ExecContext(ctx, `DELETE FROM org_concept_seeds WHERE term = ?`, term)
	if err != nil {
		return fmt.Errorf("remove org concept seed: %w", err)
	}
	return nil
}

// parseJSONArray parses a JSON string array like ["a","b"] into []string.
// Returns nil for empty or invalid input.
func parseJSONArray(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

