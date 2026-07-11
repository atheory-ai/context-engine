// Package openai implements core.LLMProvider for the OpenAI Chat Completions API.
package openai

import "github.com/atheory-ai/context-engine/internal/core"

// Model IDs.
const (
	ModelGPT4o     = "gpt-4o"
	ModelGPT4oMini = "gpt-4o-mini"
	ModelO3Mini    = "o3-mini"
)

// modelMeta holds metadata for a known OpenAI model.
type modelMeta struct {
	ContextLimit int
	Tier         string
}

// knownModels is the registry of supported OpenAI models.
var knownModels = map[string]modelMeta{
	ModelGPT4o:     {ContextLimit: 128_000, Tier: core.TierStandard},
	ModelGPT4oMini: {ContextLimit: 128_000, Tier: core.TierFast},
	ModelO3Mini:    {ContextLimit: 200_000, Tier: core.TierThinking},
	"gpt-4.1":      {ContextLimit: 1_000_000, Tier: core.TierStandard},
	"gpt-4-turbo":  {ContextLimit: 128_000, Tier: core.TierStandard},
	"o1":           {ContextLimit: 200_000, Tier: core.TierThinking},
	"o1-mini":      {ContextLimit: 128_000, Tier: core.TierThinking},
}

// ModelInfoFor returns core.ModelInfo for a given model ID, falling back to a
// safe standard-tier default when the model is unknown.
func ModelInfoFor(modelID string) core.ModelInfo {
	meta, ok := knownModels[modelID]
	if !ok {
		return core.ModelInfo{ID: modelID, ContextLimit: 128_000, Tier: core.TierStandard}
	}
	return core.ModelInfo{ID: modelID, ContextLimit: meta.ContextLimit, Tier: meta.Tier}
}
