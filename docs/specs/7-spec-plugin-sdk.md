# Context Engine — Spec 7: Plugin SDK TypeScript Package
## Implementation Spec — @ce/plugin-sdk, create-ce-plugin, Monorepo Structure
### Version 1.0 | February 2026

---

> This is an implementation spec for the Plugin SDK — a separate project
> from the engine, targeting plugin authors rather than engine users.
> Hand this document to Claude Code for the SDK monorepo.
> Companion: Context Engine PRD v0.5 Section 12. Decisions Log v1.0 Sections 5, 6.

---

## 1. Repository Overview

The Plugin SDK is a separate repository from the engine. It is a pnpm
monorepo containing three packages with a shared build toolchain.

```
ce-plugin-sdk/                  — repo root
  packages/
    plugin-sdk/                 — @ce/plugin-sdk
    plugin-sandbox/             — @ce/plugin-sandbox (Spec 8)
    create-ce-plugin/           — create-ce-plugin scaffolding CLI
  llm-skills/                   — LLM context files for plugin authoring
  examples/
    go-language/                — reference implementation (Go language plugin)
    hello-world/                — minimal single-file example
  pnpm-workspace.yaml
  package.json                  — root (scripts only, no dependencies)
  tsconfig.base.json            — shared TypeScript config
  .eslintrc.base.json           — shared ESLint config
  README.md
```

---

## 2. Root Configuration Files

```yaml
# pnpm-workspace.yaml
packages:
  - "packages/*"
  - "examples/*"
```

```json
// package.json (root)
{
  "name": "ce-plugin-sdk-monorepo",
  "private": true,
  "scripts": {
    "build":   "pnpm -r build",
    "test":    "pnpm -r test",
    "lint":    "pnpm -r lint",
    "clean":   "pnpm -r clean",
    "release": "changeset publish"
  },
  "devDependencies": {
    "@changesets/cli":      "^2.x",
    "typescript":           "^5.x",
    "eslint":               "^8.x",
    "@typescript-eslint/parser":   "^6.x",
    "@typescript-eslint/eslint-plugin": "^6.x",
    "vitest":               "^1.x",
    "esbuild":              "^0.20.x"
  }
}
```

```json
// tsconfig.base.json
{
  "compilerOptions": {
    "target":      "ES2020",
    "module":      "ESNext",
    "moduleResolution": "bundler",
    "strict":      true,
    "declaration": true,
    "declarationMap": true,
    "sourceMap":   true,
    "esModuleInterop": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noImplicitReturns": true,

    // Javy constraint: no Node.js APIs
    // This lib config targets the intersection of ES2020 and QuickJS
    "lib": ["ES2020"],
    "types": []   // no @types/node — enforces Javy compatibility
  }
}
```

The `"types": []` line is the key constraint. It prevents plugin authors from
accidentally importing Node.js types. If they try to use `fs.readFileSync`,
TypeScript errors immediately — before they ever hit a WASM build error.

---

## 3. @ce/plugin-sdk

The package every plugin imports. Pure TypeScript types plus the `definePlugin`
function. No runtime dependencies. No Node.js APIs.

### 3.1 Package Structure

```
packages/plugin-sdk/
  src/
    index.ts          — public API (re-exports everything)
    define.ts         — definePlugin() function
    types.ts          — all TypeScript types
    host.ts           — ce.* host function bindings
    ids.ts            — node_id(), edge_id() helpers
  package.json
  tsconfig.json
  build.mjs           — esbuild build script
  README.md
```

### 3.2 package.json

```json
{
  "name": "@ce/plugin-sdk",
  "version": "0.1.0",
  "description": "TypeScript SDK for authoring Context Engine plugins",
  "type": "module",
  "main":    "./dist/index.cjs",
  "module":  "./dist/index.js",
  "types":   "./dist/index.d.ts",
  "exports": {
    ".": {
      "import": "./dist/index.js",
      "require": "./dist/index.cjs",
      "types":   "./dist/index.d.ts"
    }
  },
  "files": ["dist", "README.md"],
  "scripts": {
    "build": "node build.mjs",
    "test":  "vitest run",
    "lint":  "eslint src",
    "clean": "rm -rf dist"
  },
  "devDependencies": {
    "typescript": "*",
    "esbuild":    "*",
    "vitest":     "*"
  }
}
```

