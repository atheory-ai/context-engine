// Package anthropic implements core.LLMProvider for the Anthropic Messages API.
package anthropic

import "github.com/atheory-ai/context-engine/internal/core"

// Model IDs
const (
	ModelOpus4   = "claude-opus-4-6"
	ModelSonnet4 = "claude-sonnet-4-6"
	ModelHaiku45 = "claude-haiku-4-5-20251001"
)

// modelMeta holds metadata for a known Anthropic model.
type modelMeta struct {
	ContextLimit int
	Tier         string
}

// knownModels is the registry of supported Anthropic models.
var knownModels = map[string]modelMeta{
	ModelOpus4:   {ContextLimit: 200_000, Tier: core.TierThinking},
	ModelSonnet4: {ContextLimit: 200_000, Tier: core.TierStandard},
	ModelHaiku45: {ContextLimit: 200_000, Tier: core.TierFast},
	// Legacy
	"claude-3-5-sonnet-20241022": {ContextLimit: 200_000, Tier: core.TierStandard},
	"claude-3-5-haiku-20241022":  {ContextLimit: 200_000, Tier: core.TierFast},
	"claude-3-opus-20240229":     {ContextLimit: 200_000, Tier: core.TierThinking},
}

// ModelInfoFor returns core.ModelInfo for a given model ID.
// Falls back to a safe default if the model is unknown.
func ModelInfoFor(modelID string) core.ModelInfo {
	meta, ok := knownModels[modelID]
	if !ok {
		return core.ModelInfo{
			ID:           modelID,
			ContextLimit: 200_000,
			Tier:         core.TierStandard,
		}
	}
	return core.ModelInfo{
		ID:           modelID,
		ContextLimit: meta.ContextLimit,
		Tier:         meta.Tier,
	}
}
