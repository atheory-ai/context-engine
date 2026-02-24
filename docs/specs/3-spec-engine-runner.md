# Context Engine — Spec 3: Engine Runner
## Implementation Spec — DAG Topology, Cognitive Loop, Exit Conditions
### Version 1.0 | February 2026

---

> This is an implementation spec, not a PRD section.
> Hand this document to Claude Code alongside spec-data-layer.md and spec-packages.md.
> The pseudocode here is precise enough to implement directly.
> Companion: Context Engine PRD v0.5 Sections 9, 10, 17.
> Decisions Log v1.0 Sections 3, 4.

---

## 1. Overview

The engine runner is the cognitive loop. It wires the static DAG, executes the
loop, manages concurrency, tracks the token budget, and handles both exit
conditions. It lives entirely in `internal/runner/`.

The shape is fixed. It is not configurable at runtime. The topology below is
the topology — not an example, not a starting point.

```
Query In
    │
    ▼
┌─────────────┐
│  Pre-flight  │  — project resolution, token auth, session open, budget init
└─────────────┘
    │
    ▼
┌─────────────┐
│   Router    │  — direct answer vs cognitive loop decision
└─────────────┘
    │ (cognitive path)
    ▼
┌─────────────┐
│ Strategizer │  — query → IR (compiled intent)
└─────────────┘
    │
    ▼
┌──────────────────────────────────────────┐
│              Cognitive Loop              │
│                                          │
│  ┌─────────────────────────────────┐     │
│  │  Activation  (anchor resolution │     │
│  │  + propagation + top-K)         │     │
│  └─────────────────────────────────┘     │
│      │                                   │
│      ▼                                   │
│  ┌─────────────────────────────────┐     │
│  │  Fan-out  (tool selection       │     │
│  │  + concurrent execution)        │     │
│  └─────────────────────────────────┘     │
│      │                                   │
│      ▼                                   │
│  ┌─────────────────────────────────┐     │
│  │  Reviewer  (convergence check,  │     │
│  │  enrichment approval)           │     │
│  └─────────────────────────────────┘     │
│      │                                   │
│      ├── not converged → back to Activation
│      └── converged ──────────────────────┤
│                                          │
└──────────────────────────────────────────┘
    │ (converged or forced exit)
    ▼
┌─────────────┐
│ Synthesizer │  — emissions → final answer
└─────────────┘
    │
    ▼
Answer Out (via ChanMessage)
```

---

## 2. Run Context

Every query creates a `RunContext` that carries all per-query state. It is
passed through every node, every tool call, every goroutine in the fan-out.
It is the single source of truth for the state of a running query.

```go
// internal/runner/context.go

package runner

import (
    "context"
    "sync"
    "sync/atomic"

    "github.com/atheory/context-engine/internal/core"
    "github.com/atheory/context-engine/internal/llm"
)

// RunContext carries all state for a single query execution.
// It wraps the Go context.Context and adds engine-specific state.
type RunContext struct {
    // Go context — cancellation propagates to all goroutines.
    Ctx context.Context

    // Identity
    RunID     core.RunID
    TurnID    core.TurnID
    SessionID core.SessionID
    ProjectID core.ProjectID

    // The compiled IR. Set by the Strategizer, read by everything downstream.
    // Written once; never modified after Strategizer returns.
    IR *core.IR

    // Budget tracks token usage across the entire query.
    Budget *Budget

    // Loop state
    LoopIndex int32  // atomic — readable from concurrent goroutines
    MaxLoops  int    // resolved from IR.MaxLoops or project default

    // Accumulated emissions across all loop iterations.
    // The Synthesizer reads this at the end.
    Emissions []core.Emission
    emMu      sync.Mutex  // guards Emissions

    // ForcedExit is set to true when the budget guard triggers early exit.
    // The Synthesizer checks this to produce a partial answer.
    ForcedExit bool

    // ForcedExitReason is the human-readable reason for forced exit.
    ForcedExitReason string

    // Channels — reference to the engine's AppChannels.
    // All nodes write here; the TUI/CLI reads here.
    Ch *core.AppChannels

    // Resolved anchors from the last activation pass.
    // Updated each loop iteration.
    Anchors []core.Anchor
    anchMu  sync.RWMutex
}

// IncrementLoop atomically increments the loop counter and returns the new value.
func (rc *RunContext) IncrementLoop() int {
    return int(atomic.AddInt32(&rc.LoopIndex, 1))
}

// CurrentLoop returns the current loop index atomically.
func (rc *RunContext) CurrentLoop() int {
    return int(atomic.LoadInt32(&rc.LoopIndex))
}

// AppendEmissions safely appends emissions to the accumulated list.
func (rc *RunContext) AppendEmissions(emissions []core.Emission) {
    rc.emMu.Lock()
    rc.Emissions = append(rc.Emissions, emissions...)
    rc.emMu.Unlock()
}

// SetAnchors replaces the current anchor set after an activation pass.
func (rc *RunContext) SetAnchors(anchors []core.Anchor) {
    rc.anchMu.Lock()
    rc.Anchors = anchors
    rc.anchMu.Unlock()
}

// ReadAnchors returns a snapshot of the current anchor set.
func (rc *RunContext) ReadAnchors() []core.Anchor {
    rc.anchMu.RLock()
    defer rc.anchMu.RUnlock()
    out := make([]core.Anchor, len(rc.Anchors))
    copy(out, rc.Anchors)
    return out
}
```

