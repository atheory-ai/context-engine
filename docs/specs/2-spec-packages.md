# Context Engine — Spec 2: Go Package Structure
## Implementation Spec — Directory Tree, Core Interfaces, Dependency Graph
### Version 1.0 | February 2026

---

> This is an implementation spec, not a PRD section.
> Hand this document to Claude Code alongside spec-data-layer.md.
> The package tree here is the authoritative layout. Every file placeholder
> listed gets created. Companion: Context Engine PRD v0.5 Sections 8, 9, 16.
> Decisions Log v1.0 Sections 2, 3.

---

## 1. Module Identity

```
module github.com/atheory/context-engine

go 1.23
```

All internal import paths derive from this root.

---

## 2. Top-Level Directory Tree

```
context-engine/
  cmd/
    ce/
      main.go               — entry point, thin glue only
  internal/                 — not importable outside this module
    core/                   — interfaces + shared types, zero internal deps
    storage/                — SQLite layer (see spec-data-layer.md)
    config/                 — Viper config loading + ce.yaml types
    agent/                  — cognitive loop nodes
    graph/                  — substrate, ontology, activation
    tools/                  — built-in tools (one sub-package each)
    plugins/                — plugin host (wazero + Extism)
    llm/                    — LLM provider abstraction + implementations
    indexer/                — file watching, parsing, graph population
    runner/                 — DAG wiring + cognitive loop execution
    server/                 — MCP, REST API, WebSocket servers
  cli/                      — Cobra command tree
  tui/                      — Bubbletea TUI
  scripts/                  — dev scripts (not compiled)
  go.mod
  go.sum
  Makefile
  README.md
```

`cmd/ce/main.go` is fewer than 50 lines. It parses flags, loads config,
constructs the engine, and hands control to either the CLI or TUI.

---

## 3. internal/core — The Dependency Floor

`core` has **zero imports from other internal packages**. Every other package
imports from `core`. Nothing in `core` imports from `agent`, `graph`, `storage`,
etc. This is enforced — any import cycle involving `core` is a structural error.

```
internal/core/
  types.go          — primitive types: NodeID, EdgeID, ProjectID, RunID, etc.
  interfaces.go     — all primary interfaces
  ir.go             — IR struct + validation
  emission.go       — Emission types
  channel.go        — Channel envelope types
  errors.go         — sentinel errors
  constants.go      — source classes, node types, edge types, token scopes
```

### 3.1 core/types.go

```go
package core

// Typed string aliases. Prevents passing a ProjectID where a NodeID is expected.
type (
    NodeID    string
    EdgeID    string
    ProjectID string
    RunID     string
    TurnID    string
    SessionID string
    PluginID  string
    TokenID   string
)

// Node is a property graph node as read from the substrate.
type Node struct {
    ID          NodeID
    ProjectID   ProjectID
    Type        string      // symbol | namespace | concept | file | plugin-defined
    Label       string
    CanonicalID string
    SourceClass SourceClass
    PluginID    PluginID
    Properties  map[string]any
    CreatedAt   int64
    UpdatedAt   int64
}

// Edge is a property graph edge as read from the substrate.
type Edge struct {
    ID          EdgeID
    ProjectID   ProjectID
    SourceID    NodeID
    TargetID    NodeID
    Type        string
    SourceClass SourceClass
    Weight      float64     // from edge_weight table, joined at read time
    PluginID    PluginID
    Properties  map[string]any
    CreatedAt   int64
}

// Anchor is a resolved substrate reference.
// The Strategizer produces AnchorRefs (symbolic).
// The activation layer resolves them to Anchors (concrete nodes + edges).
type Anchor struct {
    Ref        AnchorRef
    Node       *Node       // nil if not resolved to a node
    Edges      []Edge      // outbound edges from this node
    Activation float64
}

// AnchorRef is the symbolic pointer the Strategizer emits.
type AnchorRef struct {
    Type       string      // symbol | namespace | concept | file
    ID         string      // canonical identifier
    Confidence string      // high | medium | low
}

type SourceClass string

const (
    SourceStructural  SourceClass = "structural"
    SourceAssociative SourceClass = "associative"
    SourceSpeculative SourceClass = "speculative"
    SourceDerived     SourceClass = "derived"
)
```

### 3.2 core/ir.go

The IR is what the Strategizer compiles a user query into.
This is the central data structure of the cognitive loop.

