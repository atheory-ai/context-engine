// Package shaper turns a natural-language description into a validated IIR
// FunctionIntent using an LLM. It is the one model-facing piece of the IIR
// feature; internal/iir itself stays deterministic and model-free.
//
// The model produces a candidate intent; it is never trusted raw. Every
// response is run through iir.ParseIntentJSON (the same deterministic validation
// hand-authored IIR gets), and a bad response is fed back to the model and
// retried — the pattern the Strategizer uses for its IR.
package shaper

import (
	"context"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/iir"
)

// maxAttempts is the number of model calls before giving up. The second attempt
// re-prompts with the previous validation error so the model can self-correct.
const maxAttempts = 2

// maxTokens caps the shaping response. A single FunctionIntent is small.
const maxTokens = 2048

// Shaper turns natural language into a FunctionIntent via an LLM.
type Shaper struct {
	llm core.LLMProvider
}

// New creates a Shaper backed by the given model provider.
func New(llm core.LLMProvider) *Shaper {
	return &Shaper{llm: llm}
}

// Shape converts a natural-language description into a validated FunctionIntent.
// It retries up to maxAttempts, feeding a parse/validation error back to the
// model between attempts. Returns an error if the model never produces valid
// IIR.
func (s *Shaper) Shape(ctx context.Context, description string) (*iir.FunctionIntent, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("shaper: no LLM provider configured")
	}
	if description == "" {
		return nil, fmt.Errorf("shaper: empty description")
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := s.llm.Complete(ctx, core.CompletionRequest{
			System:    systemPrompt,
			Messages:  []core.Message{{Role: "user", Content: userPrompt(description, lastErr)}},
			MaxTokens: maxTokens,
		})
		if err != nil {
			return nil, fmt.Errorf("shaper LLM: %w", err)
		}

		raw, err := extractJSON(resp.Content)
		if err != nil {
			lastErr = err
			continue
		}
		intent, err := iir.ParseIntentJSON(raw)
		if err != nil {
			lastErr = err
			continue
		}
		return intent, nil
	}

	return nil, fmt.Errorf("shaper: model did not produce valid IIR after %d attempts: %w", maxAttempts, lastErr)
}