---

## 3. Token Budget

The budget tracker is the mechanism for the context-window exit condition.
It accumulates token usage across all LLM calls in a query and signals when
the safe threshold is approached.

```go
// internal/runner/budget.go

package runner

import (
    "sync/atomic"
    "github.com/atheory/context-engine/internal/core"
)

// Budget tracks token usage for a single query run.
// All LLM calls in the run report their token usage here.
// Thread-safe — multiple tool goroutines update concurrently.
type Budget struct {
    modelContextLimit int     // from ModelInfo.ContextLimit
    safetyMargin      float64 // from core.ContextWindowSafetyMargin (0.85)

    tokensIn  int64  // atomic
    tokensOut int64  // atomic
    costUSD   int64  // atomic, stored as microdollars (×1,000,000)
}

func NewBudget(modelContextLimit int) *Budget {
    return &Budget{
        modelContextLimit: modelContextLimit,
        safetyMargin:      core.ContextWindowSafetyMargin,
    }
}

// Record adds token usage from a completed LLM call.
func (b *Budget) Record(tokensIn, tokensOut int, costUSD float64) {
    atomic.AddInt64(&b.tokensIn, int64(tokensIn))
    atomic.AddInt64(&b.tokensOut, int64(tokensOut))
    atomic.AddInt64(&b.costUSD, int64(costUSD*1_000_000))
}

// ContextUsedPct returns the percentage of the model's context window consumed.
// Based on total tokens in + out as a proxy for context accumulation.
func (b *Budget) ContextUsedPct() float64 {
    total := atomic.LoadInt64(&b.tokensIn) + atomic.LoadInt64(&b.tokensOut)
    return float64(total) / float64(b.modelContextLimit)
}

// ShouldExit returns true when the context window is approaching capacity.
// Called before each LLM call. If true, the loop must exit gracefully.
func (b *Budget) ShouldExit() bool {
    return b.ContextUsedPct() >= b.safetyMargin
}

// TotalCostUSD returns the total estimated cost in dollars.
func (b *Budget) TotalCostUSD() float64 {
    return float64(atomic.LoadInt64(&b.costUSD)) / 1_000_000
}

// Summary returns a cost emission for the ChanCost channel.
func (b *Budget) Summary(rc *RunContext) core.Emission {
    return core.Emission{
        RunID:   rc.RunID,
        TurnID:  rc.TurnID,
        Channel: core.ChanCost,
        Content: fmt.Sprintf("%.4f USD | %d tokens in | %d tokens out | %.1f%% context",
            b.TotalCostUSD(),
            atomic.LoadInt64(&b.tokensIn),
            atomic.LoadInt64(&b.tokensOut),
            b.ContextUsedPct()*100,
        ),
    }
}
```

---

## 4. The Runner — Entry Point