### 3.3 types.ts — Complete Type Definitions

```typescript
// packages/plugin-sdk/src/types.ts

// ── Node and Edge types ────────────────────────────────────────────────────

export type NodeType =
  | "symbol"
  | "namespace"
  | "concept"
  | "file"
  | "directory"
  | string   // plugin-defined types

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
  | string   // plugin-defined types

export type SourceClass =
  | "structural"    // from static analysis — highest trust
  | "associative"   // learned from co-activation — medium trust
  | "speculative"   // suggested — lowest trust, awaiting confirmation
  | "derived"       // computed from other relationships

export interface Node {
  id:          string      // use nodeID() helper — do not generate manually
  type:        NodeType
  label:       string      // human-readable display name
  canonicalID: string      // fully-qualified identifier
  sourceClass: SourceClass
  properties:  Record<string, unknown>
}

export interface Edge {
  id:          string      // use edgeID() helper — do not generate manually
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
}

export interface ConceptSeed {
  term:        string      // lowercase-hyphenated
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
  channel:  "thinking" | "action" | "debug" | "warning"
  content:  string
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

// ── Substrate query (via ce.substrate_query host fn) ───────────────────────

export interface SubstrateQuery {
  projectID:     string
  nodeTypes?:    NodeType[]
  minActivation?: number
  properties?:   Record<string, string>
  limit?:        number
}

// ── Plugin definition types ────────────────────────────────────────────────

export interface LanguageDefinition {
  /**
   * Returns true if this plugin should process the given file path.
   * Called for every file during indexing. Keep this fast.
   */
  match: (filePath: string) => boolean

  /**
   * Extract nodes and edges from a file's content.
   * filePath is relative to the project root.
   * content is the raw file content as a string.
   */
  extract: (filePath: string, content: string) => ExtractionResult

  /**
   * Domain concept seeds contributed by this language plugin.
   * Injected into the Strategizer's domain vocabulary section.
   */
  concepts?: ConceptSeed[]
}

export interface RoleDefinition {
  /**
   * Name of this agent role.
   * Referenced by ce.yaml default_role or Strategizer role_hint.
   */
  name: string

  /**
   * System prompt for this role. Injected into agent nodes when active.
   * Be specific about the perspective this role brings.
   */
  systemPrompt: string

  /**
   * Names of tools this role has access to.
   * Empty = access to all tools.
   */
  tools?: string[]
}

export interface AnalyzerDefinition {
  name:        string
  description: string

  /**
   * Post-extraction analysis pass.
   * Receives all nodes extracted from a file and can produce additional edges.
   * Common use: relationship inference that requires seeing multiple nodes together.
   */
  analyze: (nodes: Node[]) => Edge[]
}

export interface ToolDefinition {
  name: string

  /**
   * Tool description shown to the Strategizer. Max 100 characters.
   * Write this as: "[verb] [what it does]"
   * Example: "Follow call chains from anchor points through the substrate"
   */
  description: string

  /**
   * Activation hint shown to the Strategizer alongside the description.
   * Tells the Strategizer which predicates or anchor types trigger this tool.
   * Example: "predicate.my-tool=true, or anchors contain symbol nodes"
   */
  activationHint?: string

  /**
   * Returns true if this tool should run given the current IR.
   * Must be a pure function — no side effects, no async.
   */
  activate: (ir: IR) => boolean

  /**
   * Execute the tool and return results.
   * substrate is a read-only view of the knowledge graph.
   */
  execute: (request: ToolRequest, substrate: SubstrateClient) => ToolResult
}

/**
 * SubstrateClient is provided to tools during execution.
 * Read-only access to the knowledge graph.
 * Uses ce.substrate_query host function internally.
 */
export interface SubstrateClient {
  query: (q: SubstrateQuery) => Node[]
}

// ── Top-level plugin definition ────────────────────────────────────────────

export interface PluginDefinition {
  /** Reverse-domain unique identifier. Example: "com.atheory.go-language" */
  id:       string
  name:     string
  version:  string

  /** Language handler — teaches the indexer about a language or framework */
  language?:  LanguageDefinition

  /** Agent role — contributed perspective for the cognitive loop */
  role?:      RoleDefinition

  /** Post-extraction analyzers */
  analyzers?: AnalyzerDefinition[]

  /** Cognitive loop tools */
  tools?:     ToolDefinition[]
}
```

