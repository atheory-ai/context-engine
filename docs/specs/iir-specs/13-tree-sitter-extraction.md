# Plan: One Tree-Sitter Parse, Consumed by Everything

Status: proposed. Resolves a design error found while validating IIR against the
real TypeScript plugin: **structural extraction is done with regex, and IIR
re-parses independently — three parses where there should be one.**

## The error

Context Engine already parses every file with tree-sitter and serializes the
concrete syntax tree (CST). Today that parse is wasted, and two consumers
reinvent it:

```
CE indexer ── tree-sitter parse ──► treeJSON ──► plugin.extract(filePath, content)
                                     │                └─ IGNORES tree, re-derives structure with REGEX
                                     └─ (tree otherwise unused)

CE IIR pass ── SEPARATE tree-sitter parse (Go) ──► FunctionIntents ──► correlate to plugin's regex nodes BY NAME
```

Three parses (one thrown away), two mechanisms (tree-sitter + regex), and a
fragile name-based correlation between them.

### Evidence

- CE sends the tree: `internal/plugins/runtime/wasm_language.go` marshals
  `{file_path, content, tree}` to `ce_language_extract`. The tree
  (`internal/indexer/parser/serialize.go` `SyntaxNode`) carries `type`,
  `startByte`/`endByte`, `startPosition`/`endPosition`, and `children`.
- The SDK ignores it: `packages/plugin-sdk/src/types.ts` defines
  `extract: (filePath: string, content: string) => ExtractionResult` — no tree
  parameter — so `plugins/typescript-language/src/extract.ts` uses
  `fnRegex`/`arrowRegex` on raw text.
- IIR re-parses: `internal/iir/extract.go` runs its own `smacker/go-tree-sitter`
  parse of the file content, then `internal/indexer/iirpass.go` correlates each
  `FunctionIntent` to a symbol node by `label` (function name).

### Why it's wrong

- **Regex silently drops real code** — class methods, non-line-start functions,
  multi-line signatures, overloads, namespaced functions, decorated methods. A
  knowledge graph whose value is structural accuracy cannot be built on it.
- **It discards work CE already did** — the accurate tree-sitter parse is right
  there, handed to the plugin, and thrown away.
- **It is the root of the IIR correlation fragility** — two extractors (Go
  tree-sitter vs JS regex) produce different function sets, forcing name-only
  matching with ambiguity-skipping and no positions (the `start_byte`
  follow-up). Parse once and that fragility disappears.

## Principle

**One tree-sitter parse per file (the indexer's), consumed by every extractor.**
CE owns the grammar and the parse; consumers walk the resulting CST. No consumer
re-parses, and nothing uses regex for structure.

```
CE indexer ── tree-sitter parse ──► CST ─┬─► plugin.extract(filePath, content, TREE)  → nodes/edges (+ positions)
                                          └─► IIR walks the SAME tree               → FunctionIntents (+ positions)
```

Extraction logic stays in plugins (extensibility — third parties add languages),
but plugins walk the tree CE provides instead of regexing. IIR, a CE-core Go
capability, reuses the indexer's parse instead of doing its own.

## Track A — kill regex in the plugin (SDK repo: `context-engine-sdk`)

Priority 1. The enabling data (the tree) is already delivered; this is SDK-side.

1. **Extend the contract**: `LanguageDefinition["extract"]` becomes
   `(filePath, content, tree: SyntaxNode | null) => ExtractionResult`. Add a
   `SyntaxNode` type mirroring CE's `serialize.go`.
2. **Wire the dispatch**: the `ce_language_extract` glue already receives
   `{file_path, content, tree}` from the host — pass `input.tree` through to
   `extract` (currently dropped).
3. **Add CST-walking helpers** to the SDK so plugin authors walk the tree
   ergonomically: `walk(node, fn)`, `findByType(node, type)`,
   `childByField(node, field)`, `text(node)`, position accessors.
4. **Rewrite the language extractors** (`typescript-language`, then
   `go-language`, `python-language`) to walk the CST instead of regex. Emit
   `start_byte`/`start_line` in symbol-node `properties` (free from the tree).
5. **Fallback**: if `tree` is null (no grammar), the plugin may keep a minimal
   text path or emit nothing — but for grammared languages the tree is always
   present.
6. Tests over the plugins' `fixtures/`; rebuild the default plugin WASM.

## Track B — IIR reuses the single parse (this repo: `context-engine`)

The natural follow-through: stop IIR's redundant re-parse and its name-based
correlation.

1. **Expose the native tree**: `internal/indexer/parser` gains a method
   returning the native `*sitter.Tree` (it currently parses then serializes and
   discards it). The indexer already parses each file here.
2. **IIR extracts from a node, not from content**: add
   `iir.ExtractAllFromNode(root, source)` alongside `ExtractAll`. The existing
   extractor already walks native `*sitter.Node`, so this is a small refactor —
   it just accepts a root instead of re-parsing.
3. **Thread it through**: `internal/indexer/indexer.go processFile` already holds
   the parsed tree; hand it to `extractFileIIR` instead of re-reading content.
4. **Exact correlation**: because IIR now walks the same tree, each
   `FunctionIntent` knows its start byte. Combined with Track A emitting
   `start_byte` on symbol nodes, correlation becomes `(file_path, name,
   start_byte)` — exact. This removes the ambiguity-skip and closes the
   `start_byte` follow-up.

Track B can land independently of Track A (it removes IIR's own re-parse
regardless), but the exact-correlation payoff needs Track A's node positions.

## Prerequisite for end-to-end validation (separate track)

The locally-built SDK plugin does not currently load into this CE checkout:
`validateExports` requires a `ce_plugin_manifest` export, but the javy build
produces the javy-stream ABI (`_start` + `config-schema`). This must be resolved
(in CE's `validateExports` or the SDK build recipe) to test either track against
a real index. Tracked separately; it blocks validation, not the design.

## Out of scope

- Moving structural extraction out of plugins into core (keep the plugin model
  for extensibility).
- Streaming/partial trees or a binary tree encoding — the JSON CST CE already
  sends is the contract; performance tuning is separate.
- New IIR node kinds or multi-function IIR.

## Verification

- **Track A**: index a fixture TS project with methods, overloads, arrow
  functions, and multi-line signatures; assert the plugin emits symbol nodes for
  all of them (cases regex missed), each with a `start_byte` property.
- **Track B**: assert IIR runs with no second parse (one `parser.ParseTree` call
  per file), and that a file with two same-named functions produces two distinct
  IIR rows correlated by position (no longer skipped as ambiguous).
- **End-to-end** (after the load blocker): `ce index --full` on the fixture with
  `iir.enabled` yields one `extracted` IIR row per function node.

## Result

One parse per file. No regex anywhere in the structural path. IIR built on the
same tree the indexer already produces — no re-parse, no name-guessing
correlation. The plugin model and extensibility are preserved; the redundancy
and fragility are gone.