```go
package core

// IR is the Intermediate Representation produced by the Strategizer.
// It is the compiled form of the user's query — the engine's
// understanding of what is being asked and what to look for.
//
// The Strategizer emits XML tags; the tag extractor parses them
// into this struct. Downstream nodes operate on the IR, not on
// the raw query text.
type IR struct {
    // Mode controls how the engine approaches the query.
    Mode IRMode // thinking | direct | audit

    // Anchors are the substrate entry points.
    // The activation layer resolves these to actual nodes.
    Anchors []AnchorRef

    // Predicates are boolean flags that activate specific tools.
    // Tool.Activate(ir) returns true if the tool should run.
    Predicates map[string]string

    // OpenQueries are unresolved sub-questions the engine identified.
    // The cognitive loop works through these until they're answered
    // or the loop converges.
    OpenQueries []string

    // MaxLoops caps the cognitive loop iteration count for this query.
    // Overrides the project default. 0 = use project default.
    MaxLoops int

    // KLimit caps the number of nodes returned per activation query.
    // Overrides the project default. 0 = use project default.
    KLimit int

    // RoleHint is an optional suggestion for which agent role to use.
    // Empty = use the default role for this project.
    RoleHint string

    // ModelTier is an optional request for a specific LLM tier.
    // Empty = let the router decide.
    ModelTier string // fast | standard | thinking
}

type IRMode string

const (
    IRModeThinking IRMode = "thinking"
    IRModeDirect   IRMode = "direct"
    IRModeAudit    IRMode = "audit"
)

// Validate checks that the IR is structurally sound.
// Called immediately after tag extraction. Returns the first
// validation error found.
func (ir *IR) Validate() error
```

### 3.3 core/interfaces.go

