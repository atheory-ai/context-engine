# Slice 6 (integration): NL → IIR Shaping

Status: accepted. Final slice of the engine-integration RFC
(`11-engine-integration.md`). This is the one place a model enters the IIR loop.

## Goal

Turn a natural-language description into a validated `FunctionIntent`, so a user
(or agent) can go from prose to verified code:

```
NL description → [model] → candidate IIR → [deterministic] validate → generate → verify
```

Everything after the model hop is the deterministic pipeline built in slices
1–7 of the standalone loop. The model's only job is producing a candidate IIR;
`iir.ParseIntentJSON` and the generate/verify/repair machinery do the rest.

## Principle

The model shapes intent; it does not free-write code. Its output is **never
trusted raw** — it passes through the same deterministic validation
(`ParseIntentJSON`) any hand-authored IIR does. A malformed or invalid intent is
caught, fed back to the model, and retried, exactly as the Strategizer does for
its IR.

## What already exists (reused, not rebuilt)

- `llm.Router` implements `core.LLMProvider` — the injectable model interface.
- The **Strategizer** (`internal/agent/strategizer`) is the working NL→structured
  precedent: `Complete` → extract → validate → 2-attempt retry that re-prompts
  with the validation error. NL→IIR mirrors it.
- `iir.ParseIntentJSON` — deterministic validation of a JSON `FunctionIntent`.
- `iir.GenerateFunction` / `VerifySource` — the downstream pipeline.

## Design

### `internal/iir/shaper` (new)

A separate subpackage keeps `internal/iir` itself deterministic and model-free;
the shaper is the isolated model-facing piece (as the Strategizer is separate
from `core.IR`).

- `Shaper{ llm core.LLMProvider }`, `New(llm)`.
- `Shape(ctx, description string) (*iir.FunctionIntent, error)`:
  1. Build a `CompletionRequest` — a system prompt describing the
     `FunctionIntent` JSON schema, asking for a single fenced JSON block.
  2. `llm.Complete(...)`.
  3. Extract the JSON block; `iir.ParseIntentJSON` it.
  4. On parse/validation failure, re-prompt with the error appended and retry
     (2 attempts total), then give up with a clear error.
- Imports `core` (LLMProvider) + `iir` (model + ParseIntentJSON). No cycle —
  `internal/iir` does not import its subpackage.

### CLI: `ce iir shape "<description>" [--generate] [--verify]`

Mirrors the existing `ce iir` subcommands.

- base: shape → print the validated IIR JSON.
- `--generate`: shaped IIR → `GenerateFunction` → print source.
- `--verify`: run the round-trip and report — the full loop end to end.

Wiring: add `Engine.LLMProvider()` (the engine already builds the router in
`buildLLMRouter`); the command builds an engine as `ce query` does and passes
`engine.LLMProvider()` to the shaper.

## Out of scope

- Wiring the shaper into the agent cognitive loop (autonomous shape → generate →
  verify mid-conversation). A larger follow-up; this slice keeps the model
  introduction contained to an explicit CLI command.
- `shape` on MCP/API (a natural follow-up mirroring slice 5).
- Multi-function / whole-file shaping.

## Testing

Deterministic, no real model:

- A fake `core.LLMProvider` returning canned responses.
- happy path (valid JSON → correct intent); malformed-first-then-corrected
  (proves the retry + error feedback); give-up after 2 bad attempts.
- prompt assembly and JSON extraction are pure helpers, unit-tested directly.

## Result

With this slice the engine-integration RFC is complete: IIR is extracted at
index time, stored in the substrate, extended by plugins, callable on every
surface, and now **reachable from natural language** — closing the overview's
loop, "agent and user shape intent, the harness turns it into verified code."
