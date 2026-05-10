// Package reviewer is the convergence authority in the cognitive loop.
// It decides whether the loop has gathered enough information to synthesize
// a useful answer, and approves proposed substrate enrichments from tools.
package reviewer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/graph/substrate"
	"github.com/atheory-ai/context-engine/internal/llm"
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
	llm       core.LLMProvider
	substrate *substrate.ReadWriter
}

// New creates a Reviewer node.
func New(llm core.LLMProvider, sub *substrate.ReadWriter) *Node {
	return &Node{llm: llm, substrate: sub}
}

// tierProvider is an optional extension of core.LLMProvider for tier-based model selection.
type tierProvider interface {
	ModelForTier(tier string) string
}

// Run evaluates the current loop state and produces a convergence decision.
// Calls the LLM (fast tier) with a prompt built from the loop emissions and open queries.
// Parses the XML response to determine convergence, update open queries, and approve enrichments.
func (r *Node) Run(rc *core.RunContext, loopEmissions []core.Emission) (ReviewResult, error) {
	// Budget guard: if ForcedExit is already set, treat as converged.
	if rc.ForcedExit {
		return ReviewResult{Converged: true}, nil
	}

	// Budget check before LLM call.
	if rc.Budget.ShouldExit() {
		rc.ForcedExit = true
		rc.ForcedExitReason = "context window capacity before reviewer LLM call"
		return ReviewResult{Converged: true}, nil
	}

	// Select model for fast tier.
	model := ""
	if tp, ok := r.llm.(tierProvider); ok {
		model = tp.ModelForTier(core.TierFast)
	}

	resp, err := r.llm.Complete(rc.Ctx, core.CompletionRequest{
		Model:     model,
		System:    r.buildSystemPrompt(rc),
		Messages:  r.buildMessages(rc, loopEmissions),
		MaxTokens: 2048,
	})
	if err != nil {
		return ReviewResult{}, fmt.Errorf("reviewer LLM: %w", err)
	}

	rc.Budget.Record(resp.TokensIn, resp.TokensOut, llm.EstimateCost(resp))

	// Emit thinking channel so CE Studio can show reviewer reasoning.
	rc.Ch.Emit(core.Emission{
		RunID:   rc.RunID,
		TurnID:  rc.TurnID,
		Channel: core.ChanThinking,
		Source:  "reviewer",
		Content: resp.Content,
	})

	// Parse reviewer output; on failure treat as non-converged and continue.
	result, parseErr := r.parseResponse(rc, resp.Content)
	if parseErr != nil {
		rc.Ch.Emit(core.Emission{
			RunID:   rc.RunID,
			TurnID:  rc.TurnID,
			Channel: core.ChanWarning,
			Source:  "reviewer",
			Content: fmt.Sprintf("reviewer parse error: %v — continuing loop", parseErr),
		})
		return ReviewResult{Converged: false}, nil
	}

	return result, nil
}

// buildSystemPrompt returns the static system prompt for the Reviewer.
func (r *Node) buildSystemPrompt(_ *core.RunContext) string {
	return reviewerSystemPrompt
}

// buildMessages builds the conversation messages for the Reviewer LLM call.
// Includes the original query, IR state, prior-iteration findings, and this
// iteration's tool findings (ChanThinking emissions).
func (r *Node) buildMessages(rc *core.RunContext, loopEmissions []core.Emission) []core.Message {
	var sb strings.Builder

	sb.WriteString("## Original Query\n\n")
	sb.WriteString(rc.Query)
	sb.WriteString("\n\n")

	sb.WriteString("## Investigation Plan (IR)\n\n")
	if rc.IR != nil {
		sb.WriteString(fmt.Sprintf("Mode: %s\n", rc.IR.Mode))
		sb.WriteString(fmt.Sprintf("Loop: %d/%d\n\n", rc.CurrentLoop(), rc.MaxLoops))
		if len(rc.IR.OpenQueries) > 0 {
			sb.WriteString("Open queries:\n")
			for _, q := range rc.IR.OpenQueries {
				sb.WriteString(fmt.Sprintf("- %s\n", q))
			}
			sb.WriteString("\n")
		}
	}

	// Previous iterations' tool findings.
	prevThinking := filterThinkingEmissions(rc.Emissions)
	if len(prevThinking) > 0 {
		sb.WriteString("## Previous Iterations\n\n")
		for _, e := range prevThinking {
			sb.WriteString(e.Content)
			sb.WriteString("\n\n")
		}
	}

	// This iteration's tool findings.
	sb.WriteString("## This Iteration (tool findings)\n\n")
	thisThinking := filterThinkingEmissions(loopEmissions)
	if len(thisThinking) == 0 {
		sb.WriteString("(no tool findings this iteration)\n\n")
	} else {
		for _, e := range thisThinking {
			sb.WriteString(e.Content)
			sb.WriteString("\n\n")
		}
	}

	return []core.Message{
		{Role: "user", Content: sb.String()},
	}
}

