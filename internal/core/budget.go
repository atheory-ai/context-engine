package core

import (
	"fmt"
	"sync/atomic"
)

// BudgetRecorder is the write/query interface for token budget tracking.
// Defined in core so agent nodes don't need to import internal/runner.
// *Budget satisfies this interface.
type BudgetRecorder interface {
	Record(tokensIn, tokensOut int, costUSD float64)
	ShouldExit() bool
	ContextUsedPct() float64
}

// Budget tracks token usage for a single query run.
// All LLM calls in the run report their token usage here.
// Thread-safe — multiple tool goroutines may update concurrently.
type Budget struct {
	modelContextLimit int
	safetyMargin      float64

	tokensIn  int64 // atomic
	tokensOut int64 // atomic
	costUSD   int64 // atomic, stored as microdollars (×1,000,000)
}

// NewBudget creates a Budget for a model with the given context window size.
func NewBudget(modelContextLimit int) *Budget {
	limit := modelContextLimit
	if limit <= 0 {
		limit = 200_000 // safe default
	}
	return &Budget{
		modelContextLimit: limit,
		safetyMargin:      ContextWindowSafetyMargin,
	}
}

// Record adds token usage from a completed LLM call.
func (b *Budget) Record(tokensIn, tokensOut int, costUSD float64) {
	atomic.AddInt64(&b.tokensIn, int64(tokensIn))
	atomic.AddInt64(&b.tokensOut, int64(tokensOut))
	atomic.AddInt64(&b.costUSD, int64(costUSD*1_000_000))
}

// ContextUsedPct returns the fraction of the model's context window consumed.
func (b *Budget) ContextUsedPct() float64 {
	total := atomic.LoadInt64(&b.tokensIn) + atomic.LoadInt64(&b.tokensOut)
	if b.modelContextLimit <= 0 {
		return 0
	}
	return float64(total) / float64(b.modelContextLimit)
}

// ShouldExit returns true when the context window is approaching capacity.
// Called before each LLM call in the loop.
func (b *Budget) ShouldExit() bool {
	return b.ContextUsedPct() >= b.safetyMargin
}

// TotalCostUSD returns the total estimated cost in dollars.
func (b *Budget) TotalCostUSD() float64 {
	return float64(atomic.LoadInt64(&b.costUSD)) / 1_000_000
}

// TokensIn returns the total input tokens recorded.
func (b *Budget) TokensIn() int64 {
	return atomic.LoadInt64(&b.tokensIn)
}

// TokensOut returns the total output tokens recorded.
func (b *Budget) TokensOut() int64 {
	return atomic.LoadInt64(&b.tokensOut)
}

// Summary returns a cost emission for the ChanCost channel.
func (b *Budget) Summary(rc *RunContext) Emission {
	return Emission{
		RunID:   rc.RunID,
		TurnID:  rc.TurnID,
		Channel: ChanCost,
		Content: fmt.Sprintf("%.4f USD | %d tokens in | %d tokens out | %.1f%% context",
			b.TotalCostUSD(),
			b.TokensIn(),
			b.TokensOut(),
			b.ContextUsedPct()*100,
		),
	}
}
