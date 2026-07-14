# Context Engine — Spec 19: Semantic Plan

## Implementation spec — a versioned contract between intent and generation

Status: proposed. Depends on Spec 2, the existing IIR model, and
[`north-star.md`](../../north-star.md). This is the first post-IIR foundation.

## Goal

Introduce a `SemanticPlan`: a versioned, inspectable representation of a
semantic unit after its intent has been resolved against project knowledge and
policy, but before source generation. It lowers generation uncertainty without
pretending that all facts are proven.

`FunctionIntent` remains the compact function-contract node. `SemanticPlan`
adds resolved references, obligations, decisions, evidence, and lifecycle state;
it must not become an AST or a bag of arbitrary source snippets.

## Model

The canonical model belongs in a new `internal/semantic/plan` package. It may
import `internal/core` and `internal/iir`; `core` must not import it. The wire
format is canonical JSON with an explicit `schemaVersion`.

```text
SemanticPlan
  id, projectId, schemaVersion, revision
  unit: SemanticUnit
  intent: FunctionIntent or a future typed intent node
  bindings: []SymbolBinding
  claims: []SemanticClaim
  obligations: []Obligation
  decisions: []Decision
  openQuestions: []OpenQuestion
  passRecords: []PassRecord
  lifecycle: declared | resolving | resolved | generated | observed | verified
  provenance: []Evidence
```

`SemanticUnit` identifies either an existing graph node (`NodeID`) or a
provisional unit with a stable requested canonical identifier. It records scope,
target language, and source-location links when they exist.

Every claim, binding, obligation, decision, and question has a stable local ID.
Each records a status and evidence:

- `observed` — mechanically lifted from source or graph facts.
- `declared` — supplied or explicitly approved by a user.
- `inferred` — proposed by a model or heuristic.
- `resolved` — selected from candidates by a deterministic rule or approval.
- `unknown` — required information is absent or ambiguous.

Evidence contains source type, graph/node references where applicable, producer
(host pass, plugin, model, or user), timestamp, confidence, and a short
explanation. Confidence does not upgrade an inferred claim to observed.

## Validation and compatibility

Provide deterministic parse, validation, canonical marshaling, and schema
upgrade functions. Validation requires stable IDs, valid references, non-empty
provenance for non-declared claims, acyclic decision dependencies, and no
`resolved` plan with blocking open questions. Unknown fields must be rejected in
v1 so plugin or client drift is visible.

`FunctionIntent` JSON remains compatible. A plan embeds it rather than changing
existing IIR storage or CLI inputs in this slice.

## Lifecycle

A plan is immutable by revision. A transformation creates a new revision with a
parent reference and adds a `PassRecord`; it never mutates or rewrites the
history of a previous revision. Only a plan with no blocking open questions may
enter `resolved`. Generation creates an artifact linked to that exact revision;
observed IIR and verification results link back to it.

## Non-goals

- Full control/data-flow IR or whole-program semantics.
- Source generation, storage migration, or public API endpoints.
- Replacing `core.IR`, which remains the query/cognitive-loop representation.

## Acceptance criteria

- Unit tests validate good and invalid plans, canonical JSON, and stable IDs.
- Tests prove provenance cannot be dropped by a plan revision.
- A plan can represent an existing function and a new requested function.
- `internal/core` retains zero internal imports.
- The package has no dependency on storage, runner, or plugin runtime.
