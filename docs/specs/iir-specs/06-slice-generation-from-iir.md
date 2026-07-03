# Slice 6: Code Generation from IIR

## Goal

Generate source code from IIR using constrained emitters.

This should only begin after verification works.

## Principle

The LLM should not free-write large blocks of code when IIR is precise enough.

The model may help shape IIR, choose patterns, or resolve ambiguity. The emitter should generate code from structured intent.

## In scope

- FunctionIntent to TypeScript function emitter
- Deterministic formatting
- Basic branch generation
- Basic ResultType failure strategy
- Defensive input validation hooks
- Re-extraction after generation
- Compare generated code against intended IIR

## Out of scope

- arbitrary feature generation
- multi-file code generation
- framework-specific generation
- natural language to IIR
- model-based code writing

## Acceptance criteria

- Given a valid FunctionIntent, the emitter creates TypeScript source.
- Generated source parses successfully.
- Generated source can be re-extracted to IIR.
- Extracted IIR matches intended IIR for the supported subset.
- Generated code respects active rule packs.