### 3.4 define.ts — definePlugin()

```typescript
// packages/plugin-sdk/src/define.ts

import type { PluginDefinition } from "./types.js"

/**
 * definePlugin is the single entry point for all plugin authors.
 *
 * Usage:
 *   import { definePlugin } from "@ce/plugin-sdk"
 *
 *   export default definePlugin({
 *     id: "com.example.my-plugin",
 *     name: "My Plugin",
 *     version: "1.0.0",
 *     language: { match, extract, concepts },
 *     tools: [{ name, description, activate, execute }],
 *   })
 *
 * All sections (language, role, analyzers, tools) are optional.
 * A plugin with only a role definition is valid.
 * A plugin with only concept seeds is valid.
 */
export function definePlugin(definition: PluginDefinition): PluginDefinition {
  // Validate at definition time — catch errors before WASM compilation

  if (!definition.id || !definition.id.includes(".")) {
    throw new Error(
      `Plugin id must be reverse-domain format (e.g., "com.example.my-plugin"). Got: "${definition.id}"`
    )
  }

  if (!definition.name) {
    throw new Error("Plugin name is required")
  }

  if (!definition.version || !isValidSemver(definition.version)) {
    throw new Error(
      `Plugin version must be valid semver (e.g., "1.0.0"). Got: "${definition.version}"`
    )
  }

  // Validate language definition
  if (definition.language) {
    if (typeof definition.language.match !== "function") {
      throw new Error("language.match must be a function")
    }
    if (typeof definition.language.extract !== "function") {
      throw new Error("language.extract must be a function")
    }
    if (definition.language.concepts) {
      for (const seed of definition.language.concepts) {
        if (seed.term !== seed.term.toLowerCase()) {
          throw new Error(
            `Concept terms must be lowercase. Got: "${seed.term}"`
          )
        }
        if (!seed.term.match(/^[a-z][a-z0-9-]*$/)) {
          throw new Error(
            `Concept terms must be lowercase-hyphenated. Got: "${seed.term}"`
          )
        }
      }
    }
  }

  // Validate tools
  if (definition.tools) {
    for (const tool of definition.tools) {
      if (!tool.name) {
        throw new Error("Tool name is required")
      }
      if (!tool.description) {
        throw new Error(`Tool "${tool.name}" description is required`)
      }
      if (tool.description.length > 100) {
        throw new Error(
          `Tool "${tool.name}" description exceeds 100 characters (${tool.description.length}). ` +
          `The Strategizer receives this in its prompt — keep it concise.`
        )
      }
      if (typeof tool.activate !== "function") {
        throw new Error(`Tool "${tool.name}" activate must be a function`)
      }
      if (typeof tool.execute !== "function") {
        throw new Error(`Tool "${tool.name}" execute must be a function`)
      }
    }
  }

  // Validate analyzers
  if (definition.analyzers) {
    for (const analyzer of definition.analyzers) {
      if (!analyzer.name) {
        throw new Error("Analyzer name is required")
      }
      if (typeof analyzer.analyze !== "function") {
        throw new Error(`Analyzer "${analyzer.name}" analyze must be a function`)
      }
    }
  }

  return definition
}

function isValidSemver(version: string): boolean {
  return /^\d+\.\d+\.\d+(-[\w.]+)?(\+[\w.]+)?$/.test(version)
}
```

### 3.5 host.ts — Host Function Bindings

These are the TypeScript bindings to the `ce.*` host functions exposed by
the engine. Plugin authors import the high-level helpers (like `nodeID()`)
rather than calling host functions directly.

