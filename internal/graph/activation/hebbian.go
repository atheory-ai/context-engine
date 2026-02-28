package activation

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// UpdateWeights applies Hebbian learning after a cognitive loop iteration.
// Called by the runner after the Reviewer pass, before the next iteration.
//
// Hebbian rule: "neurons that fire together, wire together"
// Edges between co-activated nodes get stronger.
// All other edges for the project get slightly weaker (prevents runaway growth).
func UpdateWeights(
	ctx context.Context,
	projectID core.ProjectID,
	anchors []core.Anchor,
	substrate core.SubstrateWriter,
) error {
	if len(anchors) == 0 {
		return nil
	}

	// Build set of activated node IDs for fast lookup.
	activatedIDs := make(map[core.NodeID]float64)
	for _, anchor := range anchors {
		if anchor.Node != nil {
			activatedIDs[anchor.Node.ID] = anchor.Activation
		}
	}

	// Collect edges between co-activated nodes (to strengthen).
	coActivatedEdges := make(map[core.EdgeID]coActivation)

	for _, anchor := range anchors {
		if anchor.Node == nil {
			continue
		}
		for _, edge := range anchor.Edges {
			sourceActivation, sourceActive := activatedIDs[edge.SourceID]
			targetActivation, targetActive := activatedIDs[edge.TargetID]

			if sourceActive && targetActive {
				// Weight increase proportional to the product of activations
				// (classic Hebbian rule).
				strengthening := core.HebbianLearningRate *
					sourceActivation * targetActivation

				coActivatedEdges[edge.ID] = coActivation{
					edge:          edge,
					strengthening: strengthening,
				}
			}
		}
	}

	// Apply weight updates via write buffer.
	for _, ca := range coActivatedEdges {
		newWeight := clamp(
			ca.edge.Weight+ca.strengthening,
			core.MinEdgeWeight,
			core.MaxEdgeWeight,
		)

		_ = substrate.UpdateEdgeWeight(ctx, core.WeightUpdate{
			EdgeID:            ca.edge.ID,
			ProjectID:         projectID,
			NewWeight:         newWeight,
			CoActivationDelta: 1,
			SourceClass:       updateSourceClass(string(ca.edge.SourceClass), newWeight),
		})
	}

	// Decay all other edges for this project.
	// Fire-and-forget — errors are non-fatal.
	_ = substrate.DecayEdgeWeights(ctx, projectID, core.HebbianDecayRate)

	return nil
}

type coActivation struct {
	edge          core.EdgeWithWeight
	strengthening float64
}

// updateSourceClass promotes edges that reach high weight thresholds.
// speculative → associative when weight reaches 0.4
// associative → structural requires human confirmation (not automatic).
func updateSourceClass(current string, newWeight float64) string {
	if current == "speculative" && newWeight >= 0.4 {
		return "associative"
	}
	return current
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
