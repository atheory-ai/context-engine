# Context Engine IIR Project Specs

This folder preserves the historical IIR slice RFCs. For the current product
contract, read [the IIR capability guide](../../iir.md), the
[semantic-platform north star](../../../north-star.md), and the
[dependency-ordered roadmap](../next-steps.md) first.

Start with:

1. `00-overview.md`
2. `01-slice-verify-function-intent.md`
3. `08-agent-kickoff.md`

The first implementation target is:

```bash
ce iir verify <intent-file> <source-file> --json
```

The intended development model is slice-based:

```text
Slice 1: verification
Slice 2: rules
Slice 3: comparison
Slice 4: extraction
Slice 5: plugin surface
Slice 6: generation
Slice 7: tests
```

Do not start with generation. Prove verification first.

## Status

Slices 1–7 and the repair loop are implemented; the standalone loop ships under
`ce iir`. The old engine-integration RFC is materially superseded: index-time
source lift is now plugin-owned, the host validates/persists it, and the engine
is pure Go with WASM tree-sitter rather than CGO. See the current guides above
instead of treating historical present tense as an implementation claim.

Merged follow-up: `14-slice-normalized-when-expr.md` — behavior conditions carry
an optional structured `whenExpr` (deterministic AST walk, additive, no model)
so verify compares condition *content*, not just clause counts. First structural
IL primitive.

`15-universal-il-and-conformance.md` remains a north-star RFC only. The current
implementation is function-level and TypeScript rendering is the sole supported
generation target; universal total lifting and cross-language generation are
explicitly deferred.