```typescript
// packages/plugin-sdk/src/host.ts

/**
 * Low-level host function declarations.
 * These are provided by the engine's wazero runtime.
 * Plugin authors should use the higher-level helpers below.
 */
declare function __ce_log(level: string, message: string): void
declare function __ce_emit(channel: string, content: string): void
declare function __ce_substrate_query(queryJSON: string): string
declare function __ce_get_config(key: string): string
declare function __ce_node_id(projectID: string, type: string, canonicalID: string): string
declare function __ce_edge_id(sourceID: string, type: string, targetID: string): string

// ── Logging ────────────────────────────────────────────────────────────────

export const log = {
  debug: (message: string) => __ce_log("debug", message),
  info:  (message: string) => __ce_log("info",  message),
  warn:  (message: string) => __ce_log("warn",  message),
  error: (message: string) => __ce_log("error", message),
}

// ── Emit ───────────────────────────────────────────────────────────────────

/**
 * Emit a message to an engine channel.
 * Plugins can only emit to: thinking, action, debug, warning.
 */
export function emit(channel: "thinking" | "action" | "debug" | "warning", content: string): void {
  __ce_emit(channel, content)
}

// ── Substrate ──────────────────────────────────────────────────────────────

import type { SubstrateQuery, Node, SubstrateClient } from "./types.js"

/**
 * createSubstrateClient returns a SubstrateClient for use in tool.execute().
 * Wraps the ce.substrate_query host function.
 */
export function createSubstrateClient(): SubstrateClient {
  return {
    query(q: SubstrateQuery): Node[] {
      const result = __ce_substrate_query(JSON.stringify(q))
      return JSON.parse(result) as Node[]
    }
  }
}

// ── Config ─────────────────────────────────────────────────────────────────

/**
 * getConfig reads a plugin config value from ce.yaml plugins[n].config.
 * Returns undefined if the key is not set.
 */
export function getConfig<T = unknown>(key: string): T | undefined {
  const result = __ce_get_config(key)
  if (!result) return undefined
  return JSON.parse(result) as T
}

// ── ID generation ──────────────────────────────────────────────────────────

import type { NodeType, EdgeType } from "./types.js"

/**
 * nodeID generates a deterministic node ID.
 * Always use this — never generate node IDs manually.
 * Produces the same ID as the Go engine for the same inputs.
 */
export function nodeID(projectID: string, type: NodeType, canonicalID: string): string {
  return __ce_node_id(projectID, type as string, canonicalID)
}

/**
 * edgeID generates a deterministic edge ID.
 * Always use this — never generate edge IDs manually.
 */
export function edgeID(sourceID: string, type: EdgeType, targetID: string): string {
  return __ce_edge_id(sourceID, type as string, targetID)
}
```

### 3.6 index.ts — Public API

```typescript
// packages/plugin-sdk/src/index.ts

// Everything a plugin author needs — imported from one place
export { definePlugin } from "./define.js"
export { log, emit, createSubstrateClient, getConfig, nodeID, edgeID } from "./host.js"
export type {
  // Core graph types
  Node,
  Edge,
  NodeType,
  EdgeType,
  SourceClass,
  ExtractionResult,
  ConceptSeed,
  // Cognitive loop types
  AnchorRef,
  Anchor,
  IR,
  Emission,
  ToolRequest,
  ToolResult,
  SubstrateQuery,
  SubstrateClient,
  // Plugin definition types
  LanguageDefinition,
  RoleDefinition,
  AnalyzerDefinition,
  ToolDefinition,
  PluginDefinition,
} from "./types.js"
```

### 3.7 Build Script

```javascript
// packages/plugin-sdk/build.mjs

import * as esbuild from "esbuild"

// ESM build (for import in modern environments)
await esbuild.build({
  entryPoints: ["src/index.ts"],
  bundle:      true,
  format:      "esm",
  outfile:     "dist/index.js",
  target:      "es2020",
  platform:    "neutral",    // no Node.js assumptions
  sourcemap:   true,
})

// CJS build (for require() compatibility)
await esbuild.build({
  entryPoints: ["src/index.ts"],
  bundle:      true,
  format:      "cjs",
  outfile:     "dist/index.cjs",
  target:      "es2020",
  platform:    "neutral",
  sourcemap:   true,
})

// Type declarations — generated by tsc separately
// Run: tsc --emitDeclarationOnly after esbuild
```

---

## 4. create-ce-plugin Scaffolding CLI

### 4.1 Package Structure

```
packages/create-ce-plugin/
  src/
    index.ts      — CLI entry point
    scaffold.ts   — file generation
    templates/    — template files (embedded as strings)
      index.ts.tmpl
      ce-plugin.json.tmpl
      package.json.tmpl
      tsconfig.json.tmpl
      eslintrc.json.tmpl
      gitignore.tmpl
      readme.md.tmpl
      tests/fixture.go.tmpl
      tests/language.test.ts.tmpl
  package.json
  tsconfig.json
```

