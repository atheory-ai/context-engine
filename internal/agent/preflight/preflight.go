// Package preflight is the first cognitive loop node.
// It validates the incoming query, resolves the project, and constructs
// the RunContext that carries all per-query state through the engine.
package preflight

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/storage/db"
)

// Node is the preflight cognitive loop node.
type Node struct {
	dbRegistry *db.Registry
	llm        core.LLMProvider
}

// New creates a preflight Node.
func New(dbRegistry *db.Registry, llm core.LLMProvider) *Node {
	return &Node{
		dbRegistry: dbRegistry,
		llm:        llm,
	}
}

// Run validates the incoming query and constructs a RunContext.
//
// Phase 1 implementation:
//   - Skips project DB lookup (no meta.db required)
//   - Skips token validation
//   - Skips audit session/turn tracking
//   - Generates IDs from crypto/rand
//
// Phase 2 will add: project resolution, token validation, session open.
func (n *Node) Run(
	ctx context.Context,
	query string,
	cfg *config.Config,
	ch *core.AppChannels,
) (*core.RunContext, error) {
	if query == "" {
		return nil, fmt.Errorf("preflight: query is empty")
	}

	runID := core.RunID(newID())
	turnID := core.TurnID(newID())
	sessionID := core.SessionID(newID())
	projectID := core.ProjectID("local") // Phase 1: single local project

	// Determine context limit from the configured LLM.
	contextLimit := 0
	if n.llm != nil {
		contextLimit = n.llm.ModelInfo().ContextLimit
	}
	budget := core.NewBudget(contextLimit)

	maxLoops := cfg.Engine.MaxLoops
	if maxLoops <= 0 {
		maxLoops = core.DefaultMaxLoops
	}

	ch.Emit(core.Emission{
		RunID:   runID,
		TurnID:  turnID,
		Channel: core.ChanSystem,
		Content: fmt.Sprintf("session %s | run %s", sessionID, runID),
	})

	return &core.RunContext{
		Ctx:       ctx,
		RunID:     runID,
		TurnID:    turnID,
		SessionID: sessionID,
		ProjectID: projectID,
		Query:     query,
		Budget:    budget,
		MaxLoops:  maxLoops,
		Ch:        ch,
	}, nil
}

// newID generates a short random hex ID (16 hex chars = 8 bytes).
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
