# Slice 14: Normalized `when` Expressions (structured behavior conditions)

Status: proposed. A follow-up to the extraction work (slices 4, 13) and the
comparison work (slice 3). Verification-first, additive.

## Motivation

Today a `BehaviorClause` carries its condition as a **verbatim source string**:

```go
// internal/iir/model.go
type BehaviorClause struct {
    When string `json:"when"` // e.g. "amount.cents < campaign.minimumDonation.cents"
    Then string `json:"then"`
}
```

`extractBehavior` copies the condition's source text directly
(`conditionText(...)`, `internal/iir/extract.go`), so `when` is really target
syntax wearing an IIR hat — the representation is barely "intermediate."

This leaks in two places that matter for our north star (verify):

1. **Comparison can't see content.** `compareBehavior`
   (`internal/iir/compare.go`) is currently **count-based only** — it compares
   `len(intended.Behavior)` against `len(extracted.Behavior)` and never inspects
   the `When` text. It cannot tell `a < b` from `a > b`, and it cannot tell
   `a < b` from a whitespace or commutativity variant of itself.
2. **Rules can't reason about structure.** A rule like "never compare against
   `null` with `==`" has to be a regex over a string instead of matching an
   operator node.

A structured, normalized form of the condition fixes both — and it is still
**100% deterministic to produce** (a second walk of the same tree-sitter AST we
already parse in slice 13), so it fits the "deterministic extraction, model only
at the NL→IIR hop" architecture with no new model dependency.

## Scope discipline (what this slice deliberately is NOT)

This is **not** a universal cross-language reasoning format, and it does **not**
try to make code generation deterministic across languages.

Structuring the *operator* is the easy 20%. The operands
(`amount.cents`, referencing a `Money` type) are still target-language
member-access chains, and resolving them into typed symbols is a compiler
front-end — name resolution + a type model over the graph. That is explicitly
out of scope. We normalize the expression *shape*; operands stay as opaque
canonical path strings. This keeps the slice small and honest: the payoff is
**robust same-language comparison and structural rules**, not language-agnostic
codegen.

## Principle

- **Additive, never lossy.** Keep `when` (the raw string) as ground truth and
  human-readable text. Add an *optional* structured field beside it. When the
  bounded grammar can't represent a condition, `whenExpr` is absent and
  everything falls back to today's string behavior. We never drop fidelity.
- **Deterministic.** No model. Same tree, second walk.
- **Bounded grammar first.** Cover the common majority of real conditions;
  report (not invent) anything outside it.

## Design

### Model (`internal/iir/model.go`)

Add an optional structured field. `omitempty` so existing IIR and JSON stay
byte-compatible when it's absent.

```go
type BehaviorClause struct {
    When     string `json:"when" yaml:"when"`
    Then     string `json:"then" yaml:"then"`
    WhenExpr *Expr  `json:"whenExpr,omitempty" yaml:"whenExpr,omitempty"`
}
```

`Expr` is a small uniform node — **not** a binary-only `{operator,left,right}`,
which can't hold unary ops, n-ary logic (`a && b && c`), or call/member chains:

```go
type Expr struct {
    Op   string  `json:"op"`             // "<", "<=", "==", "&&", "!", "call", "member", "lit", "path"
    Args []*Expr `json:"args,omitempty"` // operands, in source order
    Text string  `json:"text,omitempty"` // leaf payload: literal value or canonical path
}
```

Leaves:
- `{"op":"path","text":"amount.cents"}` — an opaque, un-resolved access path.
- `{"op":"lit","text":"0"}` / `{"op":"lit","text":"\"x\""}` — a literal.

The worked example becomes:

```json
{
  "when": "amount.cents < campaign.minimumDonation.cents",
  "whenExpr": {
    "op": "<",
    "args": [
      { "op": "path", "text": "amount.cents" },
      { "op": "path", "text": "campaign.minimumDonation.cents" }
    ]
  }
}
```

### Bounded grammar (v1)

Normalize only these tree-sitter node types; anything else → `whenExpr` absent:

