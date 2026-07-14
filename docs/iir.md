# Intent Representation (IIR)

This is the current capability guide. Start with the [semantic-platform north
star](../north-star.md), then the [capability matrix](./iir-capabilities.md) and
the [executable roadmap](./specs/next-steps.md). Historical design RFCs live in
[`docs/specs/iir-specs/`](./specs/iir-specs/) and are not the current runtime
contract.

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

**Indexed source lift is plugin-owned.** During `ce index`, a language plugin
may attach observed `FunctionIntent` records to its symbol nodes. The host
validates the payload and stores it in `iir`, keyed by `(project_id, node_id,
kind)`. The bundled Go, Python, and TypeScript plugins ship this legacy
intent-only lift; it is conservatively treated as **partial** semantic evidence
until a plugin emits the v1 claims, evidence, and coverage fields. See the
[capability matrix](./iir-capabilities.md) for exact current support.

The engine is pure Go: tree-sitter parsing runs as WASM on wazero, and plugins
run through wazero plus Extism. CGO is not supported.

## CLI

```bash
ce iir verify <intent-file> <source-file>   # does the source match declared intent?
ce iir generate <intent-file> [--verify]    # emit TypeScript from intent (round-trips)
ce iir gen-tests <intent-file> [--coverage] # emit tests (Vitest/Jest) from intent
ce iir repair <intent-file> <source-file>   # iteratively repair source to match intent
ce iir shape "<description>" [--generate]   # natural language → IIR (uses the model)
ce iir implement <description> [--write]    # semantic-plan mutation slice (TypeScript)
```

Intent files are JSON/YAML `FunctionIntent`s. `generate` and `gen-tests` are
deterministic TypeScript emitters. `verify` and `repair` depend on an installed
language plugin; `shape` is the model-backed hop and always revalidates its
output before use. `implement` is an experimental, explicit-write TypeScript
mutation slice; without `--write` it is read-only.

## Conformance rules

Rules live in a rule pack. The engine ships defaults, a project can add
`iir.rules.yaml`, and **plugins contribute rule packs via their manifest**
(`iirRules`) which the host merges over the defaults. A rule can match on
top-level intent fields (`when` visibility/failure modes) or on the *structure*
of a normalized condition (`require.forbidConditionShape`) — e.g. forbid
`== null` across every language at once.

## Calling IIR programmatically

The standalone IIR helpers are host functions that WASM plugins can call.
Indexed lifting is separately plugin-owned: plugins emit observed IIR and the
host validates, canonicalizes, and persists it.

- **Host functions** for WASM plugins: `ce.iir_extract`, `ce.iir_verify`,
  `ce.iir_generate`, `ce.iir_gen_tests`.
- **MCP tools** and **REST** endpoints: `ce_iir_verify`, `ce_iir_generate`, and
  `ce_iir_gen_tests`; `POST /api/v1/iir/{verify,generate,gen-tests}`.

See [IIR plugin contracts](./iir-plugins.md) and [plugin authoring](./plugin-authoring.md)
for the runtime/SDK boundary. The semantic-plan work is specified in
[`docs/specs/`](./specs/), while the historical RFC directory preserves the
earlier design record.
