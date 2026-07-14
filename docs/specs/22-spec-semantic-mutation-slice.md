# Context Engine — Spec 22: Semantic Mutation Vertical Slice

## Implementation spec — prove intent to verified implementation for one change type

Status: implemented (foundation, 2026-07-14). Depends on Specs 19–21 and
26–29. This is the first user-visible proof of the north-star architecture.

Current implementation: `internal/semantic/mutation` provides the read-only
workflow and reproducible modeled-lift fixtures, including targeted diagnostics
for absent audit effects and provider-failure behavior. `ce iir implement`
accepts a description or `--intent` file, prints every intermediate artifact,
and writes source only with both `--write` and `--out`. The existing standalone
plugin extractor cannot yet expose coverage metadata, so that CLI path is
correctly reported as conditional rather than accepted until language plugins
emit the v1 source-lift envelope.

## Goal

Deliver one narrow TypeScript workflow for adding or changing a domain mutation.
It must show that semantic resolution and policies reduce generation ambiguity,
and that generated source is accepted or repaired using observed semantics.

The supported scenario is intentionally constrained: an operation mutates a
domain entity through a repository, emits an audit/domain event, and handles a
provider failure according to the effective project policy.

## Workflow

```text
description or declared FunctionIntent
  -> SemanticPlan
  -> resolve target/service/repository/event bindings
  -> apply mutation, audit, boundary, and error policies
  -> user resolves blocking questions and approves proposed obligations
  -> implementation recipe
  -> LLM renderer (or deterministic baseline for fixtures)
  -> TypeScript source
  -> WASM plugin lift -> observed IIR
  -> fidelity + conformance report
  -> targeted repair proposal or accepted artifact
```

The CLI command is initially experimental: `ce iir implement <description>`.
Without `--write`, it prints the plan, recipe, source candidate, and report. A
source-file write requires an explicit path and explicit user authorization;
the default never changes a repository.

## Implementation recipe

The renderer consumes a compact projection of a resolved plan, not the whole
substrate. It contains selected symbol imports, function signature, required
calls/effects, failure behavior, policy obligations, and forbidden constructs.
It includes source/evidence links for diagnosis but excludes irrelevant graph
context. The deterministic TypeScript emitter remains a fixture/test oracle;
the LLM renderer is isolated behind an interface and is the only new model hop.

## UX and approval

Present unresolved questions before rendering. The user can select a candidate,
provide a binding, waive a non-mandatory policy with rationale, or abort. Show
the generated patch and semantic report together. A failed or conditional result
must explain the specific plan obligation or fidelity claim that needs repair.

## Acceptance criteria

- A fixture project completes the full flow without raw-source retrieval as the
  primary architectural input.
- The plan records selected bindings, effective policies, approvals, and the
  exact renderer input.
- A deliberately missing audit event and an unwrapped provider failure are
  detected with targeted repair guidance.
- The command makes no source or substrate writes in read-only sessions.
- Fixture generation is reproducible; model-backed tests use a mocked renderer.

## Non-goals

- General-purpose code editing, multi-file refactors, or cross-language output.
- Autonomous approval of an ambiguous binding or repository write.