// filterThinkingEmissions returns only ChanThinking emissions (tool findings).
func filterThinkingEmissions(emissions []core.Emission) []core.Emission {
	var result []core.Emission
	for _, e := range emissions {
		if e.Channel == core.ChanThinking {
			result = append(result, e)
		}
	}
	return result
}

// parseResponse extracts structured data from the Reviewer's XML output.
func (r *Node) parseResponse(rc *core.RunContext, content string) (ReviewResult, error) {
	convergedStr, err := extractTagText(content, "converged")
	if err != nil {
		return ReviewResult{}, fmt.Errorf("missing <converged> tag")
	}
	converged := strings.TrimSpace(convergedStr) == "true"

	result := ReviewResult{Converged: converged}

	// Extract updated open queries (only relevant when not converged).
	if !converged {
		result.UpdatedOpenQueries = extractOpenQueryList(content)
	}

	// Extract enrichment approvals (optional, best-effort).
	result.ApprovedEnrichments = extractEnrichments(content, rc)

	return result, nil
}

// ── XML helpers ───────────────────────────────────────────────────────────────

func extractTagText(s, tag string) (string, error) {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	if start == -1 {
		return "", fmt.Errorf("tag <%s> not found", tag)
	}
	start += len(open)
	end := strings.Index(s[start:], close)
	if end == -1 {
		return "", fmt.Errorf("closing tag </%s> not found", tag)
	}
	return s[start : start+end], nil
}

func extractOpenQueryList(s string) []string {
	container, err := extractTagText(s, "open_queries")
	if err != nil {
		return nil
	}
	oqRegex := regexp.MustCompile(`<open_query>(.*?)</open_query>`)
	matches := oqRegex.FindAllStringSubmatch(container, -1)
	var queries []string
	for _, m := range matches {
		q := strings.TrimSpace(m[1])
		if q != "" {
			queries = append(queries, q)
		}
	}
	return queries
}

func extractEnrichments(s string, rc *core.RunContext) []core.Enrichment {
	container, err := extractTagText(s, "enrichments")
	if err != nil {
		return nil
	}
	enrichRegex := regexp.MustCompile(`<enrichment\s+([^>]*?)(?:/>|>)`)
	attrRegex := regexp.MustCompile(`(\w[\w-]*)="([^"]*)"`)

	var enrichments []core.Enrichment
	for _, m := range enrichRegex.FindAllStringSubmatch(container, -1) {
		attrs := make(map[string]string)
		for _, am := range attrRegex.FindAllStringSubmatch(m[1], -1) {
			attrs[am[1]] = am[2]
		}
		action, _ := attrs["action"]
		entityType, _ := attrs["entity_type"]
		entityID, _ := attrs["entity_id"]
		rationale, _ := attrs["rationale"]
		if action == "" || entityID == "" {
			continue
		}
		enrichments = append(enrichments, core.Enrichment{
			RunID:      rc.RunID,
			TurnID:     rc.TurnID,
			LoopIndex:  rc.CurrentLoop(),
			EntityType: entityType,
			EntityID:   entityID,
			Action:     action,
			Rationale:  rationale,
		})
	}
	return enrichments
}