### 4.2 package.json

```json
{
  "name": "create-ce-plugin",
  "version": "0.1.0",
  "description": "Scaffold a new Context Engine plugin",
  "type": "module",
  "bin": {
    "create-ce-plugin": "./dist/index.js"
  },
  "scripts": {
    "build": "node build.mjs && chmod +x dist/index.js",
    "test":  "vitest run"
  },
  "dependencies": {
    "prompts": "^2.x"
  },
  "devDependencies": {
    "typescript": "*",
    "esbuild":    "*",
    "@types/prompts": "^2.x"
  }
}
```

### 4.3 Scaffolding CLI — Interactive Flow

```typescript
// packages/create-ce-plugin/src/index.ts
#!/usr/bin/env node

import prompts from "prompts"
import { scaffold } from "./scaffold.js"

async function main() {
  console.log("\nContext Engine Plugin Scaffolder\n")

  const answers = await prompts([
    {
      type:    "text",
      name:    "name",
      message: "Plugin name (e.g., my-framework-plugin)",
      validate: (v: string) => /^[a-z][a-z0-9-]*$/.test(v) || "Use lowercase-hyphenated name",
    },
    {
      type:    "text",
      name:    "id",
      message: "Plugin ID (reverse-domain, e.g., com.example.my-plugin)",
      initial: (prev: string) => `com.example.${prev}`,
      validate: (v: string) => v.includes(".") || "Must be reverse-domain format",
    },
    {
      type:    "text",
      name:    "description",
      message: "Short description",
    },
    {
      type:    "multiselect",
      name:    "capabilities",
      message: "What will this plugin contribute? (space to select)",
      choices: [
        { title: "Language handler (file parsing + AST extraction)", value: "language", selected: true },
        { title: "Agent role (specialized reasoning perspective)", value: "role" },
        { title: "Analyzers (post-extraction analysis passes)", value: "analyzers" },
        { title: "Tools (cognitive loop tools)", value: "tools" },
      ],
    },
    {
      type:    "text",
      name:    "author",
      message: "Author name",
    },
    {
      type:    "text",
      name:    "dir",
      message: "Output directory",
      initial: (prev: any, values: any) => `./${values.name}`,
    },
  ], {
    onCancel: () => { console.log("Cancelled."); process.exit(0) }
  })

  await scaffold(answers)

  console.log(`
✓ Plugin scaffolded at ./${answers.dir}

Next steps:
  cd ${answers.dir}
  pnpm install
  pnpm build            # compile to .wasm
  ce plugin validate dist/${answers.name}.wasm
  ce plugin dev         # live development with coverage
`)
}

main().catch(console.error)
```

### 4.4 Scaffolded Project Structure

`npm create ce-plugin` / `pnpm create ce-plugin` generates:

```
<plugin-name>/
  src/
    index.ts              ← definePlugin entry (main file)
    language/
      match.ts            ← (if language capability selected)
      extract.ts
      concepts.ts
    roles/
      index.ts            ← (if role capability selected)
    analyzers/
      index.ts            ← (if analyzers capability selected)
    tools/
      index.ts            ← (if tools capability selected)
  tests/
    fixtures/
      example.go          ← (example fixture for Go-like language plugins)
    language.test.ts      ← (if language selected)
    tools.test.ts         ← (if tools selected)
  ce-plugin.json          ← plugin manifest
  package.json
  tsconfig.json
  .eslintrc.json
  .gitignore
  README.md
```

### 4.5 ce-plugin.json Template

```json
{
  "id":          "{{ID}}",
  "name":        "{{NAME}}",
  "version":     "0.1.0",
  "description": "{{DESCRIPTION}}",
  "author":      "{{AUTHOR}}",
  "entry":       "./src/index.ts",
  "output":      "./dist/{{SLUG}}.wasm"
}
```

### 4.6 Generated src/index.ts Template (language + tools)

```typescript
import { definePlugin, nodeID, edgeID, log } from "@ce/plugin-sdk"
import { match } from "./language/match.js"
import { extract } from "./language/extract.js"
import { concepts } from "./language/concepts.js"
import { myTool } from "./tools/index.js"

export default definePlugin({
  id:      "{{ID}}",
  name:    "{{NAME}}",
  version: "0.1.0",

  language: {
    match,
    extract,
    concepts,
  },

  tools: [myTool],
})
```

