// Package llm provides the LLM provider abstraction and implementations.
package llm

import "github.com/atheory-ai/context-engine/internal/core"

// CostTable maps model IDs to per-token costs in USD.
// Prices are per million tokens; divide by 1,000,000 to get per-token cost.
var CostTable = map[string]ModelCost{
	// Claude 4 family
	"claude-opus-4-6":           {InputPerMToken: 15.00, OutputPerMToken: 75.00},
	"claude-sonnet-4-6":         {InputPerMToken: 3.00, OutputPerMToken: 15.00},
	"claude-haiku-4-5-20251001": {InputPerMToken: 0.80, OutputPerMToken: 4.00},
	// Legacy aliases (keep for backward compatibility)
	"claude-3-5-sonnet-20241022": {InputPerMToken: 3.00, OutputPerMToken: 15.00},
	"claude-3-5-haiku-20241022":  {InputPerMToken: 0.80, OutputPerMToken: 4.00},
	"claude-3-opus-20240229":     {InputPerMToken: 15.00, OutputPerMToken: 75.00},
}

// ModelCost holds the pricing for a single model.
type ModelCost struct {
	InputPerMToken  float64 // USD per 1,000,000 input tokens
	OutputPerMToken float64 // USD per 1,000,000 output tokens
}

// EstimateCost calculates the approximate cost of a completion response.
func EstimateCost(resp core.CompletionResponse) float64 {
	c, ok := CostTable[resp.Model]
	if !ok {
		return 0
	}
	inputCost := float64(resp.TokensIn) * c.InputPerMToken / 1_000_000
	outputCost := float64(resp.TokensOut) * c.OutputPerMToken / 1_000_000
	return inputCost + outputCost
}

// EstimateTokensRough returns a rough token count for a string using the
// ~4 chars per token heuristic. Used when a provider doesn't supply an exact count.
func EstimateTokensRough(text string) int {
	return len(text)/4 + 1
}