- comparison / equality binary ops: `< <= > >= == != === !==`
- logical connectives: `&& || !`
- parenthesized expressions (unwrap)
- literals: number, string, boolean, `null`
- member-access chains and identifiers → a single `path` leaf (dotted text)
- **defer to v2:** function calls (`call`), ternaries, arithmetic, optional
  chaining, `await`, `in`/`instanceof`, template strings, destructuring

Normalization rules kept minimal and total:
- unwrap parentheses
- canonicalize member access to a dotted path string (no reordering of operands
  — commutative canonicalization is a v2 option, not v1)

### Extraction (`internal/iir/extract.go`)

`extractBehavior` already has the condition node in hand. Add
`normalizeCondition(node, src) *Expr` returning `nil` when the node falls
outside the bounded grammar. Populate `WhenExpr` alongside the existing `When`
string. No change to the `Then` path this slice.

### Comparison (`internal/iir/compare.go`)

Upgrade `compareBehavior` from count-only to content-aware **only when both
sides carry `WhenExpr`**:

- both structured → structural `Expr` equality (order-sensitive v1). A genuine
  content mismatch (`<` vs `>`) becomes a real `MismatchBehavior` with the two
  trees in Expected/Actual, instead of passing silently because the counts
  matched.
- either side missing `WhenExpr` → **fall back to today's count-based behavior
  exactly.** No regression for conditions outside the grammar, or for older
  stored IIR.

This is the concrete verify win: comparison stops being blind to whether the
condition is the same condition.

### Rules (`internal/iir/rules.go`) — optional, thin

Expose enough for a structural predicate (e.g. a rule that flags `==`/`!=`
against a `null` literal leaf). Keep it to one demonstrating rule; the mechanism
is the point, not a rule library.

### Generation (`internal/iir/generate.go`)

Unchanged behavior. `generate` still emits the `// when:` comment from the raw
`When` string; it does **not** render from `WhenExpr`. Rendering structured
expressions back to source (especially cross-language) is out of scope — see
"Scope discipline."

## In scope

- `Expr` type + optional `WhenExpr` field (additive, `omitempty`).
- `normalizeCondition` over the bounded v1 grammar; `nil` outside it.
- Content-aware `compareBehavior` when both sides are structured; fall back
  otherwise.
- One structural rule demonstrating the new predicate surface.
- Extractor + comparator tests, including grounded fixtures.

## Out of scope

- Operand resolution (names → typed symbols); any type model. Operands stay
  opaque path strings.
- Cross-language / deterministic generation from `WhenExpr`.
- Normalizing `Then` (it is a statement/effect with control flow — a separate,
  messier problem; this slice is `when`-only).
- v2 grammar (calls, ternaries, arithmetic, optional chaining, commutative
  canonicalization).
- Storing/migrating `whenExpr` as its own column — it rides inside the existing
  `iir` JSON blob, so no schema migration.

## Acceptance criteria

- A `when` condition within the bounded grammar extracts to a `WhenExpr` tree;
  the raw `when` string is unchanged and still present.
- A condition outside the grammar yields no `WhenExpr`; the clause is otherwise
  identical to today.
- `whenExpr` is absent from serialized IIR when empty (byte-compatible with
  existing stored/emitted IIR).
- Verify reports a real behavior mismatch when two structured conditions differ
  in content (e.g. `<` vs `>`) even though the clause counts match — the case
  today's count-only comparison passes silently.
- When either side lacks `WhenExpr`, comparison is identical to current
  count-based behavior (no regression).
- Extraction is deterministic.

## Testing

Deterministic, no model:

- `normalizeCondition` unit tests per grammar node type + one out-of-grammar
  input returning `nil`.
- Round-trip on the existing `/tmp/ce-iir` TS fixture (`validateDonationAmount`)
  or an equivalent committed fixture: assert the produced `WhenExpr`.
- `compareBehavior`: (a) both structured + equal → match; (b) both structured +
  differing operator → `MismatchBehavior`; (c) one side unstructured → falls
  back to count-based path.
- JSON: intent with `whenExpr` absent serializes byte-identically to pre-slice
  output.
