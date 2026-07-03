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

// CompletionRequest is the request to an LLM provider.
type CompletionRequest struct {
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature float32
	System      string
	Thinking    *ThinkingConfig // nil = disabled
}

// ThinkingConfig enables extended reasoning on supported models.
type ThinkingConfig struct {
	BudgetTokens int
}

// CompletionResponse is the response from an LLM provider.
type CompletionResponse struct {
	Content      string
	ThinkingText string // empty if thinking not enabled
	TokensIn     int
	TokensOut    int
	Model        string
	FinishReason string
}

// Message is a single turn in a conversation.
type Message struct {
	Role    string // user | assistant
	Content string
}

// ModelInfo is metadata about a configured LLM model.
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
	// Extensions returns the file extensions this handler processes.
	// Example: []string{".go"} or []string{".ts", ".tsx", ".js", ".jsx"}
	Extensions() []string

	// GrammarPath returns the path to a tree-sitter grammar WASM file.
	// Returns empty string if using the built-in grammar or no grammar is needed.
	GrammarPath() string

	// Match returns true if this handler should process the given file path.
	// Default: check Extensions(). Override for custom matching logic.
	Match(filePath string) bool

	// HasCustomMatch returns true if Match() has been overridden beyond extension matching.
	HasCustomMatch() bool

	// Extract parses a file and returns the nodes and edges to add to the graph.
	// treeJSON is the serialized SyntaxTree (JSON), or nil if no grammar is available.
	Extract(filePath string, content []byte, treeJSON []byte) (ExtractionResult, error)

	// Concepts returns the concept seeds this language contributes.
	Concepts() []ConceptSeed
}

// ExtractionResult is the output of a language handler's Extract call.
type ExtractionResult struct {
	Nodes []Node
	Edges []Edge
}

// ConceptSeed is a vocabulary entry contributed by a language handler or plugin.
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

// ToolRequest is the input to a tool's Execute method.
type ToolRequest struct {
	RunID     RunID
	TurnID    TurnID
	LoopIndex int
	ProjectID ProjectID
	IR        IR
	Anchors   []Anchor
	Substrate SubstrateReader
}

// ToolResult is the output of a tool's Execute method.
type ToolResult struct {
	Emissions []Emission
	// Proposed substrate changes. The Reviewer decides whether to apply.
	ProposedNodes []Node
	ProposedEdges []Edge
}

// ============================================================
// Substrate
// ============================================================

// SubstrateReader is the read-only substrate view used by tools and the activation layer.
// All methods are scoped to a specific project.
type SubstrateReader interface {
	// Node retrieval
	GetNode(ctx context.Context, projectID ProjectID, nodeID NodeID) (*Node, error)
	GetNodeByCanonicalID(ctx context.Context, projectID ProjectID, canonicalID string) (*Node, error)
	GetNodesByNamespacePrefix(ctx context.Context, projectID ProjectID, prefix string, limit int) ([]Node, error)
	GetConceptNodes(ctx context.Context, projectID ProjectID, term string) ([]Node, error)
	GetNodesForFile(ctx context.Context, projectID ProjectID, filePath string) ([]Node, error)
	GetNodesBySuffix(ctx context.Context, projectID ProjectID, suffix string, limit int) ([]Node, error)

	// Top-K activation query (hot path — must use index)
	GetTopKActivated(ctx context.Context, projectID ProjectID, k int) ([]NodeWithActivation, error)

	// Edge retrieval
	GetEdgesFrom(ctx context.Context, projectID ProjectID, nodeID NodeID) ([]EdgeWithWeight, error)
	GetEdgesTo(ctx context.Context, projectID ProjectID, nodeID NodeID) ([]EdgeWithWeight, error)
	GetEdgesBetween(ctx context.Context, projectID ProjectID, sourceID, targetID NodeID) ([]EdgeWithWeight, error)

	// Concept seeds
	GetConceptSeeds(ctx context.Context, projectID ProjectID) ([]ConceptSeed, error)
	GetOrgConceptSeeds(ctx context.Context) ([]ConceptSeed, error)

	// ── Tool-specific queries ─────────────────────────────────────────────

	// For callgraph tool — multi-hop BFS via recursive CTE
	GetCallers(ctx context.Context, projectID ProjectID, nodeID NodeID, maxDepth int) ([]NodeWithActivation, error)
	GetCallees(ctx context.Context, projectID ProjectID, nodeID NodeID, maxDepth int) ([]NodeWithActivation, error)

	// For references tool — all incoming references with edge metadata
	GetReferences(ctx context.Context, projectID ProjectID, nodeID NodeID) ([]ReferenceResult, error)

	// For crossproject tool — searches the org graph across all projects
	FindInOrgGraph(ctx context.Context, canonicalID string, nodeType string) ([]OrgMatch, error)

	// For concepts tool
	GetConceptImplementors(ctx context.Context, projectID ProjectID, conceptNodeID NodeID) ([]NodeWithActivation, error)
	GetConceptSeed(ctx context.Context, projectID ProjectID, term string) (*ConceptSeed, error)

	// For filecontext tool
	GetFileNode(ctx context.Context, projectID ProjectID, filePath string) (*Node, error)
	GetFileImports(ctx context.Context, projectID ProjectID, fileNodeID NodeID) ([]Node, error)

	// For summary tool
	GetNamespaceMembers(ctx context.Context, projectID ProjectID, namespaceNodeID NodeID) ([]Node, error)
	GetNamespaceDependencies(ctx context.Context, projectID ProjectID, namespaceNodeID NodeID) ([]Node, error)
	GetNamespaceDependents(ctx context.Context, projectID ProjectID, namespaceNodeID NodeID) ([]Node, error)
}

// SubstrateQuery is a flexible read query against the substrate.
// Kept for internal use in substrate/reader.go.
type SubstrateQuery struct {
	ProjectID     ProjectID
	NodeTypes     []string
	MinActivation float64
	Properties    map[string]string // json_extract filters
	Limit         int
}

// SubstrateWriter is the write side of the substrate.
// Only the Reviewer and the indexer use this.
// All writes go through the write buffer — this interface wraps
// the buffer's Send method with typed operations.
type SubstrateWriter interface {
	// Node and edge upserts (go through write buffer)
	UpsertNode(ctx context.Context, node Node) error
	UpsertEdge(ctx context.Context, edge Edge) error

	// IIR upsert — extracted/intended intent per function node (write buffer)
	UpsertIIR(ctx context.Context, record IIRRecord) error

	// Activation updates (high frequency — write buffer deduplicates)
	UpdateActivation(ctx context.Context, nodeID NodeID, activation float64) error

	// Edge weight updates (from Hebbian learning)
	UpdateEdgeWeight(ctx context.Context, update WeightUpdate) error

	// Decay all edges for a project (single SQL UPDATE — bypasses write buffer)
	DecayEdgeWeights(ctx context.Context, projectID ProjectID, decayRate float64) error

	// Enrichment proposals from Reviewer (Reviewer-approved substrate changes)
	ApplyEnrichment(ctx context.Context, enrichment Enrichment) error

	// ResetActivation zeroes all activation values for a project.
	ResetActivation(ctx context.Context, projectID ProjectID) error

	// Flush blocks until write buffer is empty.
	Flush(ctx context.Context) error
}

// SubstrateAccessor combines read and write substrate access.
// Used by the activation layer, which must read edges and write activation updates.
// *graph/substrate.ReadWriter satisfies this interface.
type SubstrateAccessor interface {
	SubstrateReader
	SubstrateWriter
}

// Enrichment is a substrate change made by the Reviewer during cognitive loops.
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
