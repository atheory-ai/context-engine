# Context Engine — Spec 20: Semantic Resolution Pass

## Implementation spec — bind intent to project knowledge before generation

Status: proposed. Depends on Spec 19 and the indexed substrate.

## Goal

Build the first deterministic compiler pass: resolve a semantic plan's symbolic
requirements against Context Engine's graph. The result must state what was
bound, why it was chosen, competing candidates, and what remains unknown.

This turns context into resolution. It is not an LLM retrieval prompt and it
must not quietly guess an architectural decision.

## Scope

V1 resolves only the information needed for the mutation vertical slice:

- requested or existing function/service symbols;
- interfaces and implementations;
- repositories, domain services, event/audit publishers, and provider clients;
- adjacent callers and test conventions;
- project and plugin rule-pack applicability.

Resolution receives a `SemanticPlan`, a read-only substrate view, and a project
configuration. It returns a new plan revision. It does not write source or
modify graph structure.

## Package and interface design

Implement `internal/semantic/resolve`, importing `core` and
`internal/semantic/plan`. Use a narrow interface declared in `core` only if it
is generally reusable; otherwise keep the resolver's reader interface local so
the dependency floor remains clean.

The resolver is composed of named deterministic resolvers. Each resolver emits:

```text
ResolutionResult
  requirementId
  candidates: [{ nodeId, canonicalId, score, evidence }]
  selected: candidate | nil
  outcome: resolved | ambiguous | missing | incompatible
  explanation
```

Selection requires a deterministic threshold and tie-breaker. An ambiguous or
missing requirement creates a blocking `OpenQuestion`; it must not be replaced
by an inferred binding. A user or later model-assisted decision may select from
the preserved candidate set and records that provenance in the next revision.

## Resolution rules

- Resolve by canonical IDs and graph relationships first; text labels are only
  a fallback and always carry lower-confidence evidence.
- Include only graph evidence that is reachable and relevant to the unit.
- Do not synthesize new substrate nodes during a read pass.
- Respect token/session scope: read-scoped callers receive the plan in memory
  only and never create an execution or substrate record.
- Record the effective rule-pack identity so later verification can reproduce
  the same policy context.

## Observability

Expose a structured resolution report for CLI, MCP, and API consumers. It
includes selected bindings, candidates, unresolved questions, and a compact
explanation—not raw hidden ranking state. Emit warnings for ambiguous and
missing requirements through the standard application channels.

## Acceptance criteria

- Fixture projects resolve a unique repository/service/event binding.
- Ambiguous matches remain open questions with all candidates retained.
- Repeated runs over unchanged graph data produce byte-stable output.
- A resolver never writes directly to SQLite or the substrate.
- Tests cover canonical match, relationship match, fallback match, missing, and
  ambiguity behavior.
