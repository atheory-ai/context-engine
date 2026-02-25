// Package synthesizer produces the final answer from accumulated loop emissions.
// It handles both clean convergence (full synthesis) and forced exit (partial synthesis).
package synthesizer

import (
	"fmt"
	"strings"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/llm"
)

// Node is the Synthesizer cognitive loop node.
type Node struct {
	llm core.LLMProvider
}

// New creates a Synthesizer node.
func New(llm core.LLMProvider) *Node {
	return &Node{llm: llm}
}

// tierProvider is an optional extension of core.LLMProvider for tier-based model selection.
type tierProvider interface {
	ModelForTier(tier string) string
}

// Run produces the final answer from accumulated loop emissions.
// Delegates to runPartial if forced exit, runFull otherwise.
func (s *Node) Run(rc *core.RunContext) error {
	if rc.ForcedExit {
		return s.runPartial(rc)
	}
	return s.runFull(rc)
}

// RunDirect is used by the router for simple factual queries (direct path).
// Uses the same runFull path.
func (s *Node) RunDirect(rc *core.RunContext) error {
	return s.runFull(rc)
}

// runFull synthesizes when the Reviewer converged cleanly.
func (s *Node) runFull(rc *core.RunContext) error {
	model := ""
	if tp, ok := s.llm.(tierProvider); ok {
		model = tp.ModelForTier(core.TierStandard)
	}

	resp, err := s.llm.Complete(rc.Ctx, core.CompletionRequest{
		Model:     model,
		System:    s.buildSystemPrompt(rc, false),
		Messages:  s.buildMessages(rc, false),
		MaxTokens: 8192,
	})
	if err != nil {
		// Fallback to a basic synthesis from emission content.
		rc.Ch.Emit(core.Emission{
			RunID:    rc.RunID,
			TurnID:   rc.TurnID,
			Channel:  core.ChanMessage,
			Content:  s.fallbackMessage(rc),
			Markdown: true,
		})
		return nil
	}

	rc.Budget.Record(resp.TokensIn, resp.TokensOut, llm.EstimateCost(resp))

	rc.Ch.Emit(core.Emission{
		RunID:    rc.RunID,
		TurnID:   rc.TurnID,
		Channel:  core.ChanMessage,
		Content:  resp.Content,
		Markdown: true,
	})
	return nil
}

// runPartial synthesizes when the loop was cut short by a forced exit.
// Includes a clear notice that the answer is partial.
func (s *Node) runPartial(rc *core.RunContext) error {
	model := ""
	if tp, ok := s.llm.(tierProvider); ok {
		model = tp.ModelForTier(core.TierStandard)
	}

	resp, err := s.llm.Complete(rc.Ctx, core.CompletionRequest{
		Model:     model,
		System:    s.buildSystemPrompt(rc, true),
		Messages:  s.buildMessages(rc, true),
		MaxTokens: 4096,
	})
	if err != nil {
		rc.Ch.Emit(core.Emission{
			RunID:    rc.RunID,
			TurnID:   rc.TurnID,
			Channel:  core.ChanMessage,
			Content:  s.fallbackMessage(rc),
			Markdown: true,
		})
		return nil
	}

	rc.Budget.Record(resp.TokensIn, resp.TokensOut, llm.EstimateCost(resp))

	content := fmt.Sprintf("> **Partial answer** — %s\n\n%s",
		rc.ForcedExitReason, resp.Content)

	rc.Ch.Emit(core.Emission{
		RunID:    rc.RunID,
		TurnID:   rc.TurnID,
		Channel:  core.ChanMessage,
		Content:  content,
		Markdown: true,
	})
	return nil
}

// buildSystemPrompt returns the Synthesizer's system prompt.
func (s *Node) buildSystemPrompt(rc *core.RunContext, partial bool) string {
	base := `You are the Synthesizer for a codebase intelligence engine.
Your job is to produce the final answer to the developer's query
based on the evidence gathered by the cognitive loop.

Rules:
- Answer the original query directly and completely.
- Ground every claim in specific substrate evidence from the emissions.
- Reference specific functions, files, packages, and types by name.
- Format as clean Markdown with code references where appropriate.
- Be concise but thorough. The developer needs to understand, not just know.`

	if partial {
		base += fmt.Sprintf(`

This is a PARTIAL answer because the investigation was cut short: %s

Clearly state:
1. What was found in the investigation so far.
2. What specific questions remain unanswered.
3. What the developer can do to get a more complete answer (e.g., re-run with a more focused query).`,
			rc.ForcedExitReason)
	}

	return base
}

// buildMessages assembles the prompt messages for the Synthesizer.
func (s *Node) buildMessages(rc *core.RunContext, partial bool) []core.Message {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Original Query\n\n%s\n\n", rc.Query))

	if rc.IR != nil && len(rc.IR.OpenQueries) > 0 {
		sb.WriteString("## Investigation Objectives\n\n")
		for i, q := range rc.IR.OpenQueries {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("## Evidence Gathered (%d emissions across %d loop(s))\n\n",
		len(rc.Emissions), rc.CurrentLoop()))

	// Include action and message emissions as evidence.
	evidenceCount := 0
	for _, e := range rc.Emissions {
		if e.Channel == core.ChanAction || e.Channel == core.ChanMessage {
			sb.WriteString(fmt.Sprintf("**[%s]** %s\n\n", e.Source, e.Content))
			evidenceCount++
		}
	}
	if evidenceCount == 0 {
		sb.WriteString("(No tool evidence collected — answer based on query alone.)\n\n")
	}

	if partial && len(rc.IR.OpenQueries) > 0 {
		sb.WriteString("## Unresolved Questions\n\n")
		for _, q := range rc.IR.OpenQueries {
			sb.WriteString(fmt.Sprintf("- %s\n", q))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Your Task\n\nSynthesize the above evidence into a clear, grounded answer.")

	return []core.Message{
		{Role: "user", Content: sb.String()},
	}
}

// fallbackMessage is the last-resort response when synthesis fails.
func (s *Node) fallbackMessage(rc *core.RunContext) string {
	return fmt.Sprintf(
		"The engine collected %d emission(s) across %d loop(s) but could not synthesize a complete answer.\n\n"+
			"**Reason:** %s\n\n"+
			"Please try a more focused query.",
		len(rc.Emissions),
		rc.CurrentLoop(),
		func() string {
			if rc.ForcedExitReason != "" {
				return rc.ForcedExitReason
			}
			return "synthesis LLM call failed"
		}(),
	)
}
