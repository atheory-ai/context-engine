// Package openai is a stub implementation of core.LLMProvider for OpenAI.
// Not implemented in Phase 1.
package openai

import (
	"context"
	"errors"

	"github.com/atheory/context-engine/internal/core"
)

var errNotImplemented = errors.New("openai provider: not implemented in Phase 1")

// Provider is a stub OpenAI LLMProvider.
type Provider struct{}

func (p *Provider) Complete(_ context.Context, _ core.CompletionRequest) (core.CompletionResponse, error) {
	return core.CompletionResponse{}, errNotImplemented
}

func (p *Provider) Stream(_ context.Context, _ core.CompletionRequest, ch chan<- string) error {
	close(ch)
	return errNotImplemented
}

func (p *Provider) ModelInfo() core.ModelInfo {
	return core.ModelInfo{ID: "gpt-4o", ContextLimit: 128_000, Tier: core.TierStandard}
}

func (p *Provider) EstimateTokens(text string) int {
	return len(text)/4 + 1
}