### 4.7 Generated tsconfig.json

```json
{
  "extends": "@ce/plugin-sdk/tsconfig.plugin.json",
  "compilerOptions": {
    "outDir":  "./dist",
    "rootDir": "./src"
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist"]
}
```

`@ce/plugin-sdk` ships a `tsconfig.plugin.json` that extends `tsconfig.base.json`
with the Javy constraints pre-applied (`"types": []`, `"lib": ["ES2020"]`).
Plugin authors extend this — they get the right constraints automatically.

### 4.8 Generated .eslintrc.json

```json
{
  "extends": ["@ce/plugin-sdk/eslint-plugin-ce"],
  "parserOptions": {
    "project": "./tsconfig.json"
  }
}
```

---

## 5. ESLint Plugin — @ce/eslint-plugin-ce

Correctness rules that catch plugin authoring mistakes at lint time
rather than at runtime. Ships as part of `@ce/plugin-sdk`.

```
packages/plugin-sdk/src/eslint/
  index.ts          — plugin entry, rule registry
  rules/
    extract-return-type.ts   — extract() must return { nodes, edges }
    concept-term-format.ts   — concept terms must be lowercase-hyphenated
    tool-description-length.ts — description max 100 chars
    no-node-apis.ts          — ban Node.js APIs (fs, path, process, etc.)
    pure-activate.ts         — activate() must not have side effects (heuristic)
    id-helpers-required.ts   — warn if node/edge IDs look manually constructed
```

### Rule: extract-return-type

```typescript
// Catches: extract() returning wrong shape
// Before: return { symbols: [...] }
// After:  return { nodes: [...], edges: [...] }

// Rule checks that any function named "extract" or
// assigned to language.extract returns an object with
// "nodes" and "edges" properties.
```

### Rule: no-node-apis

```typescript
// Banned identifiers and member expressions:
const BANNED = [
  "require",
  "process.env", "process.exit", "process.argv",
  "fs.readFile", "fs.writeFile", "fs.readFileSync",
  "path.join", "path.resolve",  // use string operations instead
  "__dirname", "__filename",
  "Buffer",
  "setTimeout", "setInterval", "clearTimeout", "clearInterval",
  "fetch",       // no network access in plugins
  "XMLHttpRequest",
  "WebSocket",
]
```

### Rule: tool-description-length

```typescript
// Error if tool description > 100 characters.
// The 100-char limit exists because descriptions are injected into
// the Strategizer's prompt — longer descriptions waste context budget.
```

### Rule: pure-activate

```typescript
// Heuristic — warns (not errors) if activate() contains:
// - Function calls other than IR property access
// - Any assignment statement
// Activate must be a pure predicate. Side effects break tool selection.
// Example warning: "activate() should be a pure function. Found call to log()."
```

---

## 6. LLM Skills Files

The SDK ships an `/llm-skills/` directory with structured markdown files
that LLMs read as context when generating plugins. These are the files
that make autonomous plugin generation reliable.

```
llm-skills/
  README.md                   — how to use these files with LLMs
  plugin-architecture.md      — the definePlugin contract precisely
  extraction-patterns.md      — patterns for different language types
  concept-design.md           — designing good concept vocabularies
  tool-design.md              — designing effective cognitive loop tools
  validation-checklist.md     — what makes a plugin production-quality
  anti-patterns.md            — common mistakes and why they fail
  worked-examples.md          — three complete annotated plugin examples
```

### plugin-architecture.md (key content)

