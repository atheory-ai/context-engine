# Context Engine — Spec 25: Semantic Platform Documentation and Delivery

## Implementation spec — one current architecture, explicit maturity, releasable contracts

Status: proposed. Runs alongside Specs 19–24 and 26–31; it does not block their initial
design, but each implementation milestone must satisfy its documentation gate.

## Goal

Keep product direction, implementation specs, public documentation, SDK
contracts, and tests aligned as IIR evolves from a function verifier into a
semantic-development platform. Historical RFCs are valuable, but they must not
read as current architecture when the code has moved on.

## Documentation structure

- [`north-star.md`](../../north-star.md) is the durable product thesis.
- [`next-steps.md`](next-steps.md) is the dependency-ordered roadmap and
  decision-gate index.
- Specs 19–24 and 26–31 are normative implementation contracts for the next
  work.
- `docs/iir.md` is the current user-facing capability guide. It states only
  shipped functionality and links to proposed work separately.
- `docs/specs/iir-specs/` retains design history. Each RFC must state status,
  implementation supersession, and current-code deltas at its top when needed.

## Reconciliation work

Audit the IIR specs and code comments for stale claims about host-side versus
plugin-owned lifting, CGO tree-sitter, language support, deterministic versus
model-backed generation, and endpoint names. Replace historical present tense
with a short `Current implementation` note or a supersession link. Do not erase
the decision history.

Publish one capability matrix covering: source lift languages, semantic fields
per language, generation targets, verification levels, policy-pass maturity,
and storage/API availability. Mark each row `shipped`, `experimental`,
`proposed`, or `deferred`.

## SDK and compatibility

Document the plugin IIR wire contract, schema-version negotiation, required
golden fixtures, classifier-basis semantics, and manifest policy contribution
format. The runtime validates all plugin-provided IIR and policy data before
persisting or applying it. A release gate verifies the CE runtime against the
matching plugin SDK/default-plugin build.

## Delivery discipline

Each semantic feature must ship with:

- an implementation spec status update and changelog entry;
- deterministic unit tests plus relevant golden/integration fixtures;
- a migration and rollback note for persisted data;
- CLI/MCP/API contract examples where it exposes a surface;
- explicit limits and unknown-result behavior;
- a review of token-scope, write-buffer, WASM runtime, and core-dependency
  constraints.

## Acceptance criteria

- No public document claims a retired host extractor or permits CGO contrary to
  the current project constraints.
- The capability matrix is generated or tested so drift is detected in CI.
- Every planned spec has an owner/status and links to its implementation work.
- A new contributor can identify the north star, current IIR capabilities, and
  next executable milestone without reading historical RFCs end to end.
