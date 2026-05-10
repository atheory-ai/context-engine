// Package strategizer is the first cognitive loop node.
// It receives a user query and produces an IR — the compiled intent
// that drives the rest of the loop.
//
// The Strategizer does not answer the question. It plans the investigation.
package strategizer

import (
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/llm"
)

// Node is the Strategizer cognitive loop node.
// Construct with New(). Call Run() for each query.
type Node struct {
	llm        core.LLMProvider
	basePrompt string
	archPrompt string
	seeds      []core.ConceptSeed
	tools      []core.Tool

	// lastValidationError is set on the first attempt when IR validation fails.
	// It is appended to the user message on the second attempt.
	lastValidationError string
}

// New creates a Strategizer node.
func New(
	llmProvider core.LLMProvider,
	basePrompt string,
	archPrompt string,
	seeds []core.ConceptSeed,
	tools []core.Tool,
) *Node {
	return &Node{
		llm:        llmProvider,
		basePrompt: basePrompt,
		archPrompt: archPrompt,
		seeds:      seeds,
		tools:      tools,
	}
}

// Run executes the Strategizer for the given query context.
// Returns the compiled IR or an error after 2 attempts.
// On the first hard validation failure, the second attempt appends
// the validation error to the user message.
func (n *Node) Run(ac *core.AgentContext) (*core.IR, error) {
	n.lastValidationError = ""

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := n.llm.Complete(ac.Ctx, n.buildRequest(ac))
		if err != nil {
			return nil, fmt.Errorf("strategizer LLM: %w", err)
		}

		ac.Budget.Record(resp.TokensIn, resp.TokensOut, llm.EstimateCost(resp))
		n.logCall(ac, resp)

		ir, err := Extract(resp.Content)
		if err != nil {
			ac.Ch.Emit(core.Emission{
				RunID:   ac.RunID,
				TurnID:  ac.TurnID,
				Channel: core.ChanDebug,
				Source:  "strategizer",
				Content: fmt.Sprintf("strategizer extraction error (attempt %d): %v", attempt+1, err),
			})
			continue
		}

		if err := ir.Validate(); err != nil {
			if attempt == 0 {
				ac.Ch.Emit(core.Emission{
					RunID:   ac.RunID,
					TurnID:  ac.TurnID,
					Channel: core.ChanDebug,
					Source:  "strategizer",
					Content: fmt.Sprintf("strategizer IR invalid (attempt 1): %v — retrying", err),
				})
				n.lastValidationError = err.Error()
				continue
			}
			// Second failure — give up
			return nil, fmt.Errorf("strategizer produced invalid IR after 2 attempts: %w", err)
		}

		// Emit the compiled IR to the thinking channel for CE Studio
		irJSON, _ := json.Marshal(ir)
		ac.Ch.Emit(core.Emission{
			RunID:    ac.RunID,
			TurnID:   ac.TurnID,
			Channel:  core.ChanThinking,
			Source:   "strategizer",
			Content:  fmt.Sprintf("IR compiled: %s", string(irJSON)),
			Metadata: map[string]any{"ir": ir},
		})

		return ir, nil
	}
	return nil, fmt.Errorf("strategizer failed after 2 attempts")
}

func (n *Node) buildRequest(ac *core.AgentContext) core.CompletionRequest {
	systemPrompt := AssembleSystemPrompt(n.basePrompt, n.archPrompt, n.seeds, n.tools, nil)

	userContent := ac.Query
	if n.lastValidationError != "" {
		userContent += fmt.Sprintf(
			"\n\n[Previous attempt failed validation: %s. "+
				"Ensure your output includes at least one <anchor> and at least one <open_query>.]",
			n.lastValidationError,
		)
	}

	return core.CompletionRequest{
		System: systemPrompt,
		Messages: []core.Message{
			{Role: "user", Content: userContent},
		},
		MaxTokens: 4096,
	}
}

// logCall emits a debug record of the LLM call for observability.
func (n *Node) logCall(ac *core.AgentContext, resp core.CompletionResponse) {
	ac.Ch.Emit(core.Emission{
		RunID:   ac.RunID,
		TurnID:  ac.TurnID,
		Channel: core.ChanDebug,
		Source:  "strategizer",
		Content: fmt.Sprintf("LLM call: model=%s tokens_in=%d tokens_out=%d finish=%s",
			resp.Model, resp.TokensIn, resp.TokensOut, resp.FinishReason),
	})
}
