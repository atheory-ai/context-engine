package activation

import (
	"context"
	"sort"

	"github.com/atheory/context-engine/internal/core"
)

// TopK retrieves the top-K anchors by activation from the substrate.
// Delegates to SubstrateReader.TopK which issues the indexed query.
func TopK(ctx context.Context, sub core.SubstrateReader, projectID core.ProjectID, k int) ([]core.Anchor, error) {
	if k <= 0 {
		k = core.DefaultKLimit
	}
	return sub.TopK(ctx, projectID, k)
}

// TopKFromMap selects the top-K anchors from an in-memory activation map.
// Used during propagation before writing to the database.
func TopKFromMap(activations map[core.NodeID]float64, nodes map[core.NodeID]*core.Node, edges map[core.NodeID][]core.Edge, k int) []core.Anchor {
	if k <= 0 {
		k = core.DefaultKLimit
	}

	// Collect all entries.
	type entry struct {
		id         core.NodeID
		activation float64
	}
	entries := make([]entry, 0, len(activations))
	for id, a := range activations {
		if a > 0 {
			entries = append(entries, entry{id: id, activation: a})
		}
	}

	// Sort descending by activation.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].activation > entries[j].activation
	})

	// Build anchors for top-K.
	if k > len(entries) {
		k = len(entries)
	}
	anchors := make([]core.Anchor, 0, k)
	for _, e := range entries[:k] {
		node := nodes[e.id]
		if node == nil {
			continue
		}
		anchors = append(anchors, core.Anchor{
			Ref: core.AnchorRef{
				Type: node.Type,
				ID:   node.CanonicalID,
			},
			Node:       node,
			Edges:      edges[e.id],
			Activation: e.activation,
		})
	}
	return anchors
}