```go
// internal/runner/runner.go

package runner

import (
    "context"
    "fmt"

    "github.com/atheory/context-engine/internal/agent/preflight"
    "github.com/atheory/context-engine/internal/agent/strategizer"
    "github.com/atheory/context-engine/internal/agent/reviewer"
    "github.com/atheory/context-engine/internal/agent/synthesizer"
    "github.com/atheory/context-engine/internal/core"
    "github.com/atheory/context-engine/internal/graph/activation"
    "github.com/atheory/context-engine/internal/graph/substrate"
    "github.com/atheory/context-engine/internal/llm"
    "github.com/atheory/context-engine/internal/plugins"
    "github.com/atheory/context-engine/internal/storage/db"
    "github.com/atheory/context-engine/internal/storage/writebuffer"
    "github.com/atheory/context-engine/internal/config"
)

// Engine is the assembled, ready-to-use context engine.
// The zero value is invalid — construct with New().
type Engine struct {
    cfg        *config.Config
    channels   core.AppChannels
    dbRegistry *db.Registry
    buffer     writebuffer.Buffer
    substrate  *substrate.ReadWriter
    plugins    *plugins.Registry
    llmRouter  *llm.Router
}

// New constructs a fully wired Engine.
func New(ctx context.Context, cfg *config.Config) (*Engine, error) {
    e := &Engine{
        cfg:      cfg,
        channels: core.NewAppChannels(),
    }

    // Open databases
    e.dbRegistry = db.NewRegistry()
    if err := e.dbRegistry.OpenMeta(cfg.DataDir + "/meta.db"); err != nil {
        return nil, fmt.Errorf("open meta.db: %w", err)
    }
    if err := e.dbRegistry.OpenAudit(cfg.DataDir + "/audit.db"); err != nil {
        return nil, fmt.Errorf("open audit.db: %w", err)
    }
    if cfg.Tracing.Enabled {
        if err := e.dbRegistry.OpenExecution(cfg.DataDir + "/execution.db"); err != nil {
            return nil, fmt.Errorf("open execution.db: %w", err)
        }
    }
    if err := e.dbRegistry.OpenOrgGraph(cfg.DataDir + "/graphs/org.db"); err != nil {
        return nil, fmt.Errorf("open org graph: %w", err)
    }

    // Start write buffer goroutine
    e.buffer = writebuffer.New(ctx, e.dbRegistry,
        writebuffer.DefaultBufferSize,
        writebuffer.DefaultFlushInterval,
    )

    // Build substrate read/write layer
    e.substrate = substrate.NewReadWriter(e.dbRegistry, e.buffer)

    // Load plugins
    e.plugins = plugins.NewRegistry()
    if err := e.plugins.LoadAll(ctx, cfg.Plugins); err != nil {
        return nil, fmt.Errorf("load plugins: %w", err)
    }

    // Build LLM router
    e.llmRouter = llm.NewRouter(cfg.LLM)

    return e, nil
}

// Query executes the cognitive loop for a user query.
func (e *Engine) Query(ctx context.Context, query string) error {
    dag := e.buildDAG()
    return dag.Run(ctx, query)
}

// Channels returns the AppChannels. The caller reads from these to render output.
func (e *Engine) Channels() core.AppChannels {
    return e.channels
}

// Close flushes the write buffer, closes all databases, unloads plugins.
func (e *Engine) Close(ctx context.Context) error {
    if err := e.buffer.Close(ctx); err != nil {
        return fmt.Errorf("close write buffer: %w", err)
    }
    e.plugins.UnloadAll()
    return e.dbRegistry.CloseAll()
}
```

---

## 5. The DAG — Wiring

The DAG is constructed fresh for each query. It is not a persistent object.
Each `dag.Run()` call creates a new RunContext and executes the full topology.

```go
// internal/runner/dag.go

package runner

import (
    "github.com/atheory/context-engine/internal/agent/preflight"
    "github.com/atheory/context-engine/internal/agent/strategizer"
    "github.com/atheory/context-engine/internal/agent/reviewer"
    "github.com/atheory/context-engine/internal/agent/synthesizer"
    "github.com/atheory/context-engine/internal/graph/activation"
)

// dag holds references to all constructed nodes.
// Built once per Engine instance; Run() is called once per query.
type dag struct {
    preflight   *preflight.Node
    router      *routerNode
    strategizer *strategizer.Node
    activation  *activation.Node
    fanout      *fanoutNode
    reviewer    *reviewer.Node
    synthesizer *synthesizer.Node

    // engine references passed through to nodes
    engine *Engine
}

func (e *Engine) buildDAG() *dag {
    return &dag{
        preflight:   preflight.New(e.dbRegistry, e.llmRouter),
        router:      newRouterNode(e.llmRouter),
        strategizer: strategizer.New(e.llmRouter, e.plugins, e.substrate),
        activation:  activation.NewNode(e.substrate),
        fanout:      newFanoutNode(e.substrate, e.plugins),
        reviewer:    reviewer.New(e.llmRouter, e.substrate),
        synthesizer: synthesizer.New(e.llmRouter),
        engine:      e,
    }
}

// Run executes the full DAG for a single query.
// This is the complete cognitive loop.
func (d *dag) Run(ctx context.Context, query string) error {
    // ── 1. Pre-flight ──────────────────────────────────────────────────────
    rc, err := d.preflight.Run(ctx, query, d.engine.cfg, d.engine.channels)
    if err != nil {
        return fmt.Errorf("preflight: %w", err)
    }

    // ── 2. Router ──────────────────────────────────────────────────────────
    // Decides: direct answer (simple factual) vs full cognitive loop.
    // For Phase 1, the router always takes the cognitive path.
    // Direct path is Phase 2.
    routeDecision, err := d.router.Run(rc)
    if err != nil {
        return fmt.Errorf("router: %w", err)
    }
    if routeDecision == routeDirect {
        return d.synthesizer.RunDirect(rc)
    }

    // ── 3. Strategizer ─────────────────────────────────────────────────────
    ir, err := d.strategizer.Run(rc)
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

    // ── 6. Flush write buffer before returning ─────────────────────────────
    if err := d.engine.buffer.Flush(rc.Ctx); err != nil {
        // Non-fatal — log and continue. The answer was already synthesized.
        rc.Ch.Emit(core.Emission{
            Channel: core.ChanWarning,
            Content: fmt.Sprintf("write buffer flush: %v", err),
        })
    }

    // Emit cost summary
    rc.Ch.Emit(rc.Budget.Summary(rc))

    return nil
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
```

