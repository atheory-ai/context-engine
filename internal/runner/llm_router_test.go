package runner

import (
	"testing"

	"github.com/atheory-ai/context-engine/internal/config"
)

// TestBuildLLMRouter_SelectsProvider proves the config → router → provider wiring:
// the DefaultProvider name selects the matching backend, identified here by its
// default model. An empty provider falls back to Anthropic.
func TestBuildLLMRouter_SelectsProvider(t *testing.T) {
	cases := map[string]string{
		"openai": "gpt-4o",
		"local":  "llama3",
		"":       "claude-sonnet-4-6",
	}
	for provider, wantModel := range cases {
		cfg := &config.Config{}
		cfg.LLM.Provider = provider

		r := buildLLMRouter(cfg)
		if r == nil {
			t.Fatalf("provider %q: nil router", provider)
		}
		if got := r.ModelInfo().ID; got != wantModel {
			t.Errorf("provider %q: default model = %q, want %q", provider, got, wantModel)
		}
	}
}

func TestBuildLLMRouter_UsesConfiguredOpenAIStandardModel(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Provider = "openai"
	cfg.LLM.Models = map[string]string{"standard": "gpt-5.6-luna"}
	cfg.LLM.ReasoningEffort = "medium"

	r := buildLLMRouter(cfg)
	if got := r.ModelInfo().ID; got != "gpt-5.6-luna" {
		t.Fatalf("default model = %q, want gpt-5.6-luna", got)
	}
	if got := r.ModelForTier("standard"); got != "gpt-5.6-luna" {
		t.Errorf("standard model = %q, want gpt-5.6-luna", got)
	}
}
