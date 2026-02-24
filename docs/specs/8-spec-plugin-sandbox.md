# Context Engine — Spec 8: Plugin Sandbox CLI
## Implementation Spec — @ce/plugin-sandbox, Coverage Analysis, Dev Loop
### Version 1.0 | February 2026

---

> This is an implementation spec for the Plugin Sandbox CLI —
> the development runtime for plugin authors.
> Hand this document to Claude Code alongside spec-7-plugin-sdk.md.
> The sandbox is the headless version of a future CE Studio plugin view.
> Its JSON output format is a versioned contract.
> Companion: Context Engine PRD v0.5 Section 12. Decisions Log v1.0 Sections 5.8, 6.5.

---

## 1. Overview

The sandbox gives plugin authors honest feedback during development:

- **Does the plugin load?** (validates exports, manifest)
- **Does it extract the right things?** (coverage analysis against fixtures)
- **What changed between builds?** (extraction diff)
- **Does it pass validation?** (same validator as the engine)

The sandbox is CLI-only in Phase 1. Its JSON output format is designed to be
consumed by CE Studio's plugin development view in Phase 2 — no new
instrumentation will be needed when Studio adds that view.

---

## 2. Package Structure

```
packages/plugin-sandbox/
  src/
    index.ts          — CLI entry point, command routing
    commands/
      validate.ts     — ce plugin validate equivalent (TypeScript side)
      coverage.ts     — coverage analysis against fixtures
      diff.ts         — extraction diff between two builds
      run.ts          — run a single extraction and inspect output
    analysis/
      coverage.ts     — coverage algorithm
      diff.ts         — diff algorithm
      ast.ts          — AST symbol enumeration for fixtures
    output/
      report.ts       — JSON report assembly
      render.ts       — human-readable terminal rendering
      schema.ts       — SandboxReport TypeScript types
    runner/
      loader.ts       — WASM plugin loader (calls ce engine binary)
      fixtures.ts     — fixture file management
  tests/
    coverage.test.ts
    diff.test.ts
  package.json
  tsconfig.json
  build.mjs
  README.md
```

---

## 3. package.json

```json
{
  "name": "@ce/plugin-sandbox",
  "version": "0.1.0",
  "description": "Development runtime and coverage analysis for CE plugins",
  "type": "module",
  "bin": {
    "ce-sandbox": "./dist/index.js"
  },
  "scripts": {
    "build": "node build.mjs && chmod +x dist/index.js",
    "test":  "vitest run",
    "lint":  "eslint src"
  },
  "dependencies": {
    "@ce/plugin-sdk": "workspace:*"
  },
  "devDependencies": {
    "typescript": "*",
    "esbuild":    "*",
    "vitest":     "*",
    "chalk":      "^5.x",
    "cli-table3": "^0.6.x"
  }
}
```

The sandbox does not load WASM directly in TypeScript. It shells out to the
`ce plugin validate` command (the Go engine binary) to load and interrogate
the plugin. This means the sandbox always uses the same wazero loader as
production — no divergence between sandbox behavior and engine behavior.

---

## 4. CLI Commands

```
ce-sandbox validate <file.wasm>         — validate a plugin file
ce-sandbox coverage [path]              — run coverage analysis against fixtures
ce-sandbox diff <old.wasm> <new.wasm>   — show extraction changes between builds
ce-sandbox run <file.wasm> <fixture>    — run extraction on a single file

Flags (all commands):
  --json        output machine-readable JSON (for CE Studio consumption)
  --fixtures    path to fixtures directory (default: ./tests/fixtures)
  --ce          path to ce binary (default: looks in PATH)
```

---

## 5. Coverage Analysis Algorithm

Coverage measures how completely a plugin extracts the meaningful symbols
from a fixture file, relative to what a human reading the file would see.

### 5.1 How It Works

For each fixture file:

1. **Enumerate AST-visible symbols** — parse the fixture file using a
   language-appropriate heuristic (regex-based for the sandbox, not
   full AST) to count what a developer would expect to be indexed.
   These become the "expected" set.