---

## 6. The Cognitive Loop

```go
// internal/runner/loop.go

package runner

// runLoop executes the activation → fan-out → reviewer cycle
// until convergence or a forced exit condition is met.
func (d *dag) runLoop(rc *RunContext) error {
    for {
        loopIdx := rc.IncrementLoop()

        rc.Ch.Emit(core.Emission{
            RunID:   rc.RunID,
            TurnID:  rc.TurnID,
            Channel: core.ChanSystem,
            Content: fmt.Sprintf("loop %d/%d", loopIdx, rc.MaxLoops),
        })

        // ── Exit condition 1: iteration limit ─────────────────────────────
        if loopIdx > rc.MaxLoops {
            rc.ForcedExit = true
            rc.ForcedExitReason = fmt.Sprintf(
                "loop limit reached (%d/%d)", loopIdx-1, rc.MaxLoops,
            )
            return nil
        }

        // ── Exit condition 2: context window capacity ──────────────────────
        // Checked before any LLM call in this iteration.
        if rc.Budget.ShouldExit() {
            rc.ForcedExit = true
            rc.ForcedExitReason = fmt.Sprintf(
                "context window at %.0f%% capacity", rc.Budget.ContextUsedPct()*100,
            )
            return nil
        }

        // ── Activation pass ───────────────────────────────────────────────
        anchors, err := d.activation.Run(rc)
        if err != nil {
            return fmt.Errorf("loop %d activation: %w", loopIdx, err)
        }
        rc.SetAnchors(anchors)

        // ── Fan-out ───────────────────────────────────────────────────────
        toolEmissions, err := d.fanout.Run(rc)
        if err != nil {
            return fmt.Errorf("loop %d fanout: %w", loopIdx, err)
        }
        rc.AppendEmissions(toolEmissions)

        // Check budget again after fan-out (tools may have made LLM calls)
        if rc.Budget.ShouldExit() {
            rc.ForcedExit = true
            rc.ForcedExitReason = fmt.Sprintf(
                "context window at %.0f%% capacity after tool execution",
                rc.Budget.ContextUsedPct()*100,
            )
            return nil
        }

        // ── Reviewer ──────────────────────────────────────────────────────
        review, err := d.reviewer.Run(rc, toolEmissions)
        if err != nil {
            return fmt.Errorf("loop %d reviewer: %w", loopIdx, err)
        }

        // Apply approved enrichments via write buffer
        for _, enrichment := range review.ApprovedEnrichments {
            if err := d.engine.substrate.ApplyEnrichment(rc.Ctx, enrichment); err != nil {
                // Non-fatal — log, continue
                rc.Ch.Emit(core.Emission{
                    Channel: core.ChanWarning,
                    Content: fmt.Sprintf("enrichment apply: %v", err),
                })
            }
        }

        // Emit reviewer thinking to channel
        rc.AppendEmissions(review.Emissions)

        // ── Convergence check ─────────────────────────────────────────────
        if review.Converged {
            return nil // clean exit — proceed to Synthesizer
        }

        // Update open queries for next iteration based on Reviewer guidance
        if len(review.UpdatedOpenQueries) > 0 {
            rc.IR.OpenQueries = review.UpdatedOpenQueries
        }
    }
}
```

---

## 7. Fan-out Node

The fan-out node selects which tools activate on the current IR, spawns
concurrent goroutines (one per tool), and waits for all to complete.

