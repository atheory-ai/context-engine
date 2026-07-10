# Intent Representation (IIR)

IIR — **Intermediate Intent Representation** — is a structured description of
what a piece of code is *intended* to do. It sits **above the AST** (more
semantic than syntax) and **below natural language** (more verifiable than
prose), and it lets Context Engine check that source code actually matches its
declared intent.

The guiding loop:

```text
declared intent  →  source code  →  extracted intent  →  compare  →  verification report
```

Agents are strongest when they convert one representation into another and check
that meaning is preserved. IIR gives code a representation that is more semantic
than an AST, more verifiable than prose, more compact than raw source, and more
durable than a prompt instruction — usable for verification, code generation,
and test generation alike.

## The model: `FunctionIntent`

A function's intent is captured as a `FunctionIntent` (see
`internal/iir/model.go`):

| Field | Meaning |
| --- | --- |
| `name`, `language`, `visibility` | identity and public contract |
| `inputs`, `returns` | typed parameters and return type (`explicit` marks a declared return) |
| `behavior[]` | branch clauses: a `when` condition → `then` outcome, plus a normalized `whenExpr` |
| `sideEffects[]` | observable effects (e.g. `analytics.track`), de-duplicated and sorted |
| `failureModes[]` | thrown / raised failure identifiers |
| `constraints[]` | additional declared expectations |

`whenExpr` is the key normalization: a condition like `x is None` (Python),
`x == nil` (Go), or `x == null` (TS) all normalize to the same structured
expression (`{op:"==", args:[path, lit]}`), so one rule can reason across
languages. This is the seed of a **universal intermediate language (IL)**: intent
is expressed once, in a language-neutral shape, and opinions written against it
apply everywhere (see `docs/specs/iir-specs/15-universal-il-and-conformance.md`).

## How intent is produced

Two orthogonal gates operate over IIR:

- **Fidelity** — does the code match its declared intent? Source is lifted to an
  extracted `FunctionIntent` and compared against the declared one.
- **Conformance** — does the intent satisfy opinionated rules (naming, explicit
  returns, forbidden condition shapes, …)? Rules target an intent *kind*, not a
  language, so a rule written once applies across all languages.

**Extraction is plugin-owned.** During `ce index`, each language plugin lifts its
functions to IIR as part of the same tree-sitter pass it uses for structural
extraction, and attaches each `FunctionIntent` to the symbol node it came from.
The host stores it in the `iir` table keyed by `(project_id, node_id, kind)`.
Go, TypeScript, and Python are at parity (contract + behavior + whenExpr +
effects + failure modes). Enable index-time extraction with `iir.enabled: true`
in `ce.yaml`.

## CLI

```bash
ce iir verify <intent-file> <source-file>   # does the source match declared intent?
ce iir generate <intent-file> [--verify]    # emit TypeScript from intent (round-trips)
ce iir gen-tests <intent-file> [--coverage] # emit tests (Vitest/Jest) from intent
ce iir repair <intent-file> <source-file>   # iteratively repair source to match intent
ce iir shape "<description>" [--generate]   # natural language → IIR (uses the model)
```

Intent files are JSON/YAML `FunctionIntent`s. `verify`/`generate`/`gen-tests`
are deterministic; only `shape` calls a model, and its output is always run back
through the deterministic parser before use.

## Conformance rules

Rules live in a rule pack. The engine ships defaults, a project can add
`iir.rules.yaml`, and **plugins contribute rule packs via their manifest**
(`iirRules`) which the host merges over the defaults. A rule can match on
top-level intent fields (`when` visibility/failure modes) or on the *structure*
of a normalized condition (`require.forbidConditionShape`) — e.g. forbid
`== null` across every language at once.

## Calling IIR programmatically

IIR is a **host capability**, not a plugin — plugins *call* it and *extend* it,
they don't reimplement it:

- **Host functions** for WASM plugins: `ce.iir_extract`, `ce.iir_verify`,
  `ce.iir_generate`, `ce.iir_gen_tests`.
- **MCP tools** and **REST** endpoints: `ce_iir_verify` / `POST
  /api/v1/iir/{verify,generate,gen-tests}`.

See [docs/iir-plugins.md](./iir-plugins.md) for the plugin surface, and the
[`docs/specs/iir-specs/`](./specs/iir-specs/) directory for the authoritative
specs (start with `00-overview.md`, then `15-universal-il-and-conformance.md`
for the universal-IL direction).
