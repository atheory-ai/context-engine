# Context Engine — Spec 26: Intent Shaping and Normalization

## Implementation spec — establish trustworthy semantic-plan input

Status: implemented (foundation, 2026-07-14). Depends on Spec 19 and extends
the existing `internal/iir/shaper`.

Current implementation: `internal/semantic/shaping` accepts declared intent or
natural-language input, delegates model work to the existing validated IIR
shaper, creates canonical plan input, records model field provenance, and emits
explicit open questions for provisional targets and caller-declared missing
bindings or failure strategy. Resolution and persistence remain later stages.

## Goal

Make natural-language and hand-authored intent first-class inputs to a
`SemanticPlan`. The system must isolate model uncertainty at this boundary,
canonicalize what it can deterministically, and preserve unanswered questions
instead of embedding guesses as settled semantics.

## Inputs and outputs

V1 accepts: a natural-language change description, an existing
`FunctionIntent`, or a structured CLI/API request. All inputs produce an
initial plan revision with `declared` or `inferred` provenance.

The shaping boundary produces a candidate intent plus:

- semantic-unit scope and target language;
- explicit operations, inputs, outputs, effects, failures, and invariants when
  supplied or confidently extracted from the request;
- `OpenQuestion`s for absent or ambiguous mandatory information;
- a field-level explanation and model provenance for inferred claims.

## Pipeline

```text
input -> parse -> model shape when needed -> deterministic validation
      -> canonical normalization -> question extraction -> initial SemanticPlan
```

The existing two-attempt validate-and-retry behavior may remain, but a model is
never asked to repair a missing business decision by inventing one. Invalid
model output is rejected; valid but incomplete output becomes an incomplete
plan rather than a complete-looking one.

## Normalization

Implement host-owned normalizers for identity, language tags, types, visibility,
effect/failure spelling, normalized expressions, and stable local IDs. Reuse the
existing IIR canonicalization rules. Normalization must be lossless with respect
to raw declared text: preserve the original user phrase as evidence whenever a
normalized claim is derived from it.

`unknown`, `inferred`, and `declared` are semantically distinct states. The
normalizer must not collapse them to a single empty value. It may add warnings
for a likely contradiction, but only later resolution or explicit approval can
make a binding resolved.

## Interface and security

Keep the model-facing implementation outside the deterministic plan package.
It depends on `core.LLMProvider`; deterministic validation remains available
without an LLM. Bound input/output size and model retries. Read-scoped sessions
return plans in memory only and do not persist an execution record.

## Acceptance criteria

- Equivalent YAML, JSON, and natural-language fixture inputs normalize to the
  same semantic claims where they express the same fact.
- A missing repository, error strategy, or target symbol becomes an open
  question, not an invented binding.
- Invalid model output cannot produce a plan revision.
- Every inferred claim has model provenance and source text evidence.
- Existing `ce iir shape` callers remain compatible during migration.