```go
// internal/runner/fanout.go

package runner

import (
    "context"
    "fmt"
    "sync"

    "github.com/atheory/context-engine/internal/core"
)

type fanoutNode struct {
    substrate core.SubstrateReader
    plugins   *plugins.Registry
    tools     []core.Tool // all registered tools, built-in + plugin
}

func newFanoutNode(substrate core.SubstrateReader, plugins *plugins.Registry) *fanoutNode {
    return &fanoutNode{
        substrate: substrate,
        plugins:   plugins,
        tools:     buildToolList(substrate, plugins),
    }
}

// Run selects activating tools, executes them concurrently, collects results.
func (f *fanoutNode) Run(rc *RunContext) ([]core.Emission, error) {
    anchors := rc.ReadAnchors()

    // ── Tool selection ─────────────────────────────────────────────────────
    // Each tool's Activate() is a pure function — safe to call concurrently,
    // but we call sequentially here (it's fast and avoids complexity).
    var activating []core.Tool
    for _, tool := range f.tools {
        if tool.Activate(*rc.IR) {
            activating = append(activating, tool)
        }
    }

    if len(activating) == 0 {
        // No tools activated — emit a warning, Reviewer will handle convergence
        rc.Ch.Emit(core.Emission{
            RunID:   rc.RunID,
            TurnID:  rc.TurnID,
            Channel: core.ChanWarning,
            Content: "no tools activated for current IR",
        })
        return nil, nil
    }

    rc.Ch.Emit(core.Emission{
        RunID:   rc.RunID,
        TurnID:  rc.TurnID,
        Channel: core.ChanAction,
        Content: fmt.Sprintf("activating %d tools: %s",
            len(activating), toolNames(activating)),
    })

    // ── Concurrent execution ───────────────────────────────────────────────
    type result struct {
        emissions []core.Emission
        err       error
        toolName  string
    }

    results := make(chan result, len(activating))
    var wg sync.WaitGroup

    for _, tool := range activating {
        wg.Add(1)
        go func(t core.Tool) {
            defer wg.Done()

            req := core.ToolRequest{
                RunID:     rc.RunID,
                TurnID:    rc.TurnID,
                LoopIndex: rc.CurrentLoop(),
                IR:        *rc.IR,
                Anchors:   anchors,
                Substrate: f.substrate,
            }

            toolResult, err := t.Execute(rc.Ctx, req)
            if err != nil {
                results <- result{toolName: t.Name(), err: err}
                return
            }

            // Emit action confirmation
            rc.Ch.Emit(core.Emission{
                RunID:   rc.RunID,
                TurnID:  rc.TurnID,
                Channel: core.ChanAction,
                Content: fmt.Sprintf("tool:%s complete (%d emissions)",
                    t.Name(), len(toolResult.Emissions)),
            })

            // Substrate proposals go to the Reviewer for approval, not
            // directly to the write buffer. Bundle them in a thinking emission.
            if len(toolResult.ProposedNodes)+len(toolResult.ProposedEdges) > 0 {
                rc.Ch.Emit(core.Emission{
                    RunID:    rc.RunID,
                    TurnID:   rc.TurnID,
                    Channel:  core.ChanThinking,
                    Content:  fmt.Sprintf("tool:%s proposed %d nodes, %d edges",
                        t.Name(),
                        len(toolResult.ProposedNodes),
                        len(toolResult.ProposedEdges)),
                    Metadata: map[string]any{
                        "proposed_nodes": toolResult.ProposedNodes,
                        "proposed_edges": toolResult.ProposedEdges,
                        "tool":           t.Name(),
                    },
                })
            }

            results <- result{
                toolName:  t.Name(),
                emissions: toolResult.Emissions,
            }
        }(tool)
    }

    // Close results channel when all goroutines complete
    go func() {
        wg.Wait()
        close(results)
    }()

    // ── Collect results ────────────────────────────────────────────────────
    var allEmissions []core.Emission
    var errs []error

    for r := range results {
        if r.err != nil {
            // Tool errors are non-fatal. The Reviewer will note the gap.
            errs = append(errs, fmt.Errorf("tool %s: %w", r.toolName, r.err))
            rc.Ch.Emit(core.Emission{
                RunID:   rc.RunID,
                TurnID:  rc.TurnID,
                Channel: core.ChanError,
                Content: fmt.Sprintf("tool %s failed: %v", r.toolName, r.err),
            })
            continue
        }
        allEmissions = append(allEmissions, r.emissions...)
    }

    // If ALL tools failed, that's a hard error — the loop cannot continue.
    if len(errs) == len(activating) {
        return nil, fmt.Errorf("all %d tools failed: %v", len(activating), errs)
    }

    return allEmissions, nil
}

func toolNames(tools []core.Tool) string {
    names := make([]string, len(tools))
    for i, t := range tools {
        names[i] = t.Name()
    }
    return strings.Join(names, ", ")
}

// buildToolList assembles all available tools: built-in + plugin-contributed.
func buildToolList(substrate core.SubstrateReader, reg *plugins.Registry) []core.Tool {
    var tools []core.Tool

    // Built-in tools (always available)
    tools = append(tools,
        callgraph.New(substrate),
        references.New(substrate),
        crossproject.New(substrate),
        concepts.New(substrate),
        filecontext.New(substrate),
        summary.New(substrate),
    )

    // Plugin-contributed tools
    for _, plugin := range reg.Loaded() {
        tools = append(tools, plugin.Tools()...)
    }

    return tools
}
```

---

## 8. Pre-flight Node

Pre-flight is the first node in the DAG. It validates the request, resolves
the project, opens a session, and constructs the RunContext.

