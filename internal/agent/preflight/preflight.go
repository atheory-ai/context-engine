// Package preflight is the first cognitive loop node.
// It validates the incoming query, resolves the project, and constructs
// the RunContext that carries all per-query state through the engine.
package preflight

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/storage/db"
	"github.com/atheory/context-engine/internal/storage/queries"
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
// It:
//  1. Looks up the project record in meta.db (ID "local" for Phase 1).
//  2. Checks that the project has been indexed (status != "unindexed").
//  3. Mounts the project's graph database.
//  4. Opens a session and turn in audit.db.
//  5. Returns a fully populated RunContext.
func (n *Node) Run(
	ctx context.Context,
	query string,
	cfg *config.Config,
	ch *core.AppChannels,
) (*core.RunContext, error) {
	if query == "" {
		return nil, fmt.Errorf("preflight: query is empty")
	}

	// ── 1. Look up project ────────────────────────────────────────────────
	project, err := queries.GetProject(ctx, n.dbRegistry.Meta(), "local")
	if err != nil {
		return nil, fmt.Errorf("preflight: project lookup: %w", err)
	}
	if project == nil {
		return nil, fmt.Errorf("preflight: project not initialized — run 'ce index' first")
	}
	if project.Status == "unindexed" {
		return nil, fmt.Errorf("preflight: project not yet indexed — run 'ce index' first")
	}

	// ── 2. Mount project graph DB ─────────────────────────────────────────
	localDBPath := filepath.Join(cfg.DataDir, "graphs", "local.db")
	if err := n.dbRegistry.Mount("local", localDBPath); err != nil {
		return nil, fmt.Errorf("preflight: mount project graph: %w", err)
	}

	// ── 3. Generate IDs ───────────────────────────────────────────────────
	runID := core.RunID(newID())
	turnID := core.TurnID(newID())
	sessionID := core.SessionID(newID())
	now := time.Now().UnixMilli()

	// ── 4. Open session in audit.db ───────────────────────────────────────
	if err := queries.InsertSession(ctx, n.dbRegistry.Audit(), queries.Session{
		ID:           string(sessionID),
		ActorID:      "local",
		Surface:      "cli",
		StartedAt:    now,
		LastActiveAt: now,
		Properties:   "{}",
	}); err != nil {
		return nil, fmt.Errorf("preflight: open session: %w", err)
	}

	// ── 5. Open turn in audit.db ──────────────────────────────────────────
	if err := queries.InsertTurn(ctx, n.dbRegistry.Audit(), queries.Turn{
		ID:         string(turnID),
		SessionID:  string(sessionID),
		Query:      sql.NullString{String: query, Valid: true},
		StartedAt:  now,
		Status:     "running",
		Properties: "{}",
	}); err != nil {
		return nil, fmt.Errorf("preflight: open turn: %w", err)
	}

	// ── 6. Build budget ───────────────────────────────────────────────────
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
		Content: fmt.Sprintf("session %s | run %s | project %s", sessionID, runID, project.Name),
	})

	return &core.RunContext{
		Ctx:       ctx,
		RunID:     runID,
		TurnID:    turnID,
		SessionID: sessionID,
		ProjectID: core.ProjectID("local"),
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