2. **Run plugin extraction** — call the plugin's `extract()` function on
   the fixture file via the engine binary.

3. **Compare** — match extracted nodes against expected symbols.
   Compute coverage percentage. Identify missing symbols.

### 5.2 AST Symbol Enumeration

The sandbox uses language-specific regex patterns to enumerate expected symbols.
This is intentionally simpler than the engine's tree-sitter parsing — the goal
is a "reasonable human expectation" baseline, not perfect AST analysis.

```typescript
// packages/plugin-sandbox/src/analysis/ast.ts

export interface ExpectedSymbol {
  name:     string
  type:     "function" | "type" | "interface" | "method" | "const" | "var" | "class" | "other"
  line:     number
}

// Language-specific enumerators
const ENUMERATORS: Record<string, (content: string) => ExpectedSymbol[]> = {

  ".go": (content: string): ExpectedSymbol[] => {
    const symbols: ExpectedSymbol[] = []
    const lines = content.split("\n")

    lines.forEach((line, i) => {
      const lineNum = i + 1
      // Top-level functions
      const fnMatch = line.match(/^func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/)
      if (fnMatch) symbols.push({ name: fnMatch[1], type: "function", line: lineNum })

      // Type declarations
      const typeMatch = line.match(/^type\s+(\w+)\s+(?:struct|interface|func|\w)/)
      if (typeMatch) symbols.push({ name: typeMatch[1], type: "type", line: lineNum })

      // Constants (top-level only)
      const constMatch = line.match(/^\s{0,1}(\w+)\s*=/)
      if (constMatch && line.includes("const")) {
        symbols.push({ name: constMatch[1], type: "const", line: lineNum })
      }
    })

    return symbols
  },

  ".ts": (content: string): ExpectedSymbol[] => {
    const symbols: ExpectedSymbol[] = []
    const lines = content.split("\n")

    lines.forEach((line, i) => {
      const lineNum = i + 1
      const fnMatch = line.match(/^(?:export\s+)?(?:async\s+)?function\s+(\w+)/)
      if (fnMatch) symbols.push({ name: fnMatch[1], type: "function", line: lineNum })

      const classMatch = line.match(/^(?:export\s+)?class\s+(\w+)/)
      if (classMatch) symbols.push({ name: classMatch[1], type: "class", line: lineNum })

      const ifaceMatch = line.match(/^(?:export\s+)?interface\s+(\w+)/)
      if (ifaceMatch) symbols.push({ name: ifaceMatch[1], type: "interface", line: lineNum })

      const typeMatch = line.match(/^(?:export\s+)?type\s+(\w+)\s*=/)
      if (typeMatch) symbols.push({ name: typeMatch[1], type: "type", line: lineNum })
    })

    return symbols
  },

  ".py": (content: string): ExpectedSymbol[] => {
    const symbols: ExpectedSymbol[] = []
    const lines = content.split("\n")

    lines.forEach((line, i) => {
      const lineNum = i + 1
      const fnMatch = line.match(/^(?:async\s+)?def\s+(\w+)\s*\(/)
      if (fnMatch) symbols.push({ name: fnMatch[1], type: "function", line: lineNum })

      const classMatch = line.match(/^class\s+(\w+)/)
      if (classMatch) symbols.push({ name: classMatch[1], type: "class", line: lineNum })
    })

    return symbols
  },
}

export function enumerateExpectedSymbols(
  filePath: string,
  content: string
): ExpectedSymbol[] {
  const ext = filePath.substring(filePath.lastIndexOf("."))
  const enumerator = ENUMERATORS[ext]
  if (!enumerator) {
    // Unknown extension — return empty, coverage will be N/A
    return []
  }
  return enumerator(content)
}
```

### 5.3 Coverage Calculation

