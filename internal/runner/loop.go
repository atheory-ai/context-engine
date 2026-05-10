package runner

import (
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/graph/activation"
)

// runLoop executes the activation → fan-out → reviewer cycle until convergence
// or a forced exit condition is met.
func (d *dag) runLoop(rc *core.RunContext) error {
	for {
		loopIdx := rc.IncrementLoop()

		rc.Ch.Emit(core.Emission{
			RunID:   rc.RunID,
			TurnID:  rc.TurnID,
			Channel: core.ChanSystem,
			Content: fmt.Sprintf("loop %d/%d", loopIdx, rc.MaxLoops),
		})

		// ── Exit condition 1: iteration limit ─────────────────────────────
		if loopIdx > rc.MaxLoops {
			rc.ForcedExit = true
			rc.ForcedExitReason = fmt.Sprintf(
				"loop limit reached (%d/%d)", loopIdx-1, rc.MaxLoops,
			)
			return nil
		}

		// ── Exit condition 2: context window capacity ──────────────────────
		if rc.Budget.ShouldExit() {
			rc.ForcedExit = true
			rc.ForcedExitReason = fmt.Sprintf(
				"context window at %.0f%% capacity", rc.Budget.ContextUsedPct()*100,
			)
			return nil
		}

		// ── Activation pass ───────────────────────────────────────────────
		anchors, err := d.activation.Run(rc.Ctx, rc.ProjectID, rc.IR, d.engine.substrate)
		if err != nil {
			return fmt.Errorf("loop %d activation: %w", loopIdx, err)
		}
		rc.SetAnchors(anchors)

		// ── Fan-out ───────────────────────────────────────────────────────
		toolEmissions, err := d.fanout.Run(rc)
		if err != nil {
			return fmt.Errorf("loop %d fanout: %w", loopIdx, err)
		}
		rc.AppendEmissions(toolEmissions)

		// Check budget again after fan-out (tools may have made LLM calls).
		if rc.Budget.ShouldExit() {
			rc.ForcedExit = true
			rc.ForcedExitReason = fmt.Sprintf(
				"context window at %.0f%% capacity after tool execution",
				rc.Budget.ContextUsedPct()*100,
			)
			return nil
		}

		// ── Reviewer ──────────────────────────────────────────────────────
		review, err := d.reviewer.Run(rc, toolEmissions)
		if err != nil {
			return fmt.Errorf("loop %d reviewer: %w", loopIdx, err)
		}

		// Apply approved enrichments via the substrate writer.
		for _, enrichment := range review.ApprovedEnrichments {
			if err := d.engine.substrate.ApplyEnrichment(rc.Ctx, enrichment); err != nil {
				rc.Ch.Emit(core.Emission{
					RunID:   rc.RunID,
					TurnID:  rc.TurnID,
					Channel: core.ChanWarning,
					Content: fmt.Sprintf("enrichment apply: %v", err),
				})
			}
		}

		// Emit reviewer thinking.
		rc.AppendEmissions(review.Emissions)

		// ── Hebbian weight updates ─────────────────────────────────────────
		if err := activation.UpdateWeights(
			rc.Ctx,
			rc.ProjectID,
			rc.ReadAnchors(),
			d.engine.substrate,
		); err != nil {
			rc.Ch.Emit(core.Emission{
				RunID:   rc.RunID,
				TurnID:  rc.TurnID,
				Channel: core.ChanWarning,
				Content: fmt.Sprintf("hebbian update: %v", err),
			})
		}

		// ── Convergence check ─────────────────────────────────────────────
		if review.Converged {
			return nil // clean exit — proceed to Synthesizer
		}

		// ── Reset activation before next iteration ────────────────────────
		if err := activation.ResetActivation(rc.Ctx, rc.ProjectID, d.engine.substrate); err != nil {
			return fmt.Errorf("reset activation: %w", err)
		}

		// Update open queries for next iteration based on Reviewer guidance.
		if len(review.UpdatedOpenQueries) > 0 {
			rc.IR.OpenQueries = review.UpdatedOpenQueries
		}
	}
}