```go
package core

import "context"

// ============================================================
// LLM Provider
// ============================================================

// LLMProvider is the abstraction over all LLM backends.
// Implementations live in internal/llm/{provider}.
type LLMProvider interface {
    // Complete sends a prompt and returns the full response.
    // Handles retries, rate limiting, and error classification internally.
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)

    // Stream sends a prompt and streams tokens to the provided channel.
    // Channel is closed when the response is complete.
    Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error

    // ModelInfo returns metadata about the currently configured model.
    ModelInfo() ModelInfo

    // EstimateTokens returns a rough token count for the given text.
    // Used for context window budget tracking. Does not call the API.
    EstimateTokens(text string) int
}

type CompletionRequest struct {
    Model       string
    Messages    []Message
    MaxTokens   int
    Temperature float32
    System      string
    Thinking    *ThinkingConfig // nil = disabled
}

type ThinkingConfig struct {
    BudgetTokens int
}

type CompletionResponse struct {
    Content      string
    ThinkingText string // empty if thinking not enabled
    TokensIn     int
    TokensOut    int
    Model        string
    FinishReason string
}

type Message struct {
    Role    string // user | assistant
    Content string
}

type ModelInfo struct {
    ID           string
    ContextLimit int
    Tier         string // fast | standard | thinking
}

// ============================================================
// Plugin
// ============================================================

// Plugin is a loaded, validated WASM plugin.
// Implementations live in internal/plugins/runtime.
// Plugin authors never implement this interface directly —
// it is the engine-side view of a compiled .wasm artifact.
type Plugin interface {
    ID() PluginID
    Name() string
    Version() string

    // Language returns the language handler, if this plugin defines one.
    Language() LanguageHandler

    // Roles returns the agent roles this plugin defines.
    Roles() []RoleDefinition

    // Analyzers returns the analysis passes this plugin defines.
    Analyzers() []Analyzer

    // Tools returns the tools this plugin defines.
    Tools() []Tool

    // Close unloads the plugin and frees wazero resources.
    Close() error
}

// LanguageHandler teaches the indexer about a language or framework.
type LanguageHandler interface {
    // Match returns true if this handler should process the given file path.
    Match(filePath string) bool

    // Extract parses a file and returns the nodes and edges to add to the graph.
    Extract(filePath string, content []byte) (ExtractionResult, error)

    // Concepts returns the concept seeds this language contributes.
    Concepts() []ConceptSeed
}

type ExtractionResult struct {
    Nodes []Node
    Edges []Edge
}

type ConceptSeed struct {
    Term       string
    Definition string
    Related    []string
    Synonyms   []string
}

// RoleDefinition is an agent persona contributed by a plugin.
type RoleDefinition struct {
    Name         string
    SystemPrompt string
    ToolNames    []string // tools this role has access to
}

// Analyzer is a post-extraction analysis pass.
type Analyzer interface {
    Name() string
    Description() string
    // Analyze receives all nodes for a file and can produce additional edges.
    Analyze(nodes []Node) ([]Edge, error)
}

// ============================================================
// Tool
// ============================================================

// Tool is a capability the cognitive loop can invoke.
// Built-in tools implement this directly in Go.
// Plugin tools are wrapped by the plugin runtime.
type Tool interface {
    Name() string
    Description() string // max 100 chars — used in Strategizer's tool list

    // Activate returns true if this tool should run given the current IR.
    // Called during the fan-out decision. Pure function — no side effects.
    Activate(ir IR) bool

    // Execute runs the tool and returns emissions to the cognitive loop.
    // Receives the resolved anchors and read-only substrate access.
    Execute(ctx context.Context, req ToolRequest) (ToolResult, error)
}

type ToolRequest struct {
    RunID     RunID
    TurnID    TurnID
    LoopIndex int
    IR        IR
    Anchors   []Anchor
    Substrate SubstrateReader
}

type ToolResult struct {
    Emissions []Emission
    // Proposed substrate changes. The Reviewer decides whether to apply.
    ProposedNodes []Node
    ProposedEdges []Edge
}

// SubstrateReader is the read-only substrate view tools receive.
// Tools cannot write to the substrate directly — changes go
// through the Reviewer's approval, then the write buffer.
type SubstrateReader interface {
    Node(ctx context.Context, id NodeID) (*Node, error)
    Edges(ctx context.Context, nodeID NodeID, edgeType string) ([]Edge, error)
    TopK(ctx context.Context, projectID ProjectID, k int) ([]Anchor, error)
    Query(ctx context.Context, q SubstrateQuery) ([]Node, error)
}

type SubstrateQuery struct {
    ProjectID   ProjectID
    NodeTypes   []string
    MinActivation float64
    Properties  map[string]string // json_extract filters
    Limit       int
}

// ============================================================
// Emission
// ============================================================

// Emission is a unit of output from any cognitive loop node.
// All output flows through typed emissions, not direct I/O.
type Emission struct {
    RunID     RunID
    TurnID    TurnID
    LoopIndex int
    Source    string    // which node produced this (strategizer, tool:name, etc.)
    Channel   ChannelType
    Content   string
    Markdown  bool
    Metadata  map[string]any
}

// ============================================================
// Substrate Writer (engine-internal, not exposed to tools)
// ============================================================

// SubstrateWriter is the write side of the substrate.
// Only the Reviewer and the indexer use this.
// All writes go through the write buffer — this interface
// wraps the buffer's Send method with typed operations.
type SubstrateWriter interface {
    UpsertNode(ctx context.Context, node Node) error
    UpsertEdge(ctx context.Context, edge Edge) error
    UpdateActivation(ctx context.Context, nodeID NodeID, activation float64) error
    UpdateWeight(ctx context.Context, edgeID EdgeID, delta WeightDelta) error
    RecordEnrichment(ctx context.Context, e Enrichment) error
}

type WeightDelta struct {
    NewWeight           float64
    NewSourceClass      SourceClass
    CoActivationDelta   int
}

type Enrichment struct {
    RunID       RunID
    TurnID      TurnID
    LoopIndex   int
    EntityType  string // node | edge | concept_seed
    EntityID    string
    Action      string // created | updated | promoted
    BeforeState any
    AfterState  any
    Rationale   string
}
```

### 3.4 core/channel.go

