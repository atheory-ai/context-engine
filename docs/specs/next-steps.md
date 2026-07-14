# Context Engine — Semantic Platform Next Steps

## Implementation roadmap from the IIR foundation to the north star

Status: proposed. Companion to [north-star.md](../../north-star.md). This is an
execution index, not a replacement for the IIR RFCs in `iir-specs/`.

## Current position

Context Engine has a working function-level IIR loop: language plugins lift
source into observed `FunctionIntent`s; the host validates and stores them;
declared or model-shaped intent can be compared to the observed form; and rule
packs enforce conformance. The CLI, MCP, and REST surfaces expose verification,
generation, test generation, and repair.

The missing layer is a resolved semantic plan: an inspectable representation
that binds intent to the project's symbols and policies before the model emits
source. The specs below close that gap in dependency order.

## Ordered work

| Order | Spec | Outcome |
| --- | --- | --- |
| 1 | [19 — Semantic plan](19-spec-semantic-plan.md) | A versioned, evidence-bearing contract between intent and generation. |
| 2 | [26 — Intent shaping and normalization](26-spec-intent-shaping-and-normalization.md) | Prose and declared intent become validated, canonical plan input. |
| 3 | [20 — Resolution pass](20-spec-semantic-resolution.md) | Intent is bound to real graph symbols and unknowns are explicit. |
| 4 | [27 — Semantic enrichment](27-spec-semantic-enrichment.md) | Relevant semantic facts are derived with explicit evidence and uncertainty. |
| 5 | [21 — Policy-pass pipeline](21-spec-semantic-policy-passes.md) | Deterministic policies add and check traceable obligations. |
| 6 | [28 — Recipe lowering](28-spec-implementation-recipe-lowering.md) | A resolved plan becomes a compact, target-aware renderer contract. |
| 7 | [29 — Source lift contract](29-spec-source-lift-contract.md) | Plugin lifts remain a trustworthy, parity-tested verification frontend. |
| 8 | [22 — Vertical slice](22-spec-semantic-mutation-slice.md) | One user-visible TypeScript mutation flow proves the architecture end to end. |
| 9 | [23 — Verification](23-spec-semantic-verification.md) | Fidelity reports are evidence-backed and distinguish unknown from verified. |
| 10 | [30 — Repair planning](30-spec-semantic-repair-planning.md) | Failures become minimal semantic changes before a renderer retries. |
| 11 | [31 — Test-plan lowering](31-spec-test-plan-lowering.md) | Tests and coverage derive from the plan and its verification gaps. |
| 12 | [24 — Semantic build graph](24-spec-semantic-build-graph.md) | Plans, versions, provenance, runs, and diffs become durable graph artifacts. |
| 13 | [25 — Documentation and delivery](25-spec-semantic-platform-documentation.md) | Specs, public docs, SDK contracts, and release gates describe one current architecture. |

## Sequencing rules

- Do not begin LLM-backed implementation rendering before the semantic-plan
  contract and the TypeScript vertical slice have acceptance tests.
- Keep `FunctionIntent` compatible and useful as the compact function contract.
  A `SemanticPlan` is a separate resolved layer, not an unbounded rewrite of it.
- A plan may be generated or enriched only when its provenance and unresolved
  decisions are retained. Inferred facts must never be silently presented as
  observed facts.
- All substrate writes continue through `core.SubstrateWriter` and the write
  buffer. Read-scoped sessions do not persist plans, runs, or artifacts.
- Plugins remain wazero/Extism WASM plugins. Initial policy contributions are
  declarative and host-evaluated; no second plugin runtime is introduced.

## Decision gates

The team must approve the following before advancing beyond the named step:

1. **After Spec 19:** the schema, lifecycle, claim-evidence model, and semantic
   equivalence boundary are stable enough to version.
2. **After Specs 26–28:** shaping, enrichment, policy, and recipe lowering each
   have clear ownership; the renderer receives no unresolved mandatory decision.
3. **After Spec 22:** the slice demonstrates fewer unresolved implementation
   decisions at generation time and a useful semantic repair report.
4. **After Spec 23:** verification never treats lack of evidence as a passing
   fidelity result.

## Explicitly deferred

- A universal, total IL for all parseable constructs.
- Cross-language LLM generation and translation.
- Arbitrary executable plugin transformation code.
- Whole-program proof of semantic equivalence.
- Automatic source writes without an explicit user-authorized action.

Those are possible later consequences of this architecture, not prerequisites
for proving the first semantic-development workflow.
