# IIR Plugin Surface

This document describes the extension points for the Intermediate Intent
Representation (IIR). It is the contract referenced by Slice 5.

> **Status: interface-first.** The surface is defined and the built-in
> TypeScript capabilities implement it, but there is no dynamic runtime yet —
> no WASM, remote loading, sandboxing, or publishing. Plugins are registered
> in-process. A later slice can add dynamic loading behind these same
> interfaces without changing them.

## What a plugin can contribute

A plugin contributes one or more of:

- **extractors** — turn source into IIR nodes
- **comparators** — diff intended IIR against extracted IIR
- **emitters** — generate source from IIR (see Slice 6)
- **test emitters** — generate tests from IIR (see Slice 7)
- **rule packs** — durable, executable code expectations (see Slice 2)

Analyzers, additional IIR node types, and renderers are named in the spec as
future contribution types; the current interfaces cover the full loop —
extract → compare → rules, generate code, and generate tests.

## Calling IIR from a WASM plugin (`ce.iir_*` host functions)

Per the engine-integration RFC, IIR is a **host capability** — it runs in the
Go host, and WASM plugins call it through `ce.iir_*` host functions (registered
in `internal/plugins/runtime/host.go`, namespace `ce`). All arguments and
results are passed as pointers to UTF-8 strings across the Extism boundary:

| Host function | Args | Returns |
|---|---|---|
| `ce.iir_extract` | `language`, `source`, `target` | `FunctionIntent` JSON |
| `ce.iir_verify` | `intent` JSON, `source` | verification `Report` JSON |
| `ce.iir_generate` | `intent` JSON | TypeScript source |
| `ce.iir_gen_tests` | `intent` JSON | `TestArtifact` JSON |

These are pure computations over `internal/iir` — no substrate or config access,
so they are available during indexing and query time alike. Errors are returned
**in-band** as a JSON object `{"error": "..."}` rather than trapping the plugin;
a caller checks for the `error` key. Intent JSON is parsed with the JSON-native
`ParseIntentJSON` (tolerant of a marshaled `FunctionIntent`), so an extracted
intent round-trips straight back into verify/generate.

`ce.iir_extract` rejects any `language` other than `typescript` (empty defaults
to it). Plugin-supplied `source`/`intent` payloads are size-capped before
parsing, so a runaway plugin can't feed the host unbounded input.

## Calling IIR from a client (MCP tools + REST API)

The same IIR capability is exposed on the server surfaces, so an agent or app
can turn intent into verified code without a plugin:

| Capability | MCP tool | REST endpoint |
|---|---|---|
| verify source vs intent | `ce_iir_verify` | `POST /api/v1/iir/verify` |
| generate source from intent | `ce_iir_generate` | `POST /api/v1/iir/generate` |
| generate tests from intent | `ce_iir_gen_tests` | `POST /api/v1/iir/gen-tests` |

Request bodies carry the `intent` (a `FunctionIntent` object) and, for verify, a
`source` string. These handlers are engine-free — pure `internal/iir`
computation — so they run without an indexed project. The CLI equivalents are
`ce iir verify|generate|gen-tests`.

## Interfaces

The Go interfaces live in `internal/iir/plugin.go`. They mirror the
TypeScript contracts in `docs/specs/iir-specs/05-slice-plugin-surface.md`,
adapted to idiomatic Go — methods are synchronous and take a
`context.Context` rather than returning Promises.

```go
type Extractor interface {
    ID() string
    Supports(input ExtractionInput) bool
    Extract(ctx context.Context, input ExtractionInput) (ExtractionResult, error)
}

type Comparator interface {
    ID() string
    Supports(intended, extracted *FunctionIntent) bool
    Compare(intended, extracted *FunctionIntent) ComparisonResult
}

type Emitter interface {
    ID() string
    Supports(intent *FunctionIntent) bool
    Emit(intent *FunctionIntent) (string, error)
}

type TestEmitter interface {
    ID() string
    Supports(intent *FunctionIntent) bool
    EmitTests(intent *FunctionIntent) (TestArtifact, error)
}

type Plugin struct {
    ID          string
    Name        string
    Version     string
    Languages   []string
    Extractors   []Extractor
    Comparators  []Comparator
    Emitters     []Emitter
    TestEmitters []TestEmitter
    RulePacks    []PluginRulePack // each carries the owning PluginID
}
```

## The built-ins are plugins

The core does not special-case its own capabilities. The built-in TypeScript
function extractor and the built-in `FunctionIntent` comparator implement the
same interfaces a third-party plugin would, and the verification path uses
them through those interfaces:

- `BuiltinExtractor()` → the `Extractor` used by `VerifySource`
- `BuiltinComparator()` → the `Comparator` used by `Verify`
- `BuiltinEmitter()` → the `Emitter` behind `iir generate` / `GenerateFunction`
- `BuiltinTestEmitter()` → the `TestEmitter` behind `iir gen-tests` / `GenerateTests`
- `BuiltinPlugin()` → the manifest bundling all four, plus the default rule
  pack associated with the `builtin` plugin id

This is the guarantee Slice 5 exists to establish: whatever a plugin can do,
the built-ins already do the same way.

## Registration and resolution

`Registry` holds plugins and resolves capabilities:

```go
reg := iir.DefaultRegistry()          // contains only the built-in plugin
reg.Register(myPlugin)                 // add more

ext, ok := reg.ExtractorFor(input)     // last-registered supporting extractor
cmp, ok := reg.ComparatorFor(a, b)     // last-registered supporting comparator
em, ok := reg.EmitterFor(intent)       // last-registered supporting emitter
te, ok := reg.TestEmitterFor(intent)   // last-registered supporting test emitter
packs := reg.RulePacks()               // all packs, each tagged with its PluginID
```

Later registrations take precedence, so a plugin can override a built-in for
the same input (e.g. a framework-aware TypeScript extractor).

## Adding a plugin (future)

1. Implement `Extractor` and/or `Comparator`, and/or build a `RulePack`.
2. Assemble a `Plugin` manifest with a unique `ID`.
3. `reg.Register(plugin)`.

When dynamic loading arrives, only step 3 changes — the interfaces above stay
the contract.
