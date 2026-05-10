package llm

import (
	"context"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
)

// LLMConfig is the configuration for the LLM layer.
// The config package reads ce.yaml and constructs this struct for the runner.
type LLMConfig struct {
	// DefaultProvider selects the LLM backend: "anthropic" | "openai" | "local"
	DefaultProvider string

	// Anthropic provider configuration.
	Anthropic AnthropicConfig

	// TierModels maps CE tiers to model IDs for the active provider.
	// If empty, provider defaults are used.
	TierModels map[string]string // core.TierFast → model ID, etc.
}

// AnthropicConfig holds configuration for the Anthropic provider.
type AnthropicConfig struct {
	APIKey       string
	BaseURL      string // optional, for proxy/testing
	DefaultModel string
}

// Router implements core.LLMProvider by routing requests to the configured
// backend provider and handling tier-based model selection.
type Router struct {
	provider   core.LLMProvider
	tierModels map[string]string // tier → model ID
	cfg        LLMConfig
}

// NewRouter creates a Router from the given configuration.
// The active provider is selected based on cfg.DefaultProvider.
func NewRouter(cfg LLMConfig, providers ...ProviderEntry) *Router {
	r := &Router{
		cfg:        cfg,
		tierModels: make(map[string]string),
	}

	// Build provider map from entries.
	providerMap := make(map[string]core.LLMProvider, len(providers))
	for _, p := range providers {
		providerMap[p.Name] = p.Provider
	}

	// Select the active provider.
	name := cfg.DefaultProvider
	if name == "" {
		name = "anthropic"
	}
	if p, ok := providerMap[name]; ok {
		r.provider = p
	} else if len(providerMap) > 0 {
		// Fall back to first registered provider.
		for _, p := range providerMap {
			r.provider = p
			break
		}
	}

	// Populate tier→model mapping: explicit config overrides provider defaults.
	for tier, model := range cfg.TierModels {
		r.tierModels[tier] = model
	}

	return r
}

// ProviderEntry is a named provider for registration with the Router.
type ProviderEntry struct {
	Name     string
	Provider core.LLMProvider
}

// ModelForTier returns the model ID to use for a given tier.
// Explicit config takes precedence over provider defaults.
func (r *Router) ModelForTier(tier string) string {
	if model, ok := r.tierModels[tier]; ok {
		return model
	}
	// Fall back to the active provider's default model.
	if r.provider != nil {
		return r.provider.ModelInfo().ID
	}
	return ""
}

// Complete sends a completion request to the active provider.
// If req.Model is empty, it is resolved via ModelForTier using the model's tier
// from the provider's model info.
func (r *Router) Complete(ctx context.Context, req core.CompletionRequest) (core.CompletionResponse, error) {
	if r.provider == nil {
		return core.CompletionResponse{}, fmt.Errorf("llm router: no provider configured")
	}
	if req.Model == "" {
		req.Model = r.provider.ModelInfo().ID
	}
	return r.provider.Complete(ctx, req)
}

// Stream sends a streaming completion request to the active provider.
func (r *Router) Stream(ctx context.Context, req core.CompletionRequest, ch chan<- string) error {
	if r.provider == nil {
		close(ch)
		return fmt.Errorf("llm router: no provider configured")
	}
	if req.Model == "" {
		req.Model = r.provider.ModelInfo().ID
	}
	return r.provider.Stream(ctx, req, ch)
}

// ModelInfo returns metadata about the default model of the active provider.
func (r *Router) ModelInfo() core.ModelInfo {
	if r.provider == nil {
		return core.ModelInfo{}
	}
	return r.provider.ModelInfo()
}

// EstimateTokens delegates to the active provider's token estimator.
func (r *Router) EstimateTokens(text string) int {
	if r.provider == nil {
		return EstimateTokensRough(text)
	}
	return r.provider.EstimateTokens(text)
}
