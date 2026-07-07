# RFC 15: IIR as a Universal IL — generate-via-LLM, verify-via-lift, and a plugin-contributed conformance layer

Status: proposed (design RFC, from a design session). This is a north-star
reframing of what IIR is *for*. It does not obsolete the shipped IIR loop
(slices 1–14); it recasts that work as the substrate for a larger goal and
defines the architecture we grow toward. Concrete build slices are deferred to
follow-up specs; this fixes the shape and the load-bearing decisions.

## Goal

Make IIR a **universal intermediate language (IL)** for code: a language-agnostic
representation of programming *structure and intent* that

1. any language can be **lifted** into from its AST (Code → AST → IL), and
2. can be rendered back to code in any language **by an LLM** (IL → code), with
3. an **opinion layer** — plugin-contributed rules and classifiers over the IL —
   that both *guides* the LLM and *checks* its output.

The IL is the shared middle; the LLM is the renderer; verification and
conformance are what make a lossy, interpretive translation trustworthy.

## Reframing in one picture

```
                 lift (per-language, mechanical, TOTAL)
   Code_A ─ AST ─────────────────────────────────►  IL
                                                     │
                    ┌────────────────────────────────┤
                    │ guide                           │ check
                    ▼                                 ▼
              [ LLM renderer ] ── code_B ─ AST ─ lift ─► IL′
                    ▲                                 │
                    └──────── repair on mismatch ◄────┘  (fidelity gate)
                                                     │
                                        rules + classifiers  (conformance gate)
```

There are **no mechanical backends.** The bottom half of the classic compiler
hourglass — N frontends + M backends — collapses to **N frontends + one LLM +
the frontends run again as the verification oracle.** You only ever build lift.

## Theses

### T1 — Validity is a total, mechanical gate; correctness is opinion layered on top

If code is valid it parses to an AST; if it parses, the language plugin **must**
lift it to IL. There is no representable-vs-unrepresentable judgment. Lift is
**total over parseable code** because an unmodeled construct degrades to a
*foreign node* carrying its AST subtree rather than failing (see T5). Validity
never involves taste.

"Good vs bad code" is a separate axis — like well-written prose, or "not how we
write it here." That is **conformance**, evaluated by rules and classifiers over
the IL (T6). The two are orthogonal gates:

- **Fidelity** (mechanical, objective): `generate → lift → compare-at-IL`.
  Catches "this code does not *mean* what the IL said."
- **Conformance** (opinionated, pluggable): rules + classifiers over the IL.
  Catches "this is faithful but not *how we do it here*."

A generation may be faithful-but-nonconformant or unfaithful; the gates are
reported and repaired separately, with different feedback.

### T2 — The generate/verify loop: LLM renders, lift verifies, compare-at-IL referees

IL → code is the LLM. Verification is `code → AST → IL′`, then compare `IL′` to
the original `IL`. The comparison is **IL ↔ IL, never code ↔ code**, which
sidesteps textual-equivalence-is-impossible: we compare at the level where we
*chose* what is load-bearing.

Because the verifier is deterministic and cheap, a fuzzy generator becomes
reliable via **best-of-N**: sample several renderings, accept the first that
round-trips. The generator–verifier gap, exploited productively.

### T3 — One design choice does three jobs

The IL's **abstraction boundary** simultaneously *is* (a) the constraint on the
LLM, (b) what fidelity verification checks, and (c) the equivalence relation on
generated code. Dimensions the IL abstracts away (idiom, formatting, naming —
unless promoted; see T6) cannot cause a false rejection, so the cook is free to
vary there. Dimensions the IL pins must round-trip. "Precise where the model is
unreliable, loose where it is competent" stops being a slogan and becomes a
mechanical property.

### T4 — Interpretation is expected; the IL is a recipe, not a transpiler

Cross-language rendering is interpretive, like human translation between natural
languages — each language expresses concepts its own way and adaptation is
required. The IL frames the LLM's hypothesis space until it "sees exactly what to
do"; it does not determine the output. This is why we choose an *underspecified*
IL over a canonical-semantics one (see Decisions): classical hard-interlingua MT
lost to neural translation, which reintroduced a *soft, learned* interlingua. The
LLM already has a latent interlingua for code; the IL supplies the explicit
constraints its latent competence cannot be trusted to honor consistently.

### T5 — Plugins own the bindings; the center owns the ontology