```go
package core

// ChannelType identifies which output stream an emission belongs to.
type ChannelType string

const (
    ChanThinking ChannelType = "thinking"  // internal reasoning, dim in UI
    ChanAction   ChannelType = "action"    // tool activations, status
    ChanMessage  ChannelType = "message"   // final LLM speech, always markdown
    ChanDebug    ChannelType = "debug"     // --debug flag only
    ChanError    ChannelType = "error"     // errors
    ChanWarning  ChannelType = "warning"   // warnings
    ChanProgress ChannelType = "progress"  // progress bars, spinners
    ChanCoverage ChannelType = "coverage"  // structured coverage summary
    ChanCost     ChannelType = "cost"      // --show-cost flag only
    ChanSystem   ChannelType = "system"    // lifecycle events
)

// AppChannels is the centralized set of channels that flows through the
// entire cognitive loop. The runner creates it; all nodes write to it;
// the TUI/CLI pops from it.
type AppChannels struct {
    Thinking chan Emission
    Action   chan Emission
    Message  chan Emission
    Debug    chan Emission
    Error    chan Emission
    Warning  chan Emission
    Progress chan Emission
    Coverage chan Emission
    Cost     chan Emission
    System   chan Emission
}

// NewAppChannels creates AppChannels with appropriate buffer sizes.
// Buffer sizes are intentionally generous — the goal is that no
// cognitive loop node ever blocks on a channel write.
func NewAppChannels() AppChannels {
    return AppChannels{
        Thinking: make(chan Emission, 64),
        Action:   make(chan Emission, 32),
        Message:  make(chan Emission, 16),
        Debug:    make(chan Emission, 128),
        Error:    make(chan Emission, 16),
        Warning:  make(chan Emission, 16),
        Progress: make(chan Emission, 32),
        Coverage: make(chan Emission, 8),
        Cost:     make(chan Emission, 8),
        System:   make(chan Emission, 16),
    }
}

// Emit is a convenience method that sends to the correct channel
// based on the emission's ChannelType. Non-blocking — if the channel
// is full, the emission is dropped and a debug log is written.
func (c *AppChannels) Emit(e Emission) {
    switch e.Channel {
    case ChanThinking:
        select { case c.Thinking <- e: default: }
    case ChanAction:
        select { case c.Action <- e: default: }
    // ... etc for each channel type
    }
}
```

### 3.5 core/errors.go

```go
package core

import "errors"

var (
    ErrProjectNotFound    = errors.New("project not found")
    ErrProjectNotIndexed  = errors.New("project not indexed")
    ErrPluginNotFound     = errors.New("plugin not found")
    ErrInvalidIR          = errors.New("invalid IR")
    ErrContextWindowFull  = errors.New("context window approaching capacity")
    ErrLoopLimitReached   = errors.New("loop limit reached")
    ErrBufferFull         = errors.New("write buffer full")
    ErrTokenRevoked       = errors.New("token revoked")
    ErrTokenExpired       = errors.New("token expired")
    ErrInsufficientScope  = errors.New("insufficient token scope")
    ErrReadOnlySession    = errors.New("write attempted in read-only session")
)
```

### 3.6 core/constants.go

```go
package core

// Node types — built-in. Plugins can define additional types as plain strings.
const (
    NodeTypeSymbol    = "symbol"
    NodeTypeNamespace = "namespace"
    NodeTypeConcept   = "concept"
    NodeTypeFile      = "file"
    NodeTypeDirectory = "directory"
)

// Edge types — built-in.
const (
    EdgeTypeCalls       = "calls"
    EdgeTypeImports     = "imports"
    EdgeTypeImplements  = "implements"
    EdgeTypeExtends     = "extends"
    EdgeTypeContains    = "contains"
    EdgeTypeReferences  = "references"
    EdgeTypeDefines     = "defines"
    EdgeTypeBelongsTo   = "belongs_to"
    EdgeTypeSynonymOf   = "synonym_of"
    EdgeTypeCoActivates = "co_activates"
    EdgeTypeAnnotates   = "annotates"
)

// Token scopes
const (
    ScopeRead      = "read"
    ScopeReadWrite = "read-write"
    ScopeAdmin     = "admin"
)

// IR modes
const (
    TierFast     = "fast"
    TierStandard = "standard"
    TierThinking = "thinking"
)

// Activation thresholds
const (
    DefaultKLimit        = 30
    DefaultMaxLoops      = 8
    ContextWindowSafetyMargin = 0.85  // exit loop at 85% of model context limit
)
```

---

## 4. Full Internal Package Tree

Each file listed here gets created. Files marked `// stub` start as package
declaration only and are fleshed out in their respective spec session.

