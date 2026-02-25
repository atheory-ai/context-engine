// Package activation implements anchor resolution and activation spreading
// for the Context Engine's cognitive loop.
package activation

import (
	"context"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// ResolveAnchors resolves a slice of symbolic AnchorRefs into concrete Anchors.
// For each ref, it looks up the node by canonical ID in the substrate.
// Refs that don't resolve to any node are silently skipped.
func ResolveAnchors(
	ctx context.Context,
	sub core.SubstrateReader,
	projectID core.ProjectID,
	refs []core.AnchorRef,
) ([]core.Anchor, error) {
	if len(refs) == 0 {
		return nil, nil
	}

	var anchors []core.Anchor

	for _, ref := range refs {
		anchor, err := resolveOne(ctx, sub, projectID, ref)
		if err != nil {
			return nil, fmt.Errorf("resolve anchor %q: %w", ref.ID, err)
		}
		if anchor != nil {
			anchors = append(anchors, *anchor)
		}
	}

	return anchors, nil
}

// resolveOne resolves a single AnchorRef to an Anchor.
// Returns nil if the canonical ID cannot be found in the substrate.
func resolveOne(
	ctx context.Context,
	sub core.SubstrateReader,
	projectID core.ProjectID,
	ref core.AnchorRef,
) (*core.Anchor, error) {
	// Query the substrate for a node matching this canonical identifier.
	nodes, err := sub.Query(ctx, core.SubstrateQuery{
		ProjectID:  projectID,
		NodeTypes:  nodeTypesForRef(ref),
		Properties: map[string]string{"canonical_id": ref.ID},
		Limit:      1,
	})
	if err != nil {
		return nil, fmt.Errorf("query canonical id %q: %w", ref.ID, err)
	}

	// Fall back: try label match if canonical ID lookup returned nothing.
	if len(nodes) == 0 {
		nodes, err = sub.Query(ctx, core.SubstrateQuery{
			ProjectID: projectID,
			NodeTypes: nodeTypesForRef(ref),
			Limit:     1,
			Properties: map[string]string{"label": ref.ID},
		})
		if err != nil {
			return nil, fmt.Errorf("query label %q: %w", ref.ID, err)
		}
	}

	if len(nodes) == 0 {
		return nil, nil // not found — silently skip
	}

	node := nodes[0]

	// Load outbound edges for this node.
	edges, err := sub.Edges(ctx, node.ID, "")
	if err != nil {
		return nil, fmt.Errorf("load edges for %s: %w", node.ID, err)
	}

	// Seed activation: high confidence = 1.0, medium = 0.7, low = 0.4.
	seedActivation := confidenceToActivation(ref.Confidence)

	return &core.Anchor{
		Ref:        ref,
		Node:       &node,
		Edges:      edges,
		Activation: seedActivation,
	}, nil
}

// nodeTypesForRef returns the node types to search for a given anchor ref type.
// Empty slice = search all node types.
func nodeTypesForRef(ref core.AnchorRef) []string {
	switch ref.Type {
	case core.NodeTypeSymbol:
		return []string{core.NodeTypeSymbol}
	case core.NodeTypeNamespace:
		return []string{core.NodeTypeNamespace}
	case core.NodeTypeConcept:
		return []string{core.NodeTypeConcept}
	case core.NodeTypeFile:
		return []string{core.NodeTypeFile, core.NodeTypeDirectory}
	default:
		return nil // search all types
	}
}

// confidenceToActivation maps a confidence string to a seed activation value.
func confidenceToActivation(confidence string) float64 {
	switch confidence {
	case "high":
		return 1.0
	case "medium":
		return 0.7
	case "low":
		return 0.4
	default:
		return 0.7
	}
}
