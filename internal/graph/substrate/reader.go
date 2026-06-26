// Package substrate implements SubstrateReader and SubstrateWriter against
// the SQLite property graph databases managed by internal/storage.
package substrate

import (
	"context"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
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

// GetNode retrieves a single node by ID from a specific project.
func (r *Reader) GetNode(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID) (*core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNode(ctx, db, string(nodeID))
}

// GetNodeByCanonicalID retrieves a node by project ID and canonical identifier.
func (r *Reader) GetNodeByCanonicalID(ctx context.Context, projectID core.ProjectID, canonicalID string) (*core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNodeByCanonical(ctx, db, string(projectID), canonicalID)
}

// GetNodesByNamespacePrefix returns nodes whose canonical_id starts with prefix.
func (r *Reader) GetNodesByNamespacePrefix(ctx context.Context, projectID core.ProjectID, prefix string, limit int) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNodesByNamespacePrefix(ctx, db, string(projectID), prefix, limit)
}

// GetConceptNodes returns concept-type nodes for a project.
// If term is empty, returns all concept nodes.
// If term is non-empty, filters by canonical_id or label containing term.
func (r *Reader) GetConceptNodes(ctx context.Context, projectID core.ProjectID, term string) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetConceptNodesQuery(ctx, db, string(projectID), term)
}

// GetNodesForFile returns the file node plus all nodes extracted from a file.
func (r *Reader) GetNodesForFile(ctx context.Context, projectID core.ProjectID, filePath string) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNodesForFile(ctx, db, string(projectID), filePath)
}

// GetNodesBySuffix returns nodes whose canonical_id ends with (or contains) suffix.
// Used for fuzzy anchor resolution.
func (r *Reader) GetNodesBySuffix(ctx context.Context, projectID core.ProjectID, suffix string, limit int) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNodesBySuffix(ctx, db, string(projectID), suffix, limit)
}

// GetTopKActivated returns the top-K nodes by activation level, paired with their activation values.
func (r *Reader) GetTopKActivated(ctx context.Context, projectID core.ProjectID, k int) ([]core.NodeWithActivation, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetTopKActivated(ctx, db, string(projectID), k)
}

// GetEdgesFrom returns all outbound edges from a node, with weight metadata.
func (r *Reader) GetEdgesFrom(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID) ([]core.EdgeWithWeight, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetEdgesFromWithWeight(ctx, db, string(projectID), string(nodeID))
}

// GetEdgesTo returns all inbound edges to a node, with weight metadata.
func (r *Reader) GetEdgesTo(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID) ([]core.EdgeWithWeight, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetEdgesToWithWeight(ctx, db, string(projectID), string(nodeID))
}

// GetEdgesBetween returns all edges between two nodes in either direction.
func (r *Reader) GetEdgesBetween(ctx context.Context, projectID core.ProjectID, sourceID, targetID core.NodeID) ([]core.EdgeWithWeight, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetEdgesBetweenWithWeight(ctx, db, string(projectID), string(sourceID), string(targetID))
}

// GetConceptSeeds returns all concept seeds for a project.
func (r *Reader) GetConceptSeeds(ctx context.Context, projectID core.ProjectID) ([]core.ConceptSeed, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetConceptSeeds(ctx, db, string(projectID))
}

// GetOrgConceptSeeds returns all org-level concept seeds.
func (r *Reader) GetOrgConceptSeeds(ctx context.Context) ([]core.ConceptSeed, error) {
	db, err := r.dbProvider.GraphDB("org")
	if err != nil {
		return nil, fmt.Errorf("org graph db: %w", err)
	}
	return queries.GetOrgConceptSeeds(ctx, db)
}

// ── Tool-specific queries ─────────────────────────────────────────────────

// GetCallers returns nodes that call the given node, up to maxDepth hops.
func (r *Reader) GetCallers(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID, maxDepth int) ([]core.NodeWithActivation, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetCallers(ctx, db, string(projectID), string(nodeID), maxDepth)
}

// GetCallees returns nodes that the given node calls, up to maxDepth hops.
func (r *Reader) GetCallees(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID, maxDepth int) ([]core.NodeWithActivation, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetCallees(ctx, db, string(projectID), string(nodeID), maxDepth)
}

// GetReferences returns all nodes that reference the given node.
func (r *Reader) GetReferences(ctx context.Context, projectID core.ProjectID, nodeID core.NodeID) ([]core.ReferenceResult, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetReferences(ctx, db, string(projectID), string(nodeID))
}

// FindInOrgGraph searches the org graph for nodes matching the given canonical ID and type.
func (r *Reader) FindInOrgGraph(ctx context.Context, canonicalID string, nodeType string) ([]core.OrgMatch, error) {
	db, err := r.dbProvider.GraphDB("org")
	if err != nil {
		// Org graph may not exist yet — return empty rather than error.
		return nil, nil
	}
	return queries.FindInOrgGraph(ctx, db, canonicalID, nodeType)
}

// GetConceptImplementors returns nodes that implement or relate to a concept node.
func (r *Reader) GetConceptImplementors(ctx context.Context, projectID core.ProjectID, conceptNodeID core.NodeID) ([]core.NodeWithActivation, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetConceptImplementors(ctx, db, string(projectID), string(conceptNodeID))
}

// GetConceptSeed returns the concept seed for the given term.
func (r *Reader) GetConceptSeed(ctx context.Context, projectID core.ProjectID, term string) (*core.ConceptSeed, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetConceptSeedByTerm(ctx, db, string(projectID), term)
}

// GetFileNode returns the file-type node for the given file path.
func (r *Reader) GetFileNode(ctx context.Context, projectID core.ProjectID, filePath string) (*core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetFileNode(ctx, db, string(projectID), filePath)
}

// GetFileImports returns nodes imported by the given file node.
func (r *Reader) GetFileImports(ctx context.Context, projectID core.ProjectID, fileNodeID core.NodeID) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetFileImports(ctx, db, string(projectID), string(fileNodeID))
}

// GetNamespaceMembers returns all nodes directly contained in or defined by a namespace.
func (r *Reader) GetNamespaceMembers(ctx context.Context, projectID core.ProjectID, namespaceNodeID core.NodeID) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNamespaceMembers(ctx, db, string(projectID), string(namespaceNodeID))
}

// GetNamespaceDependencies returns namespaces/files that this namespace imports.
func (r *Reader) GetNamespaceDependencies(ctx context.Context, projectID core.ProjectID, namespaceNodeID core.NodeID) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNamespaceDependencies(ctx, db, string(projectID), string(namespaceNodeID))
}

// GetNamespaceDependents returns namespaces/files that import this namespace.
func (r *Reader) GetNamespaceDependents(ctx context.Context, projectID core.ProjectID, namespaceNodeID core.NodeID) ([]core.Node, error) {
	db, err := r.dbProvider.GraphDB(string(projectID))
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return queries.GetNamespaceDependents(ctx, db, string(projectID), string(namespaceNodeID))
}
