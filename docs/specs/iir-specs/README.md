# Context Engine IIR Project Specs

This folder contains the lightweight spec files for adding IIR to the existing Context Engine project.

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

Slices 1–7 and the Phase 6 repair loop are implemented and merged. The standalone
IIR loop (verify / generate / gen-tests / repair) ships behind `ce iir`.

Next is engine integration — see `11-engine-integration.md` (RFC): extract IIR at
index time into the substrate, let plugins contribute rule "flavours" via merged
rule packs, expose `ce.iir_*` host functions, and add the intent→code endpoint on
every surface. The load-bearing decision there: **IIR is a host capability that
plugins call and extend, not a plugin itself.**

Merged follow-up: `14-slice-normalized-when-expr.md` — behavior conditions carry
an optional structured `whenExpr` (deterministic AST walk, additive, no model)
so verify compares condition *content*, not just clause counts. First structural
IL primitive.

North-star reframing: `15-universal-il-and-conformance.md` (RFC) — recast IIR as
a **universal IL**: valid code lifts totally to IL (mechanical), an LLM renders
IL back to code in any language, and generation is verified by lifting the
result and comparing at the IL level (no mechanical backends). A separate,
plugin-contributed **conformance layer** (rules + classifiers over the IL,
authored once, applied across languages) enforces "how we write it here" —
mechanically replacing the decaying prose-rules files people hand their LLMs
today. Sequenced single-language-conformance first; cross-language translation
falls out of the same loop later.
