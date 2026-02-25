// Package synthesizer produces the final answer from accumulated loop emissions.
// It handles both clean convergence (full synthesis) and forced exit (partial synthesis).
//
// Phase 1: stub implementation — emits a summary of collected emissions.
// Phase 2: LLM-based synthesis with grounded substrate evidence.
package synthesizer

import (
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// Node is the Synthesizer cognitive loop node.
type Node struct {
	llm core.LLMProvider
}

// New creates a Synthesizer node.
func New(llm core.LLMProvider) *Node {
	return &Node{llm: llm}
}

// Run produces the final answer from accumulated loop emissions.
// Delegates to runPartial if forced exit, runFull otherwise.
func (s *Node) Run(rc *core.RunContext) error {
	if rc.ForcedExit {
		return s.runPartial(rc)
	}
	return s.runFull(rc)
}

// RunDirect is used by the router for simple factual queries (Phase 2).
// Phase 1 delegates to runFull.
func (s *Node) RunDirect(rc *core.RunContext) error {
	return s.runFull(rc)
}

// runFull synthesizes when the Reviewer converged cleanly.
// Phase 1 stub: emits a summary of what was collected.
func (s *Node) runFull(rc *core.RunContext) error {
	rc.Ch.Emit(core.Emission{
		RunID:   rc.RunID,
		TurnID:  rc.TurnID,
		Channel: core.ChanMessage,
		Content: fmt.Sprintf(
			"[Synthesizer stub] Query: %q\n%d emissions across %d loop iteration(s).",
			rc.Query,
			len(rc.Emissions),
			rc.CurrentLoop(),
		),
	})
	return nil
}

// runPartial synthesizes when the loop was cut short by a forced exit.
// Includes a clear notice that the answer is partial.
func (s *Node) runPartial(rc *core.RunContext) error {
	rc.Ch.Emit(core.Emission{
		RunID:   rc.RunID,
		TurnID:  rc.TurnID,
		Channel: core.ChanMessage,
		Content: fmt.Sprintf(
			"> **Partial answer** — %s\n\n[Synthesizer stub] %d emissions across %d loop iteration(s).",
			rc.ForcedExitReason,
			len(rc.Emissions),
			rc.CurrentLoop(),
		),
		Markdown: true,
	})
	return nil
}