```
internal/
  core/                     — (fully defined in Section 3)
    types.go
    interfaces.go
    ir.go
    emission.go
    channel.go
    errors.go
    constants.go

  config/
    config.go               — Config struct, Load(), Defaults()
    schema.go               — ce.yaml schema types
    validate.go             — config validation

  storage/                  — (fully defined in spec-data-layer.md)
    db/
      open.go
      registry.go
    writebuffer/
      buffer.go
      types.go
      pending.go
      buffer_test.go
    migrations/
      migrate.go
      meta/
      audit/
      execution/
      graph/
    queries/
      nodes.go
      edges.go
      activation.go
      enrichments.go
      projects.go
      tokens.go
      sessions.go
      audit.go
      execution.go

  agent/
    preflight/
      preflight.go          — project resolution, token validation, session open
    strategizer/
      strategizer.go        — Strategizer node: query → IR
      extractor.go          — XML tag extractor: response text → IR struct
      prompt.go             — system prompt assembly
    reviewer/
      reviewer.go           — Reviewer node: convergence decision, enrichments
      convergence.go        — convergence criteria evaluation
    synthesizer/
      synthesizer.go        — Synthesizer node: emissions → final answer
      partial.go            — forced-exit partial answer + continuation plan

  graph/
    substrate/
      reader.go             — SubstrateReader implementation
      writer.go             — SubstrateWriter implementation (wraps write buffer)
    activation/
      propagation.go        — activation spreading algorithm
      anchor.go             — AnchorRef → Anchor resolution
      topk.go               — top-K node retrieval
    ontology/
      ontology.go           — concept seed management, term normalization

  tools/
    callgraph/
      callgraph.go          — follows call chains through the substrate
    references/
      references.go         — finds all references to anchored symbols
    crossproject/
      crossproject.go       — traverses org graph for cross-project relationships
    concepts/
      concepts.go           — concept vocabulary expansion
    filecontext/
      filecontext.go        — retrieves file-level nodes and their neighbors
    summary/
      summary.go            — produces substrate summary for a namespace

  plugins/
    runtime/
      runtime.go            — wazero + Extism host setup
      load.go               — .wasm loading, validation, compilation cache
      host.go               — host functions exposed to plugins
      instance.go           — Plugin interface implementation wrapping wazero
    registry/
      registry.go           — plugin registration, discovery, lifecycle

  llm/
    router.go               — LLMRouter: selects provider + model per request
    budget.go               — token budget tracker, context window guard
    anthropic/
      provider.go           — Anthropic API implementation of LLMProvider
      models.go             — model IDs, context limits, tier mapping
    openai/
      provider.go           — OpenAI API implementation // stub
      models.go             // stub
    local/
      provider.go           — local/Ollama implementation // stub

  indexer/
    indexer.go              — Indexer: orchestrates full and incremental index
    watcher/
      watcher.go            — filesystem watcher (fsnotify)
      debounce.go           — debounce rapid file changes
    parser/
      parser.go             — routes files to language handlers
      treesitter.go         — tree-sitter integration for built-in languages
    walker/
      walker.go             — directory walker, respects .gitignore

  runner/
    runner.go               — Engine: the public-facing library entry point
    dag.go                  — DAG wiring: connects all nodes
    fanout.go               — WaitGroup fan-out node
    loop.go                 — cognitive loop execution
    budget.go               — per-turn token budget state
    context.go              — run context: carries IR, budget, loop state

  server/
    mcp/
      server.go             — MCP server implementation // stub
      tools.go              — MCP tool registrations // stub
    api/
      server.go             — REST API server // stub
      handlers.go           // stub
    ws/
      server.go             — WebSocket server // stub
```

---

## 5. cli/ and tui/

These live at the module root (not in `internal/`) because they are the
public-facing surfaces. They import from `internal/runner` and `internal/core`.

```
cli/
  root.go                   — root Cobra command, persistent flags
  query.go                  — ce query <text>
  index.go                  — ce index [path]
  project.go                — ce project {init|list|remove}
  token.go                  — ce token {create|list|revoke}
  plugin.go                 — ce plugin {build|install|list|remove|validate|dev}
  config.go                 — ce config {show|set|get}
  server.go                 — ce server {start|stop|status}
  completion.go             — ce completion {bash|zsh|fish|powershell}

tui/
  model.go                  — root Bubbletea model
  update.go                 — Msg handlers
  view.go                   — render functions
  styles.go                 — Lipgloss style definitions
  components/
    spinner.go
    progress.go
    thinking.go             — thinking stream renderer
    answer.go               — final answer renderer
    cost.go                 — cost display
```

---

## 6. Dependency Graph

Rules: arrows point from importer to imported. No cycles allowed.

