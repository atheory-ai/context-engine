import type { ExtractedFunction } from "./iir.js"

// ── Node and Edge types ────────────────────────────────────────────────────

export type NodeType =
  | "symbol"
  | "namespace"
  | "concept"
  | "file"
  | "directory"
  | string

export type EdgeType =
  | "calls"
  | "imports"
  | "implements"
  | "extends"
  | "contains"
  | "references"
  | "defines"
  | "belongs_to"
  | "synonym_of"
  | "co_activates"
  | "annotates"
  | string

export type SourceClass =
  | "structural"
  | "associative"
  | "speculative"
  | "derived"

export interface Node {
  id:          string
  type:        NodeType
  label:       string
  canonicalID: string
  sourceClass: SourceClass
  properties:  Record<string, unknown>
}

export interface Edge {
  id:          string
  sourceID:    string
  targetID:    string
  type:        EdgeType
  sourceClass: SourceClass
  properties:  Record<string, unknown>
}

// ── Extraction ─────────────────────────────────────────────────────────────

export interface ExtractionResult {
  nodes: Node[]
  edges: Edge[]
  // Optional lifted IIR, each attached to the symbol node it came from. Additive:
  // a plugin that doesn't lift IIR simply omits this and the host falls back to
  // its own extractor.
  iir?:  ExtractedFunction[]
}

// ── Syntax tree ──────────────────────────────────────────────────────────────
// The serialized tree-sitter concrete syntax tree the host parses and hands to
// language plugins. Extractors should walk this instead of matching raw text —
// the host already did the parse. Mirrors Context Engine's serialized SyntaxNode.

export interface Position {
  row:    number
  column: number
}

export interface SyntaxNode {
  /** tree-sitter node type, e.g. "function_declaration", "identifier". */
  type:          string
  /** true for grammar-named nodes, false for anonymous tokens ("(", "async"). */
  isNamed:       boolean
  /** the field this node occupies in its parent, e.g. "name", "body"; null if none. */
  fieldName:     string | null
  /** source text spanned by this node. */
  text:          string
  startByte:     number
  endByte:       number
  startPosition: Position
  endPosition:   Position
  /** child nodes; absent (null) on leaf nodes — use the SDK's tree helpers, which tolerate this. */
  children:      SyntaxNode[] | null
}

export interface ConceptSeed {
  term:        string
  definition?: string
  related?:    string[]
  synonyms?:   string[]
}

// ── Tool types ─────────────────────────────────────────────────────────────

export interface AnchorRef {
  type:       "symbol" | "namespace" | "concept" | "file"
  id:         string
  confidence: "high" | "medium" | "low"
}

export interface Anchor {
  ref:        AnchorRef
  node?:      Node
  edges:      Edge[]
  activation: number
}

export interface IR {
  mode:        "thinking" | "direct" | "audit"
  anchors:     AnchorRef[]
  predicates:  Record<string, string>
  openQueries: string[]
  maxLoops:    number
  kLimit:      number
  roleHint:    string
  modelTier:   string
}

export interface Emission {
  channel:   "thinking" | "action" | "debug" | "warning"
  content:   string
  metadata?: Record<string, unknown>
}

export interface ToolRequest {
  runID:     string
  turnID:    string
  loopIndex: number
  ir:        IR
  anchors:   Anchor[]
}

export interface ToolResult {
  emissions:     Emission[]
  proposedNodes: Node[]
  proposedEdges: Edge[]
}

// ── Substrate query ────────────────────────────────────────────────────────

export interface SubstrateQuery {
  projectID:      string
  nodeTypes?:     NodeType[]
  minActivation?: number
  properties?:    Record<string, string>
  limit?:         number
}

// ── Plugin definition types ────────────────────────────────────────────────

export interface LanguageDefinition {
  match:     (filePath: string) => boolean
  /**
   * Turn a file into graph nodes/edges. `tree` is the host's parsed CST — walk
   * it rather than matching raw text. It is null only when no grammar is
   * available for the file (rare); for grammared languages it is always present.
   */
  extract:   (filePath: string, content: string, tree: SyntaxNode | null) => ExtractionResult
  concepts?: ConceptSeed[]
}

export interface RoleDefinition {
  name:         string
  systemPrompt: string
  tools?:       string[]
}

export interface AnalyzerDefinition {
  name:        string
  description: string
  analyze:     (nodes: Node[]) => Edge[]
}

export interface SubstrateClient {
  query: (q: SubstrateQuery) => Node[]
}

export interface ToolDefinition {
  name:            string
  description:     string
  activationHint?: string
  activate:        (ir: IR) => boolean
  execute:         (request: ToolRequest, substrate: SubstrateClient) => ToolResult
}

// ── IIR conformance rules ───────────────────────────────────────────────────
// A plugin may contribute a rule pack — its "flavour" of code expectations — that
// the host merges over its built-in defaults (no WASM call; the manifest carries
// it as declarative data). The schema mirrors Context Engine's internal/iir rule
// model; the host is the authoritative validator, so these types are the
// authoring surface, not a second gate. See docs and internal/iir/rules.go.

export type IIRSeverity = "error" | "warning" | "info"

// Only FunctionIntent is a valid rule target today.
export type IIRTarget = "FunctionIntent"

// IIRExprPattern is a structural matcher over a normalized condition expression.
// A node matches when its operator is in `ops` (or `ops` is omitted, matching
// any) and — when `operandLiteral` is set — one of its direct operands is that
// literal. At least one of the two must be set.
export interface IIRExprPattern {
  ops?:            string[]
  operandLiteral?: string
}

export interface IIRRuleWhen {
  visibility?:      "public" | "private"
  hasFailureModes?: boolean
}

export interface IIRRuleRequire {
  explicitReturnType?:   boolean
  sideEffectsDeclared?:  boolean
  failureStrategy?:      string
  forbidConditionShape?: IIRExprPattern
}

export interface IIRRule {
  id:        string
  target:    IIRTarget
  severity:  IIRSeverity
  when?:     IIRRuleWhen
  require?:  IIRRuleRequire
}

export interface IIRRulePack {
  rules: IIRRule[]
}

export interface PluginDefinition {
  id:          string
  name:        string
  version:     string
  language?:   LanguageDefinition
  role?:       RoleDefinition
  analyzers?:  AnalyzerDefinition[]
  tools?:      ToolDefinition[]
  // iirRules is contributed to the host's IIR conformance layer via the manifest.
  iirRules?:   IIRRulePack
}