```typescript
// packages/plugin-sandbox/src/analysis/coverage.ts

import type { ExtractionResult, Node } from "@ce/plugin-sdk"
import { enumerateExpectedSymbols, type ExpectedSymbol } from "./ast.js"

export interface FileCoverageResult {
  fixturePath:      string
  expectedSymbols:  ExpectedSymbol[]
  extractedNodes:   Node[]
  matchedSymbols:   string[]   // expected symbol names found in extracted nodes
  missingSymbols:   string[]   // expected symbol names NOT found
  extraNodes:       Node[]     // extracted nodes with no matching expected symbol
  coveragePct:      number     // matchedSymbols.length / expectedSymbols.length * 100
}

export function computeCoverage(
  fixturePath:  string,
  content:      string,
  extraction:   ExtractionResult,
): FileCoverageResult {
  const expected = enumerateExpectedSymbols(fixturePath, content)

  if (expected.length === 0) {
    return {
      fixturePath,
      expectedSymbols:  [],
      extractedNodes:   extraction.nodes,
      matchedSymbols:   [],
      missingSymbols:   [],
      extraNodes:       extraction.nodes,
      coveragePct:      -1,  // -1 = N/A (unknown file type)
    }
  }

  // Match: an expected symbol is "covered" if any extracted node's
  // label or canonicalID contains the symbol name (case-sensitive).
  // This is intentionally loose — plugins may qualify names differently.
  const matched: string[] = []
  const missing: string[] = []

  for (const sym of expected) {
    const covered = extraction.nodes.some(
      node =>
        node.label === sym.name ||
        node.canonicalID.endsWith(`:${sym.name}`) ||
        node.canonicalID.endsWith(`/${sym.name}`)
    )
    if (covered) {
      matched.push(sym.name)
    } else {
      missing.push(sym.name)
    }
  }

  // Extra nodes: extracted nodes that don't match any expected symbol
  const extra = extraction.nodes.filter(
    node => !expected.some(
      sym =>
        node.label === sym.name ||
        node.canonicalID.endsWith(`:${sym.name}`) ||
        node.canonicalID.endsWith(`/${sym.name}`)
    )
  )

  const coveragePct = expected.length > 0
    ? (matched.length / expected.length) * 100
    : 0

  return {
    fixturePath,
    expectedSymbols:  expected,
    extractedNodes:   extraction.nodes,
    matchedSymbols:   matched,
    missingSymbols:   missing,
    extraNodes:       extra,
    coveragePct,
  }
}
```

---

## 6. Extraction Diff Algorithm

The diff shows what changed between two builds of the same plugin.
Primary use: `ce plugin dev` watch loop, showing what improved or regressed.

```typescript
// packages/plugin-sandbox/src/analysis/diff.ts

export interface ExtractionDiff {
  fixture:          string
  addedNodes:       string[]   // canonicalIDs present in new, absent in old
  removedNodes:     string[]   // canonicalIDs present in old, absent in new
  addedEdges:       string[]   // edge IDs present in new, absent in old
  removedEdges:     string[]   // edge IDs present in old, absent in new
  coverageDelta:    number     // new coverage% - old coverage%
}

export function diffExtractions(
  fixture:  string,
  oldResult: ExtractionResult,
  newResult: ExtractionResult,
  oldCoverage: number,
  newCoverage: number,
): ExtractionDiff {
  const oldNodeIDs = new Set(oldResult.nodes.map(n => n.canonicalID))
  const newNodeIDs = new Set(newResult.nodes.map(n => n.canonicalID))

  const oldEdgeIDs = new Set(oldResult.edges.map(e => e.id))
  const newEdgeIDs = new Set(newResult.edges.map(e => e.id))

  return {
    fixture,
    addedNodes:    [...newNodeIDs].filter(id => !oldNodeIDs.has(id)),
    removedNodes:  [...oldNodeIDs].filter(id => !newNodeIDs.has(id)),
    addedEdges:    [...newEdgeIDs].filter(id => !oldEdgeIDs.has(id)),
    removedEdges:  [...oldEdgeIDs].filter(id => !newEdgeIDs.has(id)),
    coverageDelta: newCoverage - oldCoverage,
  }
}
```

---

## 7. Sandbox Report — JSON Schema (Versioned Contract)

