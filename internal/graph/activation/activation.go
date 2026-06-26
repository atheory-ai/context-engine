// Package activation implements the spreading activation model for the Context Engine.
// It resolves IR anchor refs to substrate nodes, propagates activation through the
// graph using a Dijkstra-style algorithm, and returns the top-K activated nodes
// as Anchors for tool fan-out.
//
// Import rule: activation imports core but NEVER imports runner.
// The runner passes individual values (ctx, projectID, ir, writer) instead of RunContext.
package activation

import (
	"context"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
)

// Node is the activation pass in the cognitive loop DAG.
type Node struct {
	substrate core.SubstrateReader
}

// NewNode creates an activation Node backed by the given substrate reader.
func NewNode(substrate core.SubstrateReader) *Node {
	return &Node{substrate: substrate}
}

// Run executes one activation pass for the current loop iteration.
// Returns the top-K activated nodes as Anchors, ready for tool fan-out.
//
// The writer parameter is used for persisting activation values to the write buffer.
// It is separate from the reader to allow the caller (runner/loop.go) to pass
// a combined ReadWriter without creating an import cycle through RunContext.
func (n *Node) Run(
	ctx context.Context,
	projectID core.ProjectID,
	ir *core.IR,
	writer core.SubstrateWriter,
) ([]core.Anchor, error) {
	kLimit := ir.KLimit
	if kLimit == 0 {
		kLimit = core.DefaultKLimit
	}

	// ── 1. Resolve anchor refs to substrate nodes ──────────────────────────
	seedNodes, err := resolveAnchors(ctx, n.substrate, projectID, ir.Anchors)
	if err != nil {
		return nil, fmt.Errorf("resolve anchors: %w", err)
	}

	if len(seedNodes) == 0 {
		// No seeds resolved — fall back to top-K by current activation state.
		topKActivated, err := n.substrate.GetTopKActivated(ctx, projectID, kLimit)
		if err != nil {
			return nil, fmt.Errorf("get top-k activated: %w", err)
		}
		if len(topKActivated) == 0 {
			return nil, nil
		}
		// Convert to activatedNode slice for enrichAnchors.
		topK := make([]activatedNode, len(topKActivated))
		for i, nwa := range topKActivated {
			topK[i] = activatedNode{nodeID: nwa.Node.ID, activation: nwa.Activation}
		}
		return enrichAnchors(ctx, n.substrate, projectID, topK)
	}

	// ── 2. Run spreading activation ────────────────────────────────────────
	activationMap, err := spreadActivation(ctx, n.substrate, projectID, seedNodes)
	if err != nil {
		return nil, fmt.Errorf("spread activation: %w", err)
	}

	// ── 3. Persist activation values to write buffer ───────────────────────
	// Fire-and-forget — errors are non-fatal (buffer may be full).
	for nodeID, activation := range activationMap {
		_ = writer.UpdateActivation(ctx, nodeID, activation) //nolint:errcheck // see comment above
	}

	// ── 4. Select top-K nodes ──────────────────────────────────────────────
	topK := selectTopK(activationMap, kLimit)

	// ── 5. Enrich top-K nodes with edges for tool context ─────────────────
	anchors, err := enrichAnchors(ctx, n.substrate, projectID, topK)
	if err != nil {
		return nil, fmt.Errorf("enrich anchors: %w", err)
	}

	return anchors, nil
}

// ResetActivation zeroes all activation values for a project.
// Called by the runner at the start of each new query.
func ResetActivation(
	ctx context.Context,
	projectID core.ProjectID,
	substrate core.SubstrateWriter,
) error {
	return substrate.ResetActivation(ctx, projectID)
}