The plugin that ships a language's tree-sitter grammar also declares that
language's **lift** (AST→IL) and its **semantic profile** (how it realizes null,
truthiness, numeric behavior, error propagation, dispatch, iteration). The center
stays thin — LSP's model, MLIR's dialects, tree-sitter's own model — so the
hourglass does not collapse to N×M in maintenance.

**Load-bearing caveat:** for a concept lifted by plugin A to be rendered into
language B, both must bind to the *same* concept. So the center owns a **shared
vocabulary of concepts and semantic axes**; plugins *bind* to it and may
*extend* it, but the shared core is what makes cross-language possible at all.
The vocabulary is a **superset** — rich enough to hold a concept a target
language lacks, so lowering into a language without it is an explicit, visible
act of adaptation (handed to the LLM with the plugin's declared approximation
strategy), never a silent drop. Concept absent in target ≠ concept absent from
the IL. The foreign-node escape hatch is what keeps lift total when a *source*
construct outruns the vocabulary.

### T6 — Conformance: opinions authored once, in IL-space, applied across languages

Because opinions attach to the IL, not to any language's syntax, a cross-language
opinion ("errors propagate explicitly, never swallowed"; "no side effects in a
pure-typed function") is authored **once** and applies to every language whose
lift surfaces that dimension. Contrast eslint/ruff/golangci-lint: the same policy
re-expressed N times in N per-language tools that share no model. Here it is one
rule in IL-space, N languages.

The substrate already leans this way: `Rule` targets a `Kind` (e.g.
`FunctionIntent`), **not** a language — language-agnostic by default, with
language-specificity as the *narrowing*. Three scopes map onto machinery we have:

- **universal / all-IL** — packs targeting Kinds with no language predicate (the
  default shape).
- **language** — the language plugin's pack (`PluginManifest.IIRRules`).
- **team / project** — `DiscoverProjectRulePack`, layered on top.

Each opinion carries an **evaluator** — mechanical (a declarative predicate) or a
**classifier** (model-backed judgment for opinions that resist formalization).
Every rule is used **both directions from one definition**: compiled into a
constraint fragment that *guides* generation, and evaluated as a predicate that
*checks* extracted IL. Authored once, guides the cook and inspects the plate.

**This is the near-term killer app.** It mechanically replaces the thousands of
lines of `CLAUDE.md` / skills-file / `.cursorrules` prose people write hoping to
steer their LLMs — prose that competes for context, may go unattended, and gets
summarized/evicted and lost as a session grows. Here conventions are **data
attached to the codebase's semantic model, retrieved by relevance and applied at
the moment the relevant code is touched** — durable by construction because they
live in the engine, not the conversation. Relevance retrieval over an indexed
semantic graph is this product's core competency, which is *why the feature
belongs in Context Engine specifically.*

## Leveled IR (borrowing MLIR's dialect idea)

Do not build one IL; build a tower, and only the high levels must be truly
language-agnostic.

- **L0** — language AST (tree-sitter CST). Per-language; already consumed.
- **L1** — normalized structural IR: regularized expression/statement/control-flow
  trees. `whenExpr` (slice 14) lives here. Liftable from any AST; syntactically
  regular, not yet semantically universal.
- **L2** — semantic IR: control-/data-flow over typed operations carrying
  explicit semantic annotations (numeric/null/effect/error models). The
  genuinely language-neutral contract layer; the hard core.
- **L3** — intent IR: today's `FunctionIntent` (contracts, behaviors, effects).
  The LLM-facing, already-portable layer.

We are not starting over: `FunctionIntent` is an L3 node and `whenExpr` an
L1/L2 fragment. The work is deliberately *growing the middle* and building edges.

## Load-bearing decisions

1. **Underspecified L2, not canonical-semantics** (T4). Precision is expensive
   (registry, adapters); spend it only where the model drifts. Fidelity is an
   *observable* property (the round-trip) rather than a guaranteed one. Canonical
   semantics can be layered onto specific constructs later, driven by real
   fallback data.
2. **Cross-scope conformance conflict → most-specific-scope wins, plus severity
   as an escape valve.** Config/CSS-specificity cascade over
   universal → language → team. A universal opinion may be a `should`
   (warning; a language satisfies it its own way or not at all); a `must`
   (error) is non-negotiable. `MergeRulePacks` (override-by-id, later-registration
   precedence) is the seed; the scope cascade + severity semantics are the
   addition. **This is the one piece still carrying a genuinely open decision —
   pressure-test before building.**
3. **Classifiers must be actionable at IL granularity.** A model-backed opinion
   must emit an IL-anchored, localized explanation with a repair target (the
   `Mismatch.RepairTarget` contract), or it can *check* but not *guide* — and
   guiding is half the point. Prefer declarative rules where the opinion is
   crisply expressible; reserve classifiers for the irreducibly fuzzy tail.

## What already exists (reused, not rebuilt)

- **Lift** (Code→AST→IL): index-time extraction (`internal/indexer/iirpass.go`,
  `iir.ExtractAllFromNode`) for TS/Go/Py — **host-side Go today; migrating to
  plugins** (see "SDK enablement").
- **IL↔IL compare**: `internal/iir/compare.go` (now content-aware for behavior
  conditions via `whenExpr`, slice 14).
- **Repair loop**: `RepairLoop` (verify→propose→re-verify) with `RegenerateStage`
  — the seam where "regenerate via LLM, then lift+compare" plugs in.
- **NL→IL model hop**: `internal/iir/shaper` — the pattern the new **IL→code**
  LLM stage mirrors (prompt with IL + target-language profile → code; existing
  2-attempt validate-and-retry applies).
- **Plugin-contributed, merged rule packs**: `Rule`/`RulePack`,
  `DefaultRulePack`, `MergeRulePacks`, `DiscoverProjectRulePack`,
  `PluginManifest.IIRRules` + `iirRuleContributor` + `EffectiveRulePack`, wired
  into verify (PR #29).
- **Deterministic `GenerateFunction`** demotes from "the backend" to a fallback
  (no LLM available), a test oracle for the lift/emit pair, and the
  `RegenerateStage` baseline.

## SDK enablement: plugins all the way down

For the IL to be *universal*, lift cannot be hardcoded in the host for a
privileged handful of languages. Today three layers are tangled under "the host
language," and only two should pluginize:

1. **Grammar / parse** (text → CST): host-side. One parse per file, shared
   (PRs #26/#27). tree-sitter grammars are a neutral, standardized *asset* — not
   our opinion — so hosting them centrally and sharing one parse is correct.
2. **Extraction** (CST → graph nodes/edges): **already plugins** (the ts/go/py
   SDK plugins).
3. **IIR lift** (CST → IIR): host-side Go (`internal/iir/extract.go`). This is
   the privileged path to remove.

**Principle: bundle the neutral assets (grammars), pluginize the opinionated
logic (extraction, lift, rules).** First-party languages should use the *same*
plugin API as third-party ones — the forcing function that keeps the API honest
(VS Code, LSP, Babel, ESLint all converged here). A privileged host path is where
the public contract rots, and RFC 15 *needs* the plugin contract to be complete
(plugins own lift + semantic profile + rules), so making the built-ins ride it is
the proof that it is.

### What moves, what stays

- **Move to plugins:** IIR lift — port the tree-walking in
  `internal/iir/extract.go` into the ts/go/py SDK plugins, emitted in the *same*
  `extract` pass. This reinforces the single-parse design: today the host parses
  once but the plugin walks the tree for extraction *and* host-Go walks it again
  for lift; after the move the plugin walks once for both and the host stops
  walking for IIR entirely.
- **Stays host-side (correctly):** the parse/grammar registry, and the IIR
  *machinery* — model, `compare`, rules, `verify`, `generate`, `repair`. Only the
  *lift* portion of `internal/iir` is language logic; the rest is the host
  capability RFC 11 correctly located centrally.
- **"Out of the box" becomes a bundling/CI concern.** Ship a curated set of
  default plugin WASMs baked into the release (partly true already:
  `$DATA/plugins/defaults/`), version-locked via the release train — exactly how
  VS Code ships built-in language extensions.

This **dissolves the RFC 11↔15 reconciliation**: there is no "host lift vs plugin
lift" duality to arbitrate. Lift is uniformly plugin-side; the host built-ins are
just the first plugins.

### SDK contract changes

Grounded in the current SDK (`packages/plugin-sdk`), where `PluginDefinition`
today exposes only `language/role/analyzers/tools` and has **no IIR surface**:

- **Track A — conformance rules (small, independent, ship first).** Add
  `iirRules?` to `PluginDefinition` and emit it in the manifest `capabilities`
  block (`abi.ts`). The host already *reads* `PluginManifest.IIRRules` →
  `EffectiveRulePack` — this closes the "no SDK producer" gap and lights up a
  mechanism that is currently dormant. Delivers the "replace decaying `CLAUDE.md`"
  win in one language, today.
- **Track B — plugin-produced lift (the strategic core).**
  - Mirror the IL schema into TS (`FunctionIntent`, `Param`, `Return`,
    `BehaviorClause`, `Expr`, `Visibility`). Hand-author v1; the host's existing
    `iir.ParseIntentJSON` is the deterministic gate that rejects drifted
    emissions, same as hand-authored IIR. Codegen from a single source once the
    schema settles.
  - Extend `ExtractionResult` to carry `iir?: FunctionIntent[]`, each tagged with
    the node id it came from — no second tree walk, no new ABI entry point.
  - **Bonus correctness win:** the plugin knows which node produced which IIR, so
    it emits IIR *already attached to its node id* — eliminating the host's
    `(name, start_byte)` correlation heuristic (Track B2) entirely.
- **Track C — semantic profile (deferred).** How the language realizes
  null/truthiness/numeric/error/dispatch — the binding to the shared vocabulary
  (T5). Needed for *render* (IL→code) and translation, **not** for lift or
  conformance. Do not let it block A/B.
- **Track D — foreign nodes (incremental).** Start by skipping unmodeled
  constructs; add the foreign-node variant (carry the AST subtree) later to reach
  the totality guarantee (T1/T5).

### Migration: parity-gated strangler fig

Removing the Go extractors also removes the host-side fast fallback, so *every*
language — including ts/go/py — goes through plugin lift. That makes this a
migration to complete, not an optional enhancement. De-risk it with machinery we
already have:

1. Build the Track B lift contract.
2. Port ts/go/py lift into the plugins.
3. Run **both** host-Go lift and plugin lift and **use `compare.go` to prove IIR
   parity** on the fixture corpus — dogfooding our own IL↔IL verifier to validate
   the migration; require equality before switching.
4. Flip default to plugin lift, Go lift behind a flag.
5. Delete the Go extractors once parity holds across the corpus.

## Where the real effort moves (honest)

- **Lift becomes safety-critical.** The whole guarantee rests on code→IL being
  correct and *canonical*. A buggy/non-canonical frontend gives false rejects or
  false accepts. The bar on frontend quality rises.
- **IL normalization stops being optional.** The LLM will write `!(a >= b)`
  where the IL was `a < b` — equal, structurally different. The equivalence
  relation on IL (commutativity, De Morgan, canonical forms) now determines the
  verifier's false-reject rate. This is where effort *should* go — a far better
  place than mechanical backends.
- **Fidelity ≠ fitness — by design, not by accident.** The round-trip checks
  faithfulness to the IL, never fitness of the IL for the task. That gap is not a
  round-trip risk; it is the *domain of the conformance layer* (T1). Neither gate
  is asked to do the other's job.

## Sequencing

1. **Single-language conformance first (SDK Track A).** Deliver value in one
   language, on machinery that already exists (rule packs), with no
   frontend/backend matrix, against the pain people feel today (decaying prose
   rules). First concrete primitive: the deferred **structural rule predicate** —
   matching over IL trees (generalizing `whenExpr` structural comparison to
   whole-node/function shape) — which slice 14 made a prerequisite and this RFC
   makes foundational. Requires the small SDK `iirRules` surface.
2. **Plugin-produced lift + built-ins migration (SDK Track B).** Move lift into
   the plugins and retire the host Go extractors via the parity-gated strangler
   fig. This is what makes lift *universal* and dissolves the host/plugin duality.
3. **IL→code via LLM + verify-via-lift**, single language: the fidelity loop on
   `RegenerateStage`, plus IL normalization to control false rejects. Needs the
   semantic profile (SDK Track C).
4. **Cross-language translation** downstream — the *same* loop with the target
   language's frontend as the oracle. No new mechanism; it falls out of the
   substrate once the above exist and a second frontend is mature.

## Non-goals (for now)

- Mechanical IL→AST→code backends (replaced by LLM + verify-via-lift).
- A canonical-semantics L2 (underspecified first; harden per-construct on data).
- Faithful lowering of genuinely non-portable constructs — they lift to foreign
  nodes and carry a portability level; verify reports the gap rather than
  inventing a translation.

## Open decision to close before building

The **cross-scope conformance conflict policy** (Decision 2). Most-specific-wins
+ severity is the working proposal; it needs pressure-testing against real
universal-vs-language conflicts (e.g. a universal "no exceptions" against a
language whose idiom is exceptions) before it is encoded.
