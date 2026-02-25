// Package reviewer is the convergence authority in the cognitive loop.
// It decides whether the loop has gathered enough information to synthesize
// a useful answer, and approves proposed substrate enrichments from tools.
package reviewer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/graph/substrate"
	"github.com/atheory/context-engine/internal/llm"
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
func (r *Node) buildSystemPrompt(rc *core.RunContext) string {
	return fmt.Sprintf(`You are the Reviewer for a codebase intelligence engine.
Your job is to decide whether the cognitive loop has gathered enough information
to answer the user's query, or whether more investigation is needed.

You do NOT answer the query. You evaluate the evidence collected so far.

Evaluate on three criteria:
1. Open queries answered: Are the specific sub-questions resolved by the current evidence?
2. Sufficient depth: Is there enough substrate context to write a useful answer?
3. Diminishing returns: Is the latest loop adding materially new information?

Convergence requires satisfaction on all three criteria.

Original query: %q

Open queries being investigated:
%s

────────────────────────────────────────────────────────────────────────────
OUTPUT FORMAT
────────────────────────────────────────────────────────────────────────────

After your analysis, output exactly these XML tags:

<converged>true</converged>
OR
<converged>false</converged>

If not converged, list the remaining open queries:
<open_queries>
  <open_query>specific unresolved question</open_query>
</open_queries>

If any tool proposed substrate enrichments worth approving, list them:
<enrichments>
  <enrichment action="promote" entity_type="edge" entity_id="ID" rationale="reason"/>
</enrichments>

Be decisive. If most questions are answered and the remaining gaps are minor,
converge and let the Synthesizer handle the partial answer.`,
		rc.Query,
		formatOpenQueries(rc.IR),
	)
}

// buildMessages builds the conversation messages for the Reviewer LLM call.
func (r *Node) buildMessages(rc *core.RunContext, loopEmissions []core.Emission) []core.Message {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Loop %d/%d evidence:\n\n", rc.CurrentLoop(), rc.MaxLoops))

	// Summarize tool emissions.
	actionCount := 0
	for _, e := range loopEmissions {
		if e.Channel == core.ChanAction || e.Channel == core.ChanMessage {
			sb.WriteString(fmt.Sprintf("- %s\n", e.Content))
			actionCount++
		}
	}
	if actionCount == 0 {
		sb.WriteString("(no tool emissions this loop)\n")
	}

	// Include accumulated emissions summary.
	sb.WriteString(fmt.Sprintf("\nTotal accumulated emissions: %d across %d loop(s).\n",
		len(rc.Emissions)+len(loopEmissions), rc.CurrentLoop()))

	return []core.Message{
		{Role: "user", Content: sb.String()},
	}
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

func formatOpenQueries(ir *core.IR) string {
	if ir == nil || len(ir.OpenQueries) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for i, q := range ir.OpenQueries {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q))
	}
	return sb.String()
}
