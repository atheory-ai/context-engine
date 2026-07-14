# Context Engine — Spec 21: Semantic Policy-Pass Pipeline

## Implementation spec — deterministic enrichment and conformance over plans

Status: implemented (foundation, 2026-07-14). Depends on Specs 19 and 20, and
extends the existing IIR rule-pack model without breaking it.

Current implementation: `internal/semantic/passes` supplies host-evaluated,
declarative policy merging and application. It records applied, skipped, and
conflicting policies; preserves declared facts; adds approval questions; and
blocks plans on incompatible mandatory obligations. Plugin manifest loading and
persisting policy sources are deferred to the plugin and build-graph slices.

## Goal

Define a host-owned pipeline of semantic passes that can enrich a resolved plan
before generation and check it after lifting. Policies must be executable,
ordered, reproducible, and explainable—not prompt fragments or opaque plugin
behavior.

## Pass contract

Create `internal/semantic/passes`. A pass receives an immutable plan revision
and a read-only evaluation context, and returns a proposed next revision plus
findings. The host validates and records the revision transition.

```text
Pass
  ID, version, phase, priority
  Applies(plan, context) -> bool
  Run(plan, context) -> PassOutput

PassOutput
  patch: semantic additions/removals/updates by stable ID
  findings: actionable diagnostics
  requiredApprovals
  evidence
```

Phases are `resolve`, `enrich`, `constrain`, `pre_generate`, `verify`, and
`repair_guidance`. Within a phase, order by explicit priority then stable ID.
Passes may add obligations or findings, but may not erase declared claims or
evidence. Every output is captured as a `PassRecord` in the next plan revision.

## Policy semantics

V1 supports declarative policy contributions only. Existing IIR rules continue
to run against observed `FunctionIntent`; their reusable semantics are adapted
into plan constraints where possible. Initial plan policies may require an audit
event for mutations, forbid throws from domain services, require provider-error
wrapping, or require a repository boundary.

Conflicts are never resolved by last writer wins. Two incompatible mandatory
obligations make the plan blocked and identify the policy sources. A more
specific project policy may explicitly override a plugin/default policy only by
recording an override with rationale and approval requirements.

## Plugin boundary

Plugins contribute manifests and declarative policy definitions. The host parses,
validates, orders, and evaluates them. Plugin WASM does not receive arbitrary
write access or execute arbitrary plan transformations in v1. This preserves the
wazero/Extism runtime boundary and makes policy output reproducible.

## Approval and repair

Passes can mark a change as requiring approval. Until approved, it is an open
question or a proposed obligation, not a resolved fact. Repair consumes only
actionable findings tied to plan IDs and evidence; it must not attempt a blind
source rewrite.

## Acceptance criteria

- Built-in and plugin/project policies merge deterministically.
- Conflicts, overrides, skipped passes, and approvals appear in the plan history.
- A mutation policy produces an audit obligation before generation.
- Existing `iir.rules` behavior remains compatible.
- Tests prove no policy path writes directly to the substrate.
