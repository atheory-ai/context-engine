// Package reviewer is the convergence authority in the cognitive loop.
// It decides whether the loop has gathered enough information to synthesize
// a useful answer, and approves proposed substrate enrichments from tools.
//
// Phase 1: stub implementation — converges after any iteration with emissions.
// Phase 2: LLM-based evaluation against open queries.
package reviewer

import (
	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/graph/substrate"
)

// ReviewResult is what the Reviewer returns to the cognitive loop.
type ReviewResult struct {
	// Converged is true when the loop has enough information to synthesize.
	Converged bool

	// UpdatedOpenQueries replaces IR.OpenQueries for the next iteration.
	// nil = no change.
	UpdatedOpenQueries []string

	// ApprovedEnrichments are substrate changes the loop should apply
	// via the write buffer before the next iteration.
	ApprovedEnrichments []core.Enrichment

	// Emissions are the reviewer's thinking output, sent to ChanThinking.
	Emissions []core.Emission
}

// Node is the Reviewer cognitive loop node.
type Node struct {
	llm      core.LLMProvider
	substrate *substrate.ReadWriter
}

// New creates a Reviewer node.
func New(llm core.LLMProvider, sub *substrate.ReadWriter) *Node {
	return &Node{llm: llm, substrate: sub}
}

// Run evaluates the current loop state and produces a convergence decision.
//
// Phase 1 stub: converges after any iteration that produced emissions,
// or when the forced-exit flag is set, or when max loops is reached.
// Phase 2 will implement full LLM-based open-query evaluation.
func (r *Node) Run(rc *core.RunContext, loopEmissions []core.Emission) (ReviewResult, error) {
	// Budget guard: if ForcedExit is already set, treat as converged.
	if rc.ForcedExit {
		return ReviewResult{Converged: true}, nil
	}

	// Phase 1 stub: converge if we have any emissions or hit the loop limit.
	if len(loopEmissions) > 0 || rc.CurrentLoop() >= rc.MaxLoops {
		return ReviewResult{Converged: true}, nil
	}

	return ReviewResult{Converged: false}, nil
}
