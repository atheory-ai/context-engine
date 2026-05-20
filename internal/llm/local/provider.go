// Package local is a stub implementation of core.LLMProvider for local/Ollama models.
// Not implemented in Phase 1.
package local

import (
	"context"
	"errors"

	"github.com/atheory-ai/context-engine/internal/core"
)

var errNotImplemented = errors.New("local provider: not implemented in Phase 1")

// Provider is a stub local LLMProvider.
type Provider struct{}

func (p *Provider) Complete(_ context.Context, _ core.CompletionRequest) (core.CompletionResponse, error) {
	return core.CompletionResponse{}, errNotImplemented
}

func (p *Provider) Stream(_ context.Context, _ core.CompletionRequest, ch chan<- string) error {
	close(ch)
	return errNotImplemented
}

func (p *Provider) ModelInfo() core.ModelInfo {
	return core.ModelInfo{ID: "local", ContextLimit: 32_000, Tier: core.TierFast}
}

func (p *Provider) EstimateTokens(text string) int {
	return len(text)/4 + 1
}