This is the JSON output that CE Studio will consume in Phase 2.
The schema version is a hard contract — bump it on any breaking change.

```typescript
// packages/plugin-sandbox/src/output/schema.ts

export interface SandboxReport {
  schemaVersion: 1                    // bump on breaking change
  pluginName:    string
  pluginVersion: string
  wasmHash:      string               // sha256 of .wasm content
  builtAt:       number               // unix ms

  // Per-fixture extraction results
  fixtureResults: FixtureResult[]

  // Aggregate across all fixtures
  aggregate: {
    totalExpectedSymbols: number
    totalExtractedNodes:  number
    coveragePct:          number      // -1 if all fixtures are unknown type
    totalEdges:           number
  }

  // Validation (same checks as engine)
  validation: {
    passed:   boolean
    errors:   string[]
    warnings: string[]
  }

  // Optional diff from previous build
  diff?: {
    previousWasmHash: string
    fixtures:         ExtractionDiff[]
    coverageDelta:    number          // aggregate coverage change
  }
}

export interface FixtureResult {
  fixturePath:      string
  astSymbols:       number            // expected symbols enumerated
  extractedNodes:   number            // nodes the plugin produced
  extractedEdges:   number            // edges the plugin produced
  coveragePct:      number            // -1 = N/A
  missingSymbols:   string[]          // expected but not extracted
  extraNodes:       string[]          // extracted but not expected (canonicalIDs)
}
```

### Rendering warnings and errors

```typescript
// packages/plugin-sandbox/src/output/report.ts

export function addValidationInsights(report: SandboxReport): void {
  const { aggregate, validation } = report

  // Coverage warning threshold
  if (aggregate.coveragePct >= 0 && aggregate.coveragePct < 80) {
    validation.warnings.push(
      `Coverage is ${aggregate.coveragePct.toFixed(1)}% — below the 80% recommended threshold. ` +
      `Review missing symbols and add extraction patterns for them.`
    )
  }

  // Fixtures with zero extraction
  for (const f of report.fixtureResults) {
    if (f.extractedNodes === 0 && f.astSymbols > 0) {
      validation.errors.push(
        `Fixture ${f.fixturePath}: plugin extracted 0 nodes from a file ` +
        `with ${f.astSymbols} expected symbols. ` +
        `Check that language.match() returns true for this file extension.`
      )
      validation.passed = false
    }
  }

  // No fixtures found
  if (report.fixtureResults.length === 0) {
    validation.warnings.push(
      `No fixture files found. Add sample files to tests/fixtures/ to enable coverage analysis.`
    )
  }
}
```

---

## 8. Terminal Rendering

Human-readable output for the CLI (non-`--json` mode).

