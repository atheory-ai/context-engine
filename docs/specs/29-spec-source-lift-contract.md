# Context Engine — Spec 29: Source Lift Contract and Parity

## Implementation spec — maintain a trustworthy source-to-observed-semantics frontend

Status: proposed. Hardens existing plugin-owned IIR lifting for the semantic
compiler pipeline.

## Goal

Treat source lift as a first-class compiler frontend. A language plugin must
turn parseable supported constructs into observed semantic claims with stable
identity, source evidence, and explicit unsupported/unknown behavior. The host
uses that observed form as the verification oracle, so lift quality is a
correctness concern rather than an optional extraction feature.

## Contract

Extend the plugin IIR output contract, without adding another runtime:

```text
LiftedSemanticUnit
  nodeId, language, schemaVersion
  observedIntent: FunctionIntent
  claims: optional observed semantic claims
  evidence: source spans and classifier basis
  coverage: modeled | partial | unsupported
```

The plugin continues to run under wazero/Extism and attaches output to the
structural node it produced. The host validates all JSON, remaps IDs,
canonicalizes the payload, and stores only valid records through the write
buffer.

`coverage` is mandatory for plan-aware lifting. It prevents the verifier from
mistaking a partial walk for complete semantic observation. Unmodeled constructs
need not block indexing; they must leave an explicit unsupported or unknown
claim when they affect a requested verification obligation.

## Canonicalization and parity

The host owns shared `FunctionIntent` and plan-claim canonicalization. Plugins
bind language syntax to that shared vocabulary. Equivalent fixtures in Go,
TypeScript, and Python should produce equivalent claims where the languages
express the same semantic construct; language-specific behavior remains marked
as such.

Maintain golden fixtures by language for signatures, behavior conditions and
consequences, effects/basis, failure modes/kinds, source spans, and unsupported
constructs. The corpus must compare canonical JSON rather than incidental
plugin formatting.

## Lifecycle and compatibility

Version the lift schema independently from plugin package versions. The host
rejects unsupported major schema versions with a useful warning and skips only
the affected semantic output, not structural indexing. A plugin-capability
matrix declares which semantic fields and coverage levels each language supports.

## Acceptance criteria

- Every lifted function intent retains plugin/node identity and source evidence.
- Partial or unsupported observations cannot satisfy a mandatory verification
  obligation without an explicit policy decision.
- Golden tests cover equivalent cross-language behavior plus language-specific
  unsupported cases.
- The standalone extractor and index-time path use the same plugin lift.
- No CGO or non-wazero/Extism runtime is introduced.
