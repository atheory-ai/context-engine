# Context Engine — Spec 28: Implementation-Recipe Lowering

## Implementation spec — convert resolved semantics into renderer constraints

Status: implemented (foundation, 2026-07-14). Depends on Specs 19–21 and 26–27.

Current implementation: `internal/semantic/recipe` provides deterministic,
canonical lowering for resolved plans, compact traceable recipe records, an
explicit TypeScript capability profile, and a deterministic TypeScript renderer
used as a test oracle. Model-backed rendering, recipe storage, and source-lift
links are deferred to their respective later slices.

## Goal

Introduce a deterministic lowering pass from a fully resolved `SemanticPlan` to
an `ImplementationRecipe`: the small, target-language-aware contract consumed
by a source renderer. This is the most important boundary for reducing model
entropy before code generation.

The recipe says what source must realize; it does not prescribe formatting,
helper names, or every statement.

## Recipe model

Create `internal/semantic/recipe`, importing plan types but not the model
provider or plugin runtime. The recipe is canonical JSON and references its
source plan revision.

```text
ImplementationRecipe
  id, schemaVersion, planRevisionId, targetLanguage
  target: new or existing source unit
  imports: resolved symbol references and import forms
  signature: function/API contract
  steps: ordered semantic operations and required direct calls
  effects: required and forbidden effects
  failures: required propagation, wrapping, or result strategy
  constraints: policy obligations and forbidden constructs
  rendererProfile: language and project realization guidance
  evidenceRefs, unresolvedQuestions
```

Every recipe item links to a plan claim, obligation, binding, or decision. A
recipe cannot be produced while mandatory questions remain open, or when it
would require a renderer to choose between incompatible policy obligations.

## Lowering rules

- Preserve the semantic operation order only where it is load-bearing, such as
  authorization before an external effect or persistence before event emission.
- Select imports and boundary calls from resolved bindings; never ask the model
  to search the repository for them.
- Lower policy obligations into positive requirements and negative constraints.
- Include a target-language semantic profile only for facts the target plugin
  declares; unsupported lowering creates an explicit diagnostic.
- Keep renderer input compact. Evidence is referenceable for explanation, not
  indiscriminately copied into the prompt.

## Renderer interface

Define a `Renderer` interface with `Supports(recipe)` and `Render(ctx, recipe)`.
The deterministic TypeScript emitter implements it as a test oracle. The
LLM-backed renderer is separate, accepts only a recipe, returns source plus
generation metadata, and cannot silently alter the recipe.

## Acceptance criteria

- Equivalent resolved plan revisions produce byte-stable recipes.
- Every required repository call, audit effect, and failure policy is represented
  in the TypeScript recipe fixture.
- A plan with a blocking question cannot reach a renderer.
- Renderer input has no raw graph dump or hidden architectural decision.
- Generated source can be traced from each observed claim back to recipe and
  plan IDs.
