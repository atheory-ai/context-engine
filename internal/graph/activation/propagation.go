package activation

import (
	"context"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

const (
	// DecayFactor is the fraction of activation propagated to each neighbor per hop.
	// Lower = tighter activation cluster around seed nodes.
	DecayFactor = 0.5

	// ActivationThreshold is the minimum activation a node must have to spread
	// activation further. Prevents noise propagation.
	ActivationThreshold = 0.1
)

// Node is the activation pass node in the cognitive loop DAG.
// It resolves IR anchors, spreads activation through the graph,
// and returns the top-K activated nodes for the current iteration.
type Node struct {
	sub core.SubstrateAccessor
}

// NewNode creates an activation Node backed by the given substrate accessor.
func NewNode(sub core.SubstrateAccessor) *Node {
	return &Node{sub: sub}
}

// Run executes the activation pass for one cognitive loop iteration.
// It does not take a *runner.RunContext to avoid an import cycle —
// the runner passes individual values instead.
//
// Steps:
//  1. Resolve IR anchor refs to concrete substrate nodes (seeds)
//  2. Spread activation one hop through the graph using edge weights
//  3. Write all activation updates via the write buffer (fire-and-forget)
//  4. Return top-K anchors by activation for the fan-out node
func (n *Node) Run(ctx context.Context, projectID core.ProjectID, ir *core.IR) ([]core.Anchor, error) {
	// ── 1. Resolve anchor refs ──────────────────────────────────────────────
	seeds, err := ResolveAnchors(ctx, n.sub, projectID, ir.Anchors)
	if err != nil {
		return nil, fmt.Errorf("resolve anchors: %w", err)
	}

	// If no anchors resolved, fall back to top-K from current substrate state.
	if len(seeds) == 0 {
		k := ir.KLimit
		if k <= 0 {
			k = core.DefaultKLimit
		}
		return TopK(ctx, n.sub, projectID, k)
	}

	// ── 2. Build in-memory activation map ──────────────────────────────────
	activations := make(map[core.NodeID]float64, len(seeds)*4)
	nodes := make(map[core.NodeID]*core.Node, len(seeds)*4)
	edgeMap := make(map[core.NodeID][]core.Edge, len(seeds)*4)

	// Seed activation from resolved anchors.
	for i := range seeds {
		anchor := &seeds[i]
		if anchor.Node == nil {
			continue
		}
		activations[anchor.Node.ID] = anchor.Activation
		nodes[anchor.Node.ID] = anchor.Node
		edgeMap[anchor.Node.ID] = anchor.Edges
	}

	// ── 3. Single-hop spreading ─────────────────────────────────────────────
	// For each seed above threshold, spread activation to its neighbors.
	newActivations := make(map[core.NodeID]float64)

	for nodeID, srcActivation := range activations {
		if srcActivation < ActivationThreshold {
			continue
		}

		for _, edge := range edgeMap[nodeID] {
			spread := srcActivation * edge.Weight * DecayFactor
			if spread < ActivationThreshold {
				continue
			}
			newActivations[edge.TargetID] += spread
		}
	}

	// Merge spread activations (don't overwrite seed activations with lower values).
	for targetID, spread := range newActivations {
		if existing := activations[targetID]; spread > existing {
			activations[targetID] = spread
		}
		// Load the target node if we haven't seen it yet.
		if nodes[targetID] == nil {
			node, err := n.sub.Node(ctx, targetID)
			if err != nil {
				return nil, fmt.Errorf("load spread node %s: %w", targetID, err)
			}
			if node != nil {
				nodes[targetID] = node
				// Load edges for spread nodes too — they may be needed next iteration.
				edges, err := n.sub.Edges(ctx, targetID, "")
				if err != nil {
					return nil, fmt.Errorf("load spread edges %s: %w", targetID, err)
				}
				edgeMap[targetID] = edges
			}
		}
	}

	// ── 4. Write activation updates (fire-and-forget) ───────────────────────
	for nodeID, activation := range activations {
		if err := n.sub.UpdateActivation(ctx, nodeID, activation); err != nil {
			// Non-fatal — buffer full or similar. Log via error return is optional;
			// the loop continues with the computed activations regardless.
			_ = err
		}
	}

	// ── 5. Return top-K ─────────────────────────────────────────────────────
	k := ir.KLimit
	if k <= 0 {
		k = core.DefaultKLimit
	}

	return TopKFromMap(activations, nodes, edgeMap, k), nil
}
