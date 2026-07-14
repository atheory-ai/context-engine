# Context Engine — Spec 27: Semantic Enrichment Pass

## Implementation spec — derive relevant facts before policy and generation

Status: proposed. Depends on Specs 19, 20, and 26.

## Goal

Add a deterministic enrichment phase between graph resolution and policy
lowering. It derives semantic facts that are relevant to the requested unit so
policies and generation do not need to rediscover them from raw source or large
prompt context.

Enrichment is evidence production, not authority to invent requirements.

## V1 enrichers

Implement host-owned enrichers for the TypeScript mutation slice:

- reachable direct dependencies and architectural boundaries;
- observed direct effects and their classifier basis;
- declared and observed failure behavior, including provider boundaries;
- callers, adjacent tests, and existing implementation conventions;
- ownership and source scope of the selected symbols;
- policy-relevant graph facts, such as an existing audit publisher or repository
  interface.

Each enricher adds `SemanticClaim`s and evidence to a new plan revision. It may
produce `unknown` or `unsupported`; it must not claim universal call-graph or
data-flow coverage from a bounded traversal.

## Design

Place enrichers in `internal/semantic/enrich`. They consume a read-only graph
view, existing observed IIR, and the resolved bindings. They run before policy
passes and share the pass-record/provenance conventions from Spec 21, while
remaining separate from policy: an enricher describes the codebase; a policy
states what should be required.

Each claim declares its evidence class:

- `structural`: graph or syntax fact;
- `semantic`: deterministic lift/classifier fact;
- `heuristic`: bounded inference with lower confidence;
- `unknown`: a required fact could not be established.

Traversal budgets are part of the claim evidence. A cutoff, unresolved dynamic
dispatch, or unsupported syntax must remain visible to downstream policy and
verification.

## Ordering and caching

Run enrichment after binding resolution and before policy lowering. Cache only
by plan revision, source hash, relevant graph revision, and enricher version;
never reuse a claim after its evidence is stale. Cache invalidation is an
optimization, not a correctness boundary.

## Acceptance criteria

- Fixture plans acquire evidence-backed repository, publisher, and provider
  boundary claims before policy evaluation.
- An unsupported call path produces `unknown` with a reason and traversal data.
- Policies can select on enrichment claims without inspecting raw source text.
- Repeated enrichment over unchanged inputs is deterministic.
- Tests demonstrate the distinction between structural, semantic, heuristic,
  and unknown evidence.