```go
// internal/agent/preflight/preflight.go

package preflight

// Run validates the incoming query and constructs a RunContext.
// Returns an error if:
//   - no active project is configured
//   - project is not indexed (status == "unindexed")
//   - token is revoked, expired, or has insufficient scope
//   - project graph database cannot be mounted
func (n *Node) Run(
    ctx context.Context,
    query string,
    cfg *config.Config,
    ch *core.AppChannels,
) (*runner.RunContext, error) {

    // 1. Resolve active project from config
    project, err := n.queries.GetProjectByGitURL(ctx, cfg.Project.GitURL)
    if err != nil {
        return nil, core.ErrProjectNotFound
    }
    if project.Status == "unindexed" {
        return nil, core.ErrProjectNotIndexed
    }

    // 2. Validate token (if non-local session)
    if cfg.Token != "" {
        token, err := n.queries.GetToken(ctx, cfg.Token)
        if err != nil || token.Revoked {
            return nil, core.ErrTokenRevoked
        }
        if token.ExpiresAt != nil && time.Now().UnixMilli() > *token.ExpiresAt {
            return nil, core.ErrTokenExpired
        }
        // read-scoped tokens cannot trigger write operations
        if token.Scope == core.ScopeRead {
            cfg.ReadOnly = true
        }
    }

    // 3. Mount project graph database
    if err := n.dbRegistry.Mount(string(project.ID), project.GraphPath); err != nil {
        return nil, fmt.Errorf("mount project graph: %w", err)
    }

    // 4. Open session + turn in audit.db
    sessionID := core.SessionID(uuid.New().String())
    turnID := core.TurnID(uuid.New().String())
    runID := core.RunID(uuid.New().String())

    if err := n.queries.OpenSession(ctx, sessionID, cfg); err != nil {
        return nil, fmt.Errorf("open session: %w", err)
    }
    if err := n.queries.OpenTurn(ctx, turnID, sessionID, query); err != nil {
        return nil, fmt.Errorf("open turn: %w", err)
    }

    // 5. Emit session-open system event
    ch.Emit(core.Emission{
        RunID:   runID,
        TurnID:  turnID,
        Channel: core.ChanSystem,
        Content: fmt.Sprintf("session %s | project %s | run %s",
            sessionID, project.Name, runID),
    })

    // 6. Build run context
    budget := runner.NewBudget(n.llm.ModelInfo().ContextLimit)

    return &runner.RunContext{
        Ctx:       ctx,
        RunID:     runID,
        TurnID:    turnID,
        SessionID: sessionID,
        ProjectID: project.ID,
        Budget:    budget,
        MaxLoops:  cfg.Engine.MaxLoops,
        Ch:        ch,
    }, nil
}
```

---

## 9. Reviewer Node

The Reviewer is the convergence authority. It decides whether the loop has
gathered enough information to synthesize a useful answer. It also approves
or rejects proposed substrate enrichments from tools.

```go
// internal/agent/reviewer/reviewer.go

package reviewer

// ReviewResult is what the Reviewer returns to the loop.
type ReviewResult struct {
    Converged           bool
    UpdatedOpenQueries  []string        // nil = no change
    ApprovedEnrichments []core.Enrichment
    Emissions           []core.Emission // reviewer's thinking, emitted to ChanThinking
}

// Run evaluates the current loop state and produces a convergence decision.
func (r *Node) Run(rc *runner.RunContext, loopEmissions []core.Emission) (ReviewResult, error) {

    // Budget check — if budget guard has set ForcedExit, the loop
    // already exited. This path should not be reached, but guard anyway.
    if rc.ForcedExit {
        return ReviewResult{Converged: true}, nil
    }

    // Build the reviewer prompt (see spec-strategizer-prompt.md for full
    // prompt design — this spec covers the structural contract only)
    prompt := r.buildPrompt(rc, loopEmissions)

    // Check budget before LLM call
    if rc.Budget.ShouldExit() {
        rc.ForcedExit = true
        rc.ForcedExitReason = "context window capacity before reviewer LLM call"
        return ReviewResult{Converged: true}, nil
    }

    resp, err := r.llm.Complete(rc.Ctx, core.CompletionRequest{
        Model:    r.llm.ModelForTier(core.TierFast), // Reviewer uses fast tier
        Messages: prompt,
        System:   r.systemPrompt(rc),
    })
    if err != nil {
        return ReviewResult{}, fmt.Errorf("reviewer LLM: %w", err)
    }

    rc.Budget.Record(resp.TokensIn, resp.TokensOut, estimateCost(resp))

    // Parse reviewer output
    result, err := r.parseResponse(rc, resp.Content, loopEmissions)
    if err != nil {
        // Parse failure = treat as non-converged, continue loop
        rc.Ch.Emit(core.Emission{
            Channel: core.ChanWarning,
            Content: fmt.Sprintf("reviewer parse error: %v — continuing loop", err),
        })
        return ReviewResult{Converged: false}, nil
    }

    // Emit reviewer thinking
    rc.Ch.Emit(core.Emission{
        RunID:   rc.RunID,
        TurnID:  rc.TurnID,
        Channel: core.ChanThinking,
        Content: resp.Content,
    })

    return result, nil
}
```

### Convergence Criteria

The Reviewer LLM is prompted to evaluate:

1. **Open queries answered**: Are the open queries from the IR resolved by
   the current emissions?
2. **Sufficient depth**: Do the emissions contain enough substrate context
   to support a useful answer?
3. **Diminishing returns**: Is the last loop iteration adding materially
   new information, or repeating what's already known?

