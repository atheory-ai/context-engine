# Agent Kickoff Instructions

You are working in the existing Context Engine repository.

Implement the IIR feature as a set of narrow vertical slices.

Do not attempt to build the full system at once.

## First task

Implement Slice 1 only:

```bash
context-engine iir verify <intent-file> <source-file> --json
```

## Read first

1. `00-overview.md`
2. `01-slice-verify-function-intent.md`
3. `02-slice-rule-engine.md`
4. `03-slice-comparison-and-repair-targets.md`
5. `04-slice-iir-extraction.md`

## Implementation rules

- Prefer deterministic code.
- Do not call remote models.
- Do not build UI.
- Do not add persistent storage unless the existing project requires it.
- Fit the implementation into the existing Context Engine architecture.
- Reuse existing AST and semantic analyzer infrastructure where available.
- Keep model-facing stages as interfaces only.
- Add tests with every slice.

## Definition of done for Slice 1

The following command works:

```bash
context-engine iir verify examples/validateDonationAmount.iir.yaml examples/function-source-sample.ts --json
```

It outputs a verification report and exits non-zero when verification fails.