```typescript
// packages/plugin-sandbox/src/output/render.ts

import chalk from "chalk"

export function renderReport(report: SandboxReport): void {
  console.log()
  console.log(chalk.bold(`${report.pluginName} v${report.pluginVersion}`))
  console.log(chalk.dim(`wasm: ${report.wasmHash.slice(0, 12)}...`))
  console.log()

  // ── Coverage table ──────────────────────────────────────────────────────
  if (report.fixtureResults.length > 0) {
    console.log(chalk.bold("Coverage"))
    console.log()

    for (const f of report.fixtureResults) {
      const pct = f.coveragePct === -1 ? "N/A" : `${f.coveragePct.toFixed(0)}%`
      const color = f.coveragePct >= 80 ? chalk.green
                  : f.coveragePct >= 50 ? chalk.yellow
                  : chalk.red
      const bar = coverageBar(f.coveragePct)

      console.log(`  ${f.fixturePath}`)
      console.log(`  ${color(pct.padStart(4))}  ${bar}  ${f.extractedNodes} nodes, ${f.extractedEdges} edges`)

      if (f.missingSymbols.length > 0 && f.missingSymbols.length <= 5) {
        console.log(chalk.dim(`         missing: ${f.missingSymbols.join(", ")}`))
      } else if (f.missingSymbols.length > 5) {
        console.log(chalk.dim(`         missing: ${f.missingSymbols.slice(0, 5).join(", ")} +${f.missingSymbols.length - 5} more`))
      }
      console.log()
    }

    // Aggregate
    const aggPct = report.aggregate.coveragePct
    const aggColor = aggPct >= 80 ? chalk.green : aggPct >= 50 ? chalk.yellow : chalk.red
    console.log(`  ${chalk.bold("Total")}  ${aggColor(`${aggPct.toFixed(1)}%`)}  ` +
      `(${report.aggregate.totalExtractedNodes}/${report.aggregate.totalExpectedSymbols} symbols)`)
    console.log()
  }

  // ── Diff (if present) ───────────────────────────────────────────────────
  if (report.diff) {
    const delta = report.diff.coverageDelta
    const sign = delta > 0 ? "+" : ""
    const color = delta > 0 ? chalk.green : delta < 0 ? chalk.red : chalk.dim
    console.log(chalk.bold("Changes from previous build"))
    console.log(`  Coverage: ${color(`${sign}${delta.toFixed(1)}%`)}`)

    for (const f of report.diff.fixtures) {
      if (f.addedNodes.length > 0 || f.removedNodes.length > 0) {
        console.log(`  ${f.fixture}`)
        if (f.addedNodes.length > 0) {
          console.log(chalk.green(`    + ${f.addedNodes.length} nodes: ${f.addedNodes.slice(0, 3).join(", ")}`))
        }
        if (f.removedNodes.length > 0) {
          console.log(chalk.red(`    - ${f.removedNodes.length} nodes: ${f.removedNodes.slice(0, 3).join(", ")}`))
        }
      }
    }
    console.log()
  }

  // ── Validation results ──────────────────────────────────────────────────
  if (report.validation.errors.length > 0) {
    for (const err of report.validation.errors) {
      console.log(chalk.red(`  ✗ ${err}`))
    }
    console.log()
  }

  if (report.validation.warnings.length > 0) {
    for (const warn of report.validation.warnings) {
      console.log(chalk.yellow(`  ⚠ ${warn}`))
    }
    console.log()
  }

  if (report.validation.passed && report.validation.warnings.length === 0) {
    console.log(chalk.green("  ✓ All checks passed"))
    console.log()
  }
}

function coverageBar(pct: number): string {
  if (pct < 0) return chalk.dim("░░░░░░░░░░ N/A")
  const filled = Math.round(pct / 10)
  const empty = 10 - filled
  const color = pct >= 80 ? chalk.green : pct >= 50 ? chalk.yellow : chalk.red
  return color("█".repeat(filled)) + chalk.dim("░".repeat(empty))
}
```

---

## 9. Plugin Loader — Shelling Out to Engine

The sandbox does not embed a WASM runtime. It calls the `ce` binary to run
plugin operations. This ensures sandbox behavior is identical to production.

```typescript
// packages/plugin-sandbox/src/runner/loader.ts

import { execSync, spawnSync } from "child_process"
import type { ExtractionResult } from "@ce/plugin-sdk"

export class PluginLoader {
  private ceBinary: string

  constructor(ceBinary = "ce") {
    this.ceBinary = ceBinary
    this.validateBinary()
  }

  private validateBinary(): void {
    try {
      execSync(`${this.ceBinary} version`, { stdio: "pipe" })
    } catch {
      throw new Error(
        `CE binary not found: ${this.ceBinary}\n` +
        `Install it or specify path with --ce flag.`
      )
    }
  }

  validate(wasmPath: string): { passed: boolean; errors: string[]; output: string } {
    const result = spawnSync(this.ceBinary, ["plugin", "validate", wasmPath, "--json"], {
      encoding: "utf8",
    })

    if (result.status !== 0) {
      return { passed: false, errors: [result.stderr], output: result.stdout }
    }

    try {
      const parsed = JSON.parse(result.stdout)
      return { passed: true, errors: [], output: result.stdout, ...parsed }
    } catch {
      return { passed: true, errors: [], output: result.stdout }
    }
  }

  extract(wasmPath: string, fixturePath: string, content: string): ExtractionResult {
    // Write content to a temp file, call ce plugin run-extract, read result
    const tmpInput = writeTempFile(JSON.stringify({ filePath: fixturePath, content }))

    const result = spawnSync(
      this.ceBinary,
      ["plugin", "extract", wasmPath, "--input", tmpInput, "--json"],
      { encoding: "utf8" }
    )

    cleanupTempFile(tmpInput)

    if (result.status !== 0) {
      throw new Error(`Extraction failed: ${result.stderr}`)
    }

    return JSON.parse(result.stdout) as ExtractionResult
  }
}
```