Any of these conditions alone is insufficient — the Reviewer must be
satisfied on all three, or must explicitly flag which are still unmet
(returned as `UpdatedOpenQueries`).

The Reviewer outputs XML tags:

```xml
<converged>true</converged>

<open_queries>
  <open_query>how does billing event trigger propagate to scheduler?</open_query>
</open_queries>

<enrichments>
  <enrichment action="promote" entity_type="edge" entity_id="abc123"
    rationale="co-activated 3 times in this loop"/>
</enrichments>
```

---

## 10. Synthesizer Node

The Synthesizer produces the final answer from accumulated emissions.
It handles two cases: clean convergence and forced exit.

```go
// internal/agent/synthesizer/synthesizer.go

package synthesizer

// Run produces the final answer from accumulated loop emissions.
func (s *Node) Run(rc *runner.RunContext) error {
    if rc.ForcedExit {
        return s.runPartial(rc)
    }
    return s.runFull(rc)
}

// runFull synthesizes when the Reviewer converged cleanly.
func (s *Node) runFull(rc *runner.RunContext) error {
    prompt := s.buildPrompt(rc, false)

    resp, err := s.llm.Complete(rc.Ctx, core.CompletionRequest{
        Model:    s.llm.ModelForTier(core.TierStandard),
        Messages: prompt,
        System:   s.systemPrompt(rc),
    })
    if err != nil {
        return fmt.Errorf("synthesizer LLM: %w", err)
    }

    rc.Budget.Record(resp.TokensIn, resp.TokensOut, estimateCost(resp))

    rc.Ch.Emit(core.Emission{
        RunID:    rc.RunID,
        TurnID:   rc.TurnID,
        Channel:  core.ChanMessage,
        Content:  resp.Content,
        Markdown: true,
    })

    return nil
}

// runPartial synthesizes when the loop was cut short by a forced exit.
// The partial answer includes:
// 1. What was found (the emissions gathered so far)
// 2. What would have been done next (the continuation plan)
// 3. A clear signal to the user that the answer is partial
func (s *Node) runPartial(rc *runner.RunContext) error {
    prompt := s.buildPrompt(rc, true) // true = partial mode

    resp, err := s.llm.Complete(rc.Ctx, core.CompletionRequest{
        Model:    s.llm.ModelForTier(core.TierStandard),
        Messages: prompt,
        System:   s.systemPromptPartial(rc),
    })
    if err != nil {
        // If even the partial synthesis fails, emit a fallback message.
        rc.Ch.Emit(core.Emission{
            RunID:    rc.RunID,
            TurnID:   rc.TurnID,
            Channel:  core.ChanMessage,
            Content:  s.fallbackMessage(rc),
            Markdown: true,
        })
        return nil // non-fatal — user gets a degraded but valid response
    }

    rc.Budget.Record(resp.TokensIn, resp.TokensOut, estimateCost(resp))

    // Prepend a partial-answer notice
    content := fmt.Sprintf(
        "> ⚠️ **Partial answer** — %s\n\n%s",
        rc.ForcedExitReason,
        resp.Content,
    )

    rc.Ch.Emit(core.Emission{
        RunID:    rc.RunID,
        TurnID:   rc.TurnID,
        Channel:  core.ChanMessage,
        Content:  content,
        Markdown: true,
    })

    return nil
}

// fallbackMessage is the last-resort response when partial synthesis fails.
func (s *Node) fallbackMessage(rc *runner.RunContext) string {
    return fmt.Sprintf(
        "The engine collected partial results (%d emissions across %d loops) "+
            "but could not synthesize a complete answer. Reason: %s. "+
            "Please try a more focused query.",
        len(rc.Emissions),
        rc.CurrentLoop(),
        rc.ForcedExitReason,
    )
}
```

### Synthesizer Prompt Structure

The Synthesizer receives:
1. The original user query
2. All accumulated emissions (tool results, Reviewer thinking)
3. A flag indicating whether this is a partial answer
4. If partial: the open queries that remain unresolved + what would have been done next

The system prompt instructs it to:
- Answer the original query directly and completely
- Ground every claim in specific substrate evidence from the emissions
- For partial answers: clearly state what was found vs what remains unknown
- Format as clean Markdown with code references where appropriate

---

## 11. Execution Log — Writing

Every LLM call in the loop is logged to `execution.db` when tracing is enabled.
This is the contract that CE Studio consumes.