```markdown
# Context Engine Plugin Architecture

## The definePlugin Contract

Every plugin exports a single default export from `definePlugin()`.
All sections are optional. The engine inspects capabilities from the
plugin manifest and only calls functions that are declared.

## Language Handler

`match(filePath)` is called for every file during indexing.
Return true if your plugin handles this file. Keep it fast — regex only.

`extract(filePath, content)` receives the file path and raw content.
Return { nodes: Node[], edges: Edge[] }.

Always use nodeID() and edgeID() helpers — never construct IDs manually.
The engine uses deterministic hashing; inconsistent IDs break the graph.

## Node ID Conventions

Symbol nodes:    "package/path:SymbolName"
Namespace nodes: "package/path"
Concept nodes:   "lowercase-hyphenated-term"
File nodes:      "relative/path/from/root.ext"

## Edge Source Classes

"structural"  — from static analysis (highest trust, set this for AST edges)
"associative" — learned from patterns (let the engine set this)
"speculative" — uncertain relationships (use for inferred edges)

## Tool Activation

activate(ir) must be a PURE FUNCTION. No side effects, no logging, no state.
It is called on every IR to determine if the tool should run.
Check ir.predicates["your-predicate"] === "true" for explicit activation.
Check anchor types for implicit activation.

## Common Mistakes

1. Returning { symbols: [] } from extract() instead of { nodes: [], edges: [] }
2. Using fs, path, or process — these don't exist in the WASM sandbox
3. Generating node IDs as strings instead of using nodeID()
4. tool.description over 100 characters
5. Side effects in activate()
```

---

## 7. Examples

### examples/hello-world/src/index.ts

The minimal plugin. Ships in the repo as the simplest possible reference.

```typescript
import { definePlugin } from "@ce/plugin-sdk"

export default definePlugin({
  id:      "com.example.hello-world",
  name:    "Hello World Plugin",
  version: "0.1.0",

  // A language plugin that matches .hello files
  language: {
    match: (filePath) => filePath.endsWith(".hello"),

    extract: (filePath, content) => ({
      nodes: [{
        id:          nodeID("", "file", filePath),
        type:        "file",
        label:       filePath,
        canonicalID: filePath,
        sourceClass: "structural",
        properties:  { lineCount: content.split("\n").length },
      }],
      edges: [],
    }),

    concepts: [
      { term: "hello-file", definition: "A .hello source file" }
    ],
  },
})
```

### examples/go-language/ — Reference Implementation

The Go language plugin is the most important example — it's the first
production plugin and proves the full pipeline. Its structure:

```
examples/go-language/
  src/
    index.ts
    language/
      match.ts          — *.go, exclude vendor/
      extract.ts        — parse Go syntax: functions, types, interfaces, imports
      concepts.ts       — Go-specific concept seeds
    analyzers/
      interface-impl.ts — detect which types implement which interfaces
  tests/
    fixtures/
      simple.go         — basic function, type, import
      interface.go      — interface definition and implementations
      complex.go        — methods, embedded types, goroutines
    language.test.ts
  ce-plugin.json
  package.json
```

The Go plugin does NOT use tree-sitter (that's the engine's built-in parser).
It uses regex and simple string analysis — sufficient for the reference
implementation. Full tree-sitter integration is a future enhancement.

---

## 8. Package Layout Summary

```
packages/plugin-sdk/
  src/
    index.ts          — public re-exports
    types.ts          — all TypeScript types
    define.ts         — definePlugin() with validation
    host.ts           — ce.* host function bindings + helpers
    ids.ts            — nodeID(), edgeID() (re-exported from host.ts)
    eslint/
      index.ts        — ESLint plugin entry
      rules/          — individual rule implementations
    tsconfig.plugin.json  — base tsconfig for plugins to extend
  dist/               — built output
  package.json
  tsconfig.json
  build.mjs

packages/create-ce-plugin/
  src/
    index.ts          — CLI entry, prompts flow
    scaffold.ts       — directory + file generation
    templates/        — embedded template strings
  dist/
  package.json
  tsconfig.json
  build.mjs
```

---

## 9. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Monorepo tool | pnpm workspaces |
| TypeScript target | ES2020, `"types": []` (no Node.js APIs) |
| Build tool | esbuild (same tool as engine build pipeline uses) |
| definePlugin() | Single entry point — validates at definition time |
| ID generation | Always via nodeID()/edgeID() helpers — never manual |
| Host function style | Low-level `__ce_*` declarations + high-level helpers |
| ESLint rules | Correctness rules (not just style) — ship with SDK |
| Javy constraints enforced by | `"types": []` in tsconfig + `no-node-apis` ESLint rule |
| Reference implementation | Go language plugin in examples/ |
| LLM skills | Shipped in /llm-skills/ — prose docs for LLM context |

---

*Spec 7: Plugin SDK TypeScript Package — v1.0 — February 2026*
*Next: Spec 8 — Plugin Sandbox CLI*
*Companion: Context Engine PRD v0.5 Section 12 | Decisions Log v1.0 Sections 5, 6*