**Note for Spec 4 Claude Code**: The `ce plugin extract` subcommand is an
internal command used by the sandbox. It takes a `--input` JSON file with
`{ filePath, content }`, loads the plugin, calls `ce_language_extract`, and
outputs the `ExtractionResult` as JSON to stdout. Add this to the CLI spec.

---

## 10. Integration with `ce plugin dev`

The `ce plugin dev` watch loop (defined in Spec 6 `cli/plugin.go`) calls
the sandbox coverage analysis after each build. It can do this in two ways:

**Option A** — spawn `ce-sandbox coverage --json` as a subprocess and parse
the JSON output. Clean separation, but adds subprocess overhead per build.

**Option B** — the `ce` binary embeds the coverage analysis logic (rewritten
in Go). Same analysis, no subprocess, faster feedback.

**Decision: Option A for Phase 1.** The overhead per build is acceptable
(builds take seconds anyway), and it keeps the sandbox code in TypeScript
where it belongs. The coverage algorithm can be ported to Go in Phase 2 if
the subprocess overhead becomes a problem.

The `ce plugin dev` output is the sandbox `--json` report rendered inline
in the terminal. No separate terminal window required.

---

## 11. LLM Skills — Sandbox-Specific Content

The sandbox adds one file to `/llm-skills/`:

```markdown
# llm-skills/sandbox-workflow.md

## Using the Sandbox for Plugin Development

The sandbox validates plugins before they ship. Use this workflow:

1. Scaffold: `pnpm create ce-plugin`
2. Write your plugin in src/
3. Add fixture files to tests/fixtures/ (real code samples the plugin should handle)
4. Build: `ce plugin build`
5. Run coverage: `ce-sandbox coverage --json`
6. Interpret the report:
   - coveragePct >= 80% is the target
   - missingSymbols tells you what patterns to add to extract()
   - extraNodes tells you what patterns are over-matching

## Iterating on Coverage

If coverage is low, the missing symbols tell you exactly what to add.

Example: missingSymbols includes "init" and "TestMain" for a Go plugin.
This means your extract() regex doesn't match `func init()` or `func TestMain(`.
Add those patterns.

## What the Coverage Score Means

Coverage measures extraction completeness against a simple heuristic baseline.
A score of 75% means the plugin is finding 3 out of 4 symbols a developer
would expect to be indexed.

80% is the recommended minimum for publishing a plugin.
100% is achievable for languages with regular syntax.
For languages with complex macro systems or generated code, 80-90% is realistic.

## Common Low-Coverage Patterns

1. Method receivers: Go `func (r *Receiver) Method()` — regex must handle receiver
2. Interface methods: often missed because they're inside a block
3. Exported constants in const blocks: only the block keyword is on its own line
4. Anonymous functions assigned to vars: `var handler = func() {}`
```

---

## 12. Test Cases

### Coverage algorithm tests

```typescript
// packages/plugin-sandbox/tests/coverage.test.ts

import { describe, it, expect } from "vitest"
import { computeCoverage } from "../src/analysis/coverage.js"

describe("computeCoverage", () => {
  it("matches extracted nodes to expected symbols", () => {
    const content = `
package main

func main() {}
func helper(x int) string { return "" }
type Config struct { Port int }
`
    const extraction = {
      nodes: [
        { id: "abc", type: "symbol", label: "main",
          canonicalID: "main:main", sourceClass: "structural" as const, properties: {} },
        { id: "def", type: "symbol", label: "helper",
          canonicalID: "main:helper", sourceClass: "structural" as const, properties: {} },
        // Config is not extracted — intentionally missing
      ],
      edges: [],
    }

    const result = computeCoverage("main.go", content, extraction)

    expect(result.coveragePct).toBeCloseTo(66.7, 0) // 2 of 3 symbols
    expect(result.matchedSymbols).toContain("main")
    expect(result.matchedSymbols).toContain("helper")
    expect(result.missingSymbols).toContain("Config")
  })

  it("returns -1 coverage for unknown file extensions", () => {
    const result = computeCoverage("file.xyz", "some content", { nodes: [], edges: [] })
    expect(result.coveragePct).toBe(-1)
  })

  it("handles empty fixture", () => {
    const result = computeCoverage("empty.go", "", { nodes: [], edges: [] })
    expect(result.coveragePct).toBe(0)
    expect(result.expectedSymbols).toHaveLength(0)
  })
})
```

### Diff algorithm tests

```typescript
// packages/plugin-sandbox/tests/diff.test.ts

import { describe, it, expect } from "vitest"
import { diffExtractions } from "../src/analysis/diff.js"

describe("diffExtractions", () => {
  it("identifies added nodes between builds", () => {
    const old = { nodes: [
      { id: "1", canonicalID: "pkg:FuncA", type: "symbol" as const,
        label: "FuncA", sourceClass: "structural" as const, properties: {} }
    ], edges: [] }
    const next = { nodes: [
      { id: "1", canonicalID: "pkg:FuncA", type: "symbol" as const,
        label: "FuncA", sourceClass: "structural" as const, properties: {} },
      { id: "2", canonicalID: "pkg:FuncB", type: "symbol" as const,
        label: "FuncB", sourceClass: "structural" as const, properties: {} },
    ], edges: [] }

    const diff = diffExtractions("main.go", old, next, 50, 100)

    expect(diff.addedNodes).toContain("pkg:FuncB")
    expect(diff.removedNodes).toHaveLength(0)
    expect(diff.coverageDelta).toBe(50)
  })
})
```

---

## 13. Package Layout Summary

```
packages/plugin-sandbox/
  src/
    index.ts              — CLI entry, command routing
    commands/
      validate.ts         — validate subcommand
      coverage.ts         — coverage subcommand
      diff.ts             — diff subcommand
      run.ts              — run subcommand (single file extraction)
    analysis/
      coverage.ts         — computeCoverage()
      diff.ts             — diffExtractions()
      ast.ts              — enumerateExpectedSymbols() + language enumerators
    output/
      report.ts           — SandboxReport assembly, addValidationInsights()
      render.ts           — terminal rendering, coverageBar()
      schema.ts           — SandboxReport TypeScript types (versioned contract)
    runner/
      loader.ts           — PluginLoader (shells out to ce binary)
      fixtures.ts         — fixture discovery and loading
  tests/
    coverage.test.ts
    diff.test.ts
  package.json
  tsconfig.json
  build.mjs
```

---

## 14. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| WASM loading | Shells out to `ce` binary — not embedded in TypeScript |
| Coverage baseline | Regex-based symbol enumeration — intentionally simple |
| Coverage threshold | 80% recommended minimum (warning below, not error) |
| Zero extraction | Hard error — likely a match() bug |
| JSON output | Versioned schema (schemaVersion: 1) — CE Studio contract |
| Diff storage | Not persisted — caller provides previous ExtractionResult |
| `ce plugin dev` integration | Option A: subprocess, Phase 1 |
| Coverage for unknown extensions | Returns -1 (N/A), not 0% — honest about unknown |
| LLM skills addition | sandbox-workflow.md — how to iterate on coverage |

---

*Spec 8: Plugin Sandbox CLI — v1.0 — February 2026*
*All eight specs complete.*
*Companion: Context Engine PRD v0.5 Section 12 | Decisions Log v1.0 Sections 5.8, 6.5*
