package core

import "context"

// AgentContext is the per-query context passed to cognitive loop nodes.
// It provides everything a node needs without requiring an import of internal/runner,
// which would create an import cycle (runner → agent → runner).
//
// BudgetRecorder is defined in core/budget.go.
type AgentContext struct {
	// Go context — cancellation propagates to all goroutines.
	Ctx context.Context

	// Identity
	RunID     RunID
	TurnID    TurnID
	ProjectID ProjectID

	// Query is the raw user query text. Set by the runner, read by the Strategizer.
	Query string

	// Budget tracks token usage across the query. Thread-safe.
	Budget BudgetRecorder

	// Ch is the engine's output channels. All nodes write here.
	Ch *AppChannels

	// LoopIndex is the current cognitive loop iteration (0-based).
	LoopIndex int
}