```go
// internal/runner/execlog.go

package runner

// logLLMCall writes an ExecutionLogEntry after every LLM completion.
// Called immediately after Budget.Record().
// No-op if tracing is disabled or session is read-scoped.
func (e *Engine) logLLMCall(
    rc *RunContext,
    nodeType string,
    req core.CompletionRequest,
    resp core.CompletionResponse,
    irEmitted *core.IR,
) {
    if !e.cfg.Tracing.Enabled {
        return
    }
    if e.cfg.ReadOnly {
        return  // read-scoped sessions never write execution log
    }

    entry := &queries.ExecutionLogEntry{
        ID:               uuid.New().String(),
        RunID:            string(rc.RunID),
        TurnID:           string(rc.TurnID),
        SessionID:        string(rc.SessionID),
        NodeType:         nodeType,
        LoopIndex:        rc.CurrentLoop(),
        Model:            resp.Model,
        Tier:             inferTier(resp.Model),
        PromptMessages:   marshalMessages(req.Messages),
        Response:         resp.Content,
        ThinkingTrace:    resp.ThinkingText,
        IREmitted:        marshalIR(irEmitted),
        TokensIn:         resp.TokensIn,
        TokensOut:        resp.TokensOut,
        EstimatedCostUSD: estimateCost(resp),
        LatencyMS:        0,  // set by caller with timing wrapper
        ContextUsedPct:   rc.Budget.ContextUsedPct(),
        Timestamp:        time.Now().UnixMilli(),
    }

    // Non-blocking fire-and-forget write
    go func() {
        if err := e.execQueries.Insert(context.Background(), entry); err != nil {
            // Log but do not fail the query
            e.channels.Emit(core.Emission{
                Channel: core.ChanDebug,
                Content: fmt.Sprintf("exec log write: %v", err),
            })
        }
    }()
}
```

---

## 12. Error Handling and Cancellation

### Cancellation Propagation

The `context.Context` created in Pre-flight is the single cancellation root.
When the user sends SIGINT (Ctrl-C), the CLI cancels this context. All
goroutines — the loop, the fan-out workers, the LLM calls — respect `ctx.Done()`.

The fan-out goroutines check `rc.Ctx` at the start of each tool execution:

```go
go func(t core.Tool) {
    defer wg.Done()

    // Respect cancellation before starting work
    select {
    case <-rc.Ctx.Done():
        results <- result{toolName: t.Name(), err: rc.Ctx.Err()}
        return
    default:
    }

    // ... tool execution
}(tool)
```

### Error Classification

| Error type | Handling |
|-----------|---------|
| Single tool failure | Non-fatal. Log to ChanError. Reviewer notes the gap. |
| All tools fail | Fatal. Loop returns error. Synthesizer not called. |
| Reviewer LLM failure | Non-fatal. Log warning. Treat as non-converged, continue. |
| Strategizer LLM failure | Fatal. No IR = no loop. Return error to caller. |
| Synthesizer LLM failure | Graceful degradation. Emit fallback message. |
| Budget forced exit | Clean path. Synthesizer runs in partial mode. |
| Context cancellation | Clean path. Write buffer flushed. Session closed. |
| Database error | Fatal. Propagated up. Logged to audit.db if possible. |

### Session Close on Error

Pre-flight opens a session; it must be closed regardless of what happens.
The runner uses defer:

```go
func (d *dag) Run(ctx context.Context, query string) error {
    rc, err := d.preflight.Run(...)
    if err != nil {
        return err
    }

    defer func() {
        // Always close the turn and session, even on error
        status := "complete"
        if err != nil {
            status = "error"
        }
        _ = d.queries.CloseTurn(context.Background(), rc.TurnID, status)
        _ = d.queries.CloseSession(context.Background(), rc.SessionID)
        _ = d.dbRegistry.Unmount(string(rc.ProjectID))
    }()

    // ... rest of execution
}
```

---

## 13. Package Layout — internal/runner/

```
internal/runner/
  runner.go         — Engine struct, New(), Query(), Close()
  dag.go            — dag struct, buildDAG(), dag.Run()
  loop.go           — runLoop()
  fanout.go         — fanoutNode, concurrent tool execution
  context.go        — RunContext struct
  budget.go         — Budget struct, token tracking
  execlog.go        — logLLMCall(), ExecutionLogEntry writing
  toollist.go       — buildToolList(), tool registration
```

---

## 14. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| DAG topology | Static, hardcoded — not data-driven |
| Loop exit condition 1 | Iteration limit (IR.MaxLoops or project default) |
| Loop exit condition 2 | Context window at 85% of model limit |
| Budget check timing | Before every LLM call + after fan-out |
| Fan-out concurrency | WaitGroup, one goroutine per activating tool |
| Tool failure handling | Non-fatal unless ALL tools fail |
| Reviewer tier | Fast (speed over depth — it's a convergence check) |
| Synthesizer tier | Standard |
| Forced exit handling | Partial synthesis with continuation plan + user notice |
| Cancellation | Single context.Context threaded through all goroutines |
| Session lifecycle | Opened in preflight, closed in deferred call regardless of outcome |
| Execution log writes | Fire-and-forget goroutine, non-blocking, non-fatal on failure |

---

*Spec 3: Engine Runner — v1.0 — February 2026*
*Next: Spec 4 — Plugin System (Engine Side)*
*Companion: Context Engine PRD v0.5 Sections 9, 10, 17 | Decisions Log v1.0 Section 3*
