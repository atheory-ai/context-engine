# Open Questions

## IIR model

- How broad should the first core IIR vocabulary be?
- Should FunctionIntent be the first-class MVP node or should it be a specialization of BehaviorIntent?
- How much rationale should IIR carry in the first version?
- Should decisions be separate nodes or metadata on intent nodes?

## Extraction

- Should extraction require TypeScript type-checker support or start with AST-only parsing?
- How conservative should side-effect detection be?
- How should unsupported or unknown semantics be represented?

## Rules

- Should rules be declarative only in MVP?
- Do rules need autofix guidance immediately?
- How are rule packs configured per project?

## Comparison

- What counts as semantic equivalence?
- Should comparisons be strict by default?
- How are acceptable differences configured?

## Generation

- Should emitters produce AST first or source text directly?
- Should formatters be mandatory after generation?
- How should generation handle unsupported IIR?

## Harness

- Where does the user approve decisions?
- Which stages are allowed to call models?
- Which stages must be deterministic?
