package activation

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// resolveAnchors maps IR anchor refs to substrate nodes.
// Returns seed nodes with their initial activation values.
// Unresolvable anchors are silently skipped (non-fatal).
func resolveAnchors(
	ctx context.Context,
	substrate core.SubstrateReader,
	projectID core.ProjectID,
	anchorRefs []core.AnchorRef,
) ([]seedNode, error) {
	var seeds []seedNode

	for _, ref := range anchorRefs {
		initialActivation := confidenceToActivation(ref.Confidence)

		nodes, err := findNodesForRef(ctx, substrate, projectID, ref)
		if err != nil {
			return nil, err
		}

		if len(nodes) == 0 {
			// Try fuzzy resolution — suffix match on canonical ID
			nodes, err = fuzzyResolveRef(ctx, substrate, projectID, ref)
			if err != nil {
				return nil, err
			}
		}

		for _, node := range nodes {
			n := node // copy
			seeds = append(seeds, seedNode{
				node:       n,
				activation: initialActivation,
			})
		}
	}

	return seeds, nil
}

// seedNode pairs a resolved substrate node with its initial activation value.
type seedNode struct {
	node       core.Node
	activation float64
}

// confidenceToActivation maps a confidence string to a seed activation value.
func confidenceToActivation(confidence string) float64 {
	switch confidence {
	case "high":
		return core.ActivationHighConfidence
	case "medium":
		return core.ActivationMediumConfidence
	case "low":
		return core.ActivationLowConfidence
	default:
		return core.ActivationMediumConfidence
	}
}

// findNodesForRef looks up nodes by type and canonical ID.
func findNodesForRef(
	ctx context.Context,
	substrate core.SubstrateReader,
	projectID core.ProjectID,
	ref core.AnchorRef,
) ([]core.Node, error) {
	switch ref.Type {
	case "symbol":
		// Exact match on canonical ID
		node, err := substrate.GetNodeByCanonicalID(ctx, projectID, ref.ID)
		if err != nil || node == nil {
			return nil, err
		}
		return []core.Node{*node}, nil

	case "namespace":
		// All nodes whose canonical ID starts with this namespace
		return substrate.GetNodesByNamespacePrefix(ctx, projectID, ref.ID, 20)

	case "concept":
		// Concept nodes plus synonym expansion
		return substrate.GetConceptNodes(ctx, projectID, ref.ID)

	case "file":
		// File node plus all nodes extracted from this file
		return substrate.GetNodesForFile(ctx, projectID, ref.ID)

	default:
		return nil, nil
	}
}

// fuzzyResolveRef attempts suffix matching when exact match fails.
// Example: "ProcessPayment" matches "internal/billing:ProcessPayment"
func fuzzyResolveRef(
	ctx context.Context,
	substrate core.SubstrateReader,
	projectID core.ProjectID,
	ref core.AnchorRef,
) ([]core.Node, error) {
	if ref.Type != "symbol" {
		return nil, nil // fuzzy resolution only for symbols
	}

	// Try suffix match — "ProcessPayment" matches any canonicalID containing ":ProcessPayment"
	return substrate.GetNodesBySuffix(ctx, projectID, ref.ID, 5)
}
