# Slice 8+ (RFC): IIR Engine Integration

Status: historical RFC; materially superseded by the current
[IIR capability guide](../../iir.md) and the semantic-platform specs. Parts of
the intended integration shipped, but the implementation changed: index-time
source lift is plugin-owned, host validation persists it; tree-sitter runs as
WASM on wazero with CGO disabled; and the semantic-plan pipeline is documented
under `docs/specs/19`–`31`.

Read the remainder as decision history, not current architecture. The final
slice (NL → IIR) is specced in `12-nl-to-iir.md`.

## Context

Today `internal/iir` (~4,000 lines) is a complete but self-contained loop —
verify, generate code, generate tests, repair — with a plugin surface that is
Go interfaces only. It imports nothing from the rest of the engine, only the
`ce iir` CLI consumes it, and it is fed by hand-authored YAML.

To be useful in the product we need three things it doesn't have yet:

1. **IIR for real code, without hand-authoring** — extract it from the indexed
   codebase and keep it in the substrate.
2. **Team-specific code preferences** — let plugins add rules/"flavours" so the
   engine writes and checks the kind of code a team wants.
3. **A front door on every surface** — CLI, MCP, and API, so an agent or user
   can turn intent into verified code.

The design principle that makes this tractable: **only natural-language → IIR
needs a model. Everything else is deterministic.**

- source → IIR (extraction) — deterministic AST walk
- IIR → code / tests (generation) — deterministic
- IIR ⇄ IIR (comparison, rules, repair) — deterministic
- **NL → IIR (shaping) — the one model-backed hop**

## Historical load-bearing decision: IIR is a host capability, not a plugin

This RFC proposed a host IIR extractor built on CGO tree-sitter. That proposal
was not adopted. The current pure-Go design instead has WASM language plugins
lift indexed IIR, while the host validates and persists it. The standalone host
helpers remain exposed to plugins through `ce.iir_*` functions:

- Plugins **call** IIR through new `ce.iir_*` host functions.
- Plugins **extend** IIR by declaring rule packs in their manifest; the host
  merges them and runs IIR with the merged set.

This dissolves the runtime split: nothing IIR-related runs in WASM, and the
Slice 5 Go plugin surface (`Extractor`/`Comparator`/`Emitter`/`RulePack`)
becomes the host-side shape that the WASM manifest capability feeds into.

## Decisions

### D1 — Extract eagerly at index time (not just-in-time)

Extracted IIR is produced during `ce index`, alongside AST node/edge extraction,
and persisted. Rationale:

- Matches CE's eager-index / lazy-query model.
- Nearly free: the tree is already parsed for node extraction; IIR is one more
  walk. JIT would re-parse later (CE doesn't cache trees).
- Rides the existing incremental machinery (`file_hashes`): pay once, refresh
  only changed files.
- Only a full pass unlocks repo-wide capability — running rule packs across the
  whole codebase, "find all functions with undeclared side effects", feeding IIR
  into queries.

`ce iir verify` (in-process, one file) remains the **on-demand fallback** for
un-indexed code. Model-backed *intended* IIR is created on demand and is not an
indexing concern.

### D2 — Store IIR in a dedicated table, per function node

CE is strictly table-per-concern (`nodes`, `edges`, `edge_weight`,
`concept_seeds`, `enrichments`, `file_hashes`, …). IIR is a concern, so it gets a
table — not a blob in `nodes.properties`, and not `enrichments` (that table is an
agent-action audit log: `run_id`/`turn_id`/`before_state`/`after_state`).

```sql
CREATE TABLE iir (
  id          TEXT PRIMARY KEY,
  project_id  TEXT NOT NULL,
  node_id     TEXT NOT NULL,        -- the function symbol node
  kind        TEXT NOT NULL,        -- 'extracted' | 'intended'
  language    TEXT NOT NULL,
  iir         TEXT NOT NULL,        -- FunctionIntent JSON
  source_hash TEXT,                 -- staleness vs file_hashes
  run_id      TEXT,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  UNIQUE(project_id, node_id, kind)
);
CREATE INDEX idx_iir_node ON iir(project_id, node_id);
CREATE INDEX idx_iir_kind ON iir(project_id, kind);
```

Granularity is per **function node**, reusing the AST boundaries the indexer
already produces — no new breakdown logic. The `kind` discriminator lets
`extracted` and `intended` IIR coexist, so verification becomes a join on
`node_id` and the substrate itself holds both sides of the comparison.

Writes go through the **write buffer** (hard constraint) via a new
`OpUpsertIIR`, mirroring `OpUpsertNode`.

### D3 — Extraction runs in the post-extraction hook, correlating to nodes

The proposal below described a post-extraction Go-native pass that correlated
host-extracted `FunctionIntent`s with nodes. Current code instead remaps the
plugin-attached `nodeId` directly to the substrate node ID, avoiding this
correlation step.

The existing `Analyzer` interface is `(nodes) → edges`; IIR needs `(content,
nodes) → iir records`. So this hook is generalized to a Go-native, node-data pass
— a small, reusable extension beyond IIR.

Correlation key: `(file_path, function name)`. `file_path` is a known node
property convention; the name is the node `label`. Where a file has same-named
functions (overloads), disambiguate by start position (see Key risk — the
position data is available host-side).

### D4 — Plugins extend IIR through merged rule packs (tier 1)

"Flavours of code writing" splits into two tiers:

- **Tier 1 (this RFC): declarative rule packs.** A plugin declares IIR rules in
  its manifest (`PluginCapabilities`). The host collects rule packs from every
  loaded plugin, merges them over the built-in defaults (`MergeRulePacks`,
  already built), and runs verification/generation with the merged set. Host-run,
  deterministic, safe. Covers most of "write the code I want" — Result types
  only, no throws in public APIs, naming/return conventions, etc.
- **Tier 2 (deferred): custom emitters/comparators.** Plugin-side codegen that
  generates source differently. A bigger surface (plugin code emitting source);
  design the seam, don't build it here.

### D5 — `ce.iir_*` host functions

New host functions in `internal/plugins/runtime/host.go`, registered under the
`ce` namespace alongside `ce.node_id` / `ce.substrate_query`:

- `ce.iir_extract(language, source, target) → FunctionIntent`
- `ce.iir_verify(intent, source) → Report`
- `ce.iir_generate(intent) → source`
- `ce.iir_generate_tests(intent) → TestArtifact`

Note: `HostDeps.Substrate` is nil during indexing, so index-time IIR writes
(not substrate reads); the `ce.iir_*` read/query calls are for query/agent-time
plugin use.

### D6 — "Intent → code" on every surface

One core capability, thin adapters. The core already exists
(`GenerateFunction`, `VerifySource`, `RepairLoop`) and the CLI already exposes
`ce iir generate|verify|gen-tests|repair`. Add:

- **MCP**: register an `iir.*` tool (`mcp.RegisterTool`).
- **API**: an `/api/iir/...` route.
- **NL → IIR**: the shaping step uses `internal/llm` (router exists). Only this
  hop is model-backed; everything downstream is the deterministic pipeline.

## Key risk — investigated, downgraded

Original concern: node correlation depends on the external TS plugin's node
schema, which isn't visible from this repo. Investigation findings:

- **Node properties are plugin pass-through.** `wasm_language.go:Extract`
  unmarshals the plugin's output straight into `core.Node`; the host imposes no
  location contract. `file_path` is a known convention (`filecontext.go`); a
  line/span is *not* host-guaranteed. No in-repo fixture captures a real TS node,
  and the built `typescript.wasm` is absent in dev checkouts (placeholder only).
- **But position data provably exists in the pipeline.** The serialized tree
  (`parser/serialize.go`, `SyntaxNode`) carries `StartByte`/`EndByte` and
  `StartPosition`/`EndPosition` (row/col) for every node — available host-side at
  extraction time to both the plugin and our IIR pass.

So this is a design choice, not a blocker. Correlate by `(file_path, name)`, and
resolve same-name collisions one of two ways:

1. **Plugin-contract (preferred):** document that IIR-capable language plugins
   include `start_byte` (or `start_line`) in function-node properties. Trivial —
   the tree already exposes it — and we control the SDK. Correlation becomes
   `(file_path, name, start_byte)`, bulletproof.
2. **Host-side fallback:** when a node lacks a position, match by name; treat a
   same-name collision within a file as ambiguous (log + skip correlation for
   that function). Rare at TS module scope.

Residual unknown (not blocking): the exact `label` / `canonical_id` the *current*
`typescript.wasm` emits — confirm when the plugin/SDK is available; the design
above only assumes `file_path` + name, which any reasonable symbol node carries.

## Out of scope

- Tier 2 custom emitters/comparators (plugin-side codegen).
- Multi-file / cross-function IIR (call-graph-level intent).
- Languages without an IIR extractor (TS only, today).
- Non-function IIR node kinds.

## Slice plan

1. **IIR storage** — `iir` table migration, `queries/iir.go`, `OpUpsertIIR` in
   the write buffer. (No behavior change; storage only.)
2. **Index-time extraction** — generalize the post-extraction hook to a
   Go-native pass; run the built-in TS IIR extractor; correlate to nodes by
   `(file_path, name)`; write `extracted` IIR. Config/capability gated
   (`iir.enabled`, TS only). Not blocked (see Key risk); the `start_byte` node
   contract is a small SDK follow-up for overload disambiguation.
3. **Plugin rule contributions** — extend `PluginManifest.Capabilities` to carry
   IIR rule packs; host aggregates + merges over defaults; extraction/verify use
   the merged set.
4. **`ce.iir_*` host functions** — expose extract/verify/generate/gen-tests to
   plugins.
5. **Surface endpoints** — MCP tool + API route over the core capability.
6. **NL → IIR shaping** — model-backed stage via `internal/llm`, feeding the
   deterministic generate → verify → repair pipeline. (Model boundary; keep the
   stage an interface with the deterministic path as the fallback.)

Slices 1–5 are deterministic and follow the established cadence. Slice 6 is the
first place a model enters the loop.

## Verification

- Storage/extraction: index a fixture TS project; assert one `extracted` IIR row
  per function node; reindex an unchanged file and assert no rewrite (file-hash
  incremental); edit one function and assert only its row updates.
- Rule merging: load a plugin declaring a rule pack; assert a repo-wide verify
  reflects the merged rule (e.g. a downgraded/added severity).
- Host functions: a test plugin calls `ce.iir_verify` and receives a Report.
- Surfaces: `ce iir` (built), an MCP `iir.verify` tool call, and an
  `/api/iir/verify` request all produce the same report for the same input.
- End-to-end: NL intent → shaped IIR → generated source → verify passes (the
  repair loop converges).
