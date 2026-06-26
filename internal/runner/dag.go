package runner

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/atheory-ai/context-engine/internal/agent/preflight"
	"github.com/atheory-ai/context-engine/internal/agent/reviewer"
	"github.com/atheory-ai/context-engine/internal/agent/strategizer"
	"github.com/atheory-ai/context-engine/internal/agent/synthesizer"
	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/graph/activation"
	"github.com/atheory-ai/context-engine/internal/storage/queries"
)

// dag holds references to all constructed cognitive loop nodes.
// Built once per Engine instance; Run() is called once per query.
type dag struct {
	preflight   *preflight.Node
	strategizer *strategizer.Node
	activation  *activation.Node
	fanout      *fanoutNode
	reviewer    *reviewer.Node
	synthesizer *synthesizer.Node

	engine *Engine
}

// routeDecision is the result of the router node.
type routeDecision int

const (
	routeCognitive routeDecision = iota
	routeDirect
)

// buildDAG constructs the DAG from the engine's components.
// Called once per Engine.Query() call — not per-query, per engine.
func (e *Engine) buildDAG() *dag {
	tools := buildToolList(e.substrate, e.plugins)
	seeds := e.plugins.ConceptSeeds()

	// Phase 1: strategizer built with empty project prompts.
	// Phase 2: load base_prompt and arch_prompt from project DB.
	strat := strategizer.New(e.llmRouter, "", "", seeds, tools)

	return &dag{
		preflight:   preflight.New(e.dbRegistry, e.llmRouter),
		strategizer: strat,
		activation:  activation.NewNode(e.substrate.Reader),
		fanout:      &fanoutNode{tools: tools},
		reviewer:    reviewer.New(e.llmRouter, e.substrate),
		synthesizer: synthesizer.New(e.llmRouter),
		engine:      e,
	}
}

// Run executes the full DAG for a single query using the engine's own channels.
func (d *dag) Run(ctx context.Context, query string) error {
	return d.run(ctx, query, d.engine.channels)
}

// RunWithChannels executes the full DAG using caller-supplied channels.
// Used by QueryWithChannels so the WebSocket handler can provide its own channel set.
func (d *dag) RunWithChannels(ctx context.Context, query string, ch *core.AppChannels) error {
	return d.run(ctx, query, ch)
}

// run is the shared DAG execution path.
func (d *dag) run(ctx context.Context, query string, ch *core.AppChannels) (retErr error) {
	// ── 1. Pre-flight ──────────────────────────────────────────────────────
	rc, err := d.preflight.Run(ctx, query, d.engine.cfg, ch)
	if err != nil {
		return fmt.Errorf("preflight: %w", err)
	}

	// Always close the session and turn on exit, even on error.
	defer func() {
		bg := context.Background()
		now := time.Now().UnixMilli()
		status := "complete"
		if retErr != nil {
			status = "error"
		}
		if err := queries.UpdateTurn(bg, d.engine.dbRegistry.Audit(), string(rc.TurnID),
			sql.NullInt64{Int64: now, Valid: true},
			sql.NullInt64{Int64: int64(rc.CurrentLoop()), Valid: true},
			status,
		); err != nil && retErr == nil {
			retErr = fmt.Errorf("update turn teardown: %w", err)
		}
		if err := queries.EndSession(bg, d.engine.dbRegistry.Audit(), string(rc.SessionID), now); err != nil && retErr == nil {
			retErr = fmt.Errorf("end session teardown: %w", err)
		}
		if err := d.engine.dbRegistry.Unmount(string(rc.ProjectID)); err != nil && retErr == nil {
			retErr = fmt.Errorf("unmount project db: %w", err)
		}
	}()

	// ── 2. Router ──────────────────────────────────────────────────────────
	// Phase 1: always take the cognitive path.
	// Phase 2: route simple factual queries to the direct path.
	route := d.route(rc)
	if route == routeDirect {
		return d.synthesizer.RunDirect(rc)
	}

	// ── 3. Strategizer ─────────────────────────────────────────────────────
	ir, err := d.strategizer.Run(rc.AgentContext())
	if err != nil {
		return fmt.Errorf("strategizer: %w", err)
	}
	rc.IR = ir
	rc.MaxLoops = resolveMaxLoops(ir, d.engine.cfg)

	// ── 4. Cognitive Loop ──────────────────────────────────────────────────
	if err := d.runLoop(rc); err != nil {
		return fmt.Errorf("cognitive loop: %w", err)
	}

	// ── 5. Synthesizer ─────────────────────────────────────────────────────
	if err := d.synthesizer.Run(rc); err != nil {
		return fmt.Errorf("synthesizer: %w", err)
	}

	// ── 6. Flush write buffer ──────────────────────────────────────────────
	if err := d.engine.buffer.Flush(rc.Ctx); err != nil {
		rc.Ch.Emit(core.Emission{
			RunID:   rc.RunID,
			TurnID:  rc.TurnID,
			Channel: core.ChanWarning,
			Content: fmt.Sprintf("write buffer flush: %v", err),
		})
	}

	// Emit cost summary.
	rc.Ch.Emit(rc.Budget.Summary(rc))

	return nil
}

// route decides whether to take the cognitive path or the direct path.
// Phase 1 always returns routeCognitive.
func (d *dag) route(_ *core.RunContext) routeDecision {
	return routeCognitive
}

func resolveMaxLoops(ir *core.IR, cfg *config.Config) int {
	if ir.MaxLoops > 0 {
		return ir.MaxLoops
	}
	if cfg.Engine.MaxLoops > 0 {
		return cfg.Engine.MaxLoops
	}
	return core.DefaultMaxLoops
}
