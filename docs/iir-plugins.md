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
- **rule packs** — durable, executable code expectations (see Slice 2)

Analyzers, additional IIR node types, code/test emitters, and renderers are
named in the spec as future contribution types; the current interfaces cover
the verification loop (extract → compare → rules).

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

type Plugin struct {
    ID          string
    Name        string
    Version     string
    Languages   []string
    Extractors  []Extractor
    Comparators []Comparator
    RulePacks   []PluginRulePack // each carries the owning PluginID
}
```

## The built-ins are plugins

The core does not special-case its own capabilities. The built-in TypeScript
function extractor and the built-in `FunctionIntent` comparator implement the
same interfaces a third-party plugin would, and the verification path uses
them through those interfaces:

- `BuiltinExtractor()` → the `Extractor` used by `VerifySource`
- `BuiltinComparator()` → the `Comparator` used by `Verify`
- `BuiltinPlugin()` → the manifest bundling both, plus the default rule pack
  associated with the `builtin` plugin id

This is the guarantee Slice 5 exists to establish: whatever a plugin can do,
the built-ins already do the same way.

## Registration and resolution

`Registry` holds plugins and resolves capabilities:

```go
reg := iir.DefaultRegistry()          // contains only the built-in plugin
reg.Register(myPlugin)                 // add more

ext, ok := reg.ExtractorFor(input)     // last-registered supporting extractor
cmp, ok := reg.ComparatorFor(a, b)     // last-registered supporting comparator
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
