package activation

import (
	"context"
	"fmt"
	"sort"

	"github.com/atheory/context-engine/internal/core"
)

// activatedNode pairs a node ID with its activation value.
type activatedNode struct {
	nodeID     core.NodeID
	activation float64
}

// selectTopK returns the K nodes with highest activation values.
// Returns fewer than K if fewer nodes were activated.
func selectTopK(activationMap map[core.NodeID]float64, k int) []activatedNode {
	nodes := make([]activatedNode, 0, len(activationMap))
	for nodeID, activation := range activationMap {
		nodes = append(nodes, activatedNode{nodeID, activation})
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].activation > nodes[j].activation
	})

	if len(nodes) > k {
		nodes = nodes[:k]
	}
	return nodes
}

// enrichAnchors fetches full node data and connected edges for the top-K nodes.
// Returns Anchors ready for tool execution.
func enrichAnchors(
	ctx context.Context,
	substrate core.SubstrateReader,
	projectID core.ProjectID,
	topK []activatedNode,
) ([]core.Anchor, error) {
	anchors := make([]core.Anchor, 0, len(topK))

	for _, activated := range topK {
		node, err := substrate.GetNode(ctx, projectID, activated.nodeID)
		if err != nil || node == nil {
			continue // node may have been deleted — skip
		}

		// Fetch outbound edges for context.
		edges, err := substrate.GetEdgesFrom(ctx, projectID, activated.nodeID)
		if err != nil {
			return nil, fmt.Errorf("get edges for anchor %s: %w", activated.nodeID, err)
		}

		// Also fetch incoming edges — tools need both directions.
		inEdges, err := substrate.GetEdgesTo(ctx, projectID, activated.nodeID)
		if err != nil {
			return nil, fmt.Errorf("get in-edges for anchor %s: %w", activated.nodeID, err)
		}

		allEdges := append(edges, inEdges...)

		anchors = append(anchors, core.Anchor{
			Ref: core.AnchorRef{
				Type:       node.Type,
				ID:         node.CanonicalID,
				Confidence: activationToConfidence(activated.activation),
			},
			Node:       node,
			Edges:      allEdges,
			Activation: activated.activation,
		})
	}

	return anchors, nil
}

// activationToConfidence converts an activation value to a confidence string.
func activationToConfidence(activation float64) string {
	switch {
	case activation >= core.ActivationHighConfidence*0.8:
		return "high"
	case activation >= core.ActivationMediumConfidence*0.8:
		return "medium"
	default:
		return "low"
	}
}
