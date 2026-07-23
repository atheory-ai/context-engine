package llm

import (
	"context"
	"testing"

	"github.com/atheory-ai/context-engine/internal/core"
)

type recordingProvider struct {
	request core.CompletionRequest
	info    core.ModelInfo
}

func (p *recordingProvider) Complete(_ context.Context, req core.CompletionRequest) (core.CompletionResponse, error) {
	p.request = req
	return core.CompletionResponse{}, nil
}

func (p *recordingProvider) Stream(_ context.Context, req core.CompletionRequest, ch chan<- string) error {
	p.request = req
	close(ch)
	return nil
}

func (p *recordingProvider) ModelInfo() core.ModelInfo { return p.info }
func (p *recordingProvider) EstimateTokens(string) int { return 0 }

func TestRouterCompleteUsesConfiguredStandardModelWhenRequestOmitsModel(t *testing.T) {
	provider := &recordingProvider{info: core.ModelInfo{ID: "provider-default", Tier: core.TierStandard}}
	router := NewRouter(Config{
		DefaultProvider: "openai",
		TierModels:      map[string]string{core.TierStandard: "gpt-5.6-luna"},
	}, ProviderEntry{Name: "openai", Provider: provider})

	if _, err := router.Complete(context.Background(), core.CompletionRequest{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := provider.request.Model; got != "gpt-5.6-luna" {
		t.Errorf("request model = %q, want configured standard model", got)
	}
}