```
cmd/ce/main.go
  → cli/
  → tui/
  → internal/runner
  → internal/config

cli/
  → internal/runner
  → internal/core
  → internal/config
  → internal/storage/queries  (token validation)

tui/
  → internal/runner
  → internal/core
  → internal/config

internal/runner
  → internal/agent/{preflight,strategizer,reviewer,synthesizer}
  → internal/graph/{substrate,activation,ontology}
  → internal/tools/{all}
  → internal/plugins/{runtime,registry}
  → internal/llm
  → internal/storage
  → internal/config
  → internal/core

internal/agent/*
  → internal/core
  → internal/llm
  → internal/graph/substrate   (reader only)
  NO dependency on internal/runner (avoids cycles)

internal/graph/*
  → internal/core
  → internal/storage
  NO dependency on internal/agent

internal/tools/*
  → internal/core
  → internal/graph/substrate   (reader only)
  NO dependency on internal/agent
  NO dependency on internal/runner

internal/plugins/*
  → internal/core
  NO dependency on internal/agent
  NO dependency on internal/graph

internal/llm/*
  → internal/core
  NO dependency on anything else internal

internal/indexer/*
  → internal/core
  → internal/graph/substrate   (writer)
  → internal/plugins/registry
  NO dependency on internal/agent
  NO dependency on internal/runner

internal/storage/*
  → internal/core
  NO dependency on anything else internal

internal/config
  → internal/core
  NO dependency on anything else internal

internal/core
  NO internal dependencies (the floor)
```

**The critical rule**: `internal/core` imports nothing internal. Anything that
would create a cycle is a sign that logic belongs in a higher layer.

---

## 7. The Engine Entry Point

`internal/runner/runner.go` is what `cli/` and `tui/` call. It is the single
public face of the library.

```go
// internal/runner/runner.go

package runner

import (
    "context"
    "github.com/atheory/context-engine/internal/core"
    // ... internal imports
)

// Engine is the assembled, ready-to-use context engine.
// Construct with New(). Call Query() to run a cognitive loop.
type Engine struct {
    cfg       *config.Config
    channels  core.AppChannels
    registry  *db.Registry
    buffer    writebuffer.Buffer
    substrate *substrate.ReadWriter
    plugins   *plugins.Registry
    llm       *llm.Router
    indexer   *indexer.Indexer
}

// New constructs a fully wired Engine from config.
// Opens all databases, starts the write buffer goroutine,
// loads plugins, mounts the active project graph.
// Call Close() when done.
func New(ctx context.Context, cfg *config.Config) (*Engine, error)

// Query runs the cognitive loop for a user query.
// Emits to channels as work progresses.
// Blocks until the answer is synthesized or an error occurs.
// Channels are drained by the caller (TUI or CLI renderer).
func (e *Engine) Query(ctx context.Context, query string) error

// Index runs a full index of the active project.
// Emits progress to channels.
func (e *Engine) Index(ctx context.Context) error

// Channels returns the AppChannels for this engine.
// The caller reads from these to render output.
func (e *Engine) Channels() core.AppChannels

// Close flushes the write buffer, closes all databases,
// unloads plugins, and shuts down background goroutines.
func (e *Engine) Close(ctx context.Context) error
```

---

## 8. Build Tags Strategy

No build tags required for Phase 1. Tag strategy is:

- `//go:build integration` — tests that require a real filesystem or network
- `//go:build wasm` — reserved for any future WASM compilation target

Standard `_test.go` convention for unit tests. No tags needed.

---

## 9. Test Package Conventions

| Package | Test approach |
|---------|---------------|
| `internal/core` | Pure unit tests, no external deps |
| `internal/storage` | In-memory SQLite, run migrations in TestMain |
| `internal/agent/*` | Mock LLMProvider from core interfaces |
| `internal/graph/*` | In-memory SQLite substrate |
| `internal/tools/*` | Mock SubstrateReader |
| `internal/plugins/runtime` | Real wazero, test fixtures in testdata/ |
| `internal/llm/*` | Mock HTTP server for API tests |
| `internal/runner` | Integration tests with `//go:build integration` |
| `cli/` | Cobra command testing with captured output |

All mocks implement the interfaces defined in `internal/core`. No mock generation
framework — hand-written mocks in `*_test.go` or `testdata/mock_*.go` files.

---

## 10. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Module path | `github.com/atheory/context-engine` |
| Entry point | `cmd/ce/main.go` (<50 lines) |
| Internal boundary | `internal/` — not importable outside module |
| Dependency floor | `internal/core` — zero internal imports |
| Interface location | `internal/core/interfaces.go` — all primary interfaces |
| Import cycle prevention | Enforced by package layout + dependency graph above |
| CLI library | Cobra (cli/) |
| TUI library | Bubbletea + Charm (tui/) |
| Test mocking | Hand-written, implements core interfaces |
| Build tags | integration, wasm only |

---

*Spec 2: Go Package Structure — v1.0 — February 2026*
*Next: Spec 3 — Engine Runner*
*Companion: Context Engine PRD v0.5 Sections 8, 9 | Decisions Log v1.0 Section 2*
