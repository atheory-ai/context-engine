# Context Engine — Spec 30: Semantic Repair Planning

## Implementation spec — repair the plan or recipe before retrying generation

Status: proposed. Depends on Specs 23 and 28, and replaces blind regeneration as
the default repair strategy for plan-backed work.

## Goal

Turn verification failures into a minimal, inspectable semantic repair plan.
The system should change the source-rendering recipe only where evidence shows
the generated code diverged from the intended plan or conformance obligations.

## Repair model

Create `internal/semantic/repair`. It consumes a plan revision, recipe,
generation artifact, observed lift, and plan-level verification report. It emits:

```text
RepairPlan
  id, parentPlanRevision, artifactId, verificationId
  changes: []PlanPatch or []RecipePatch
  rationale: findings and evidence references
  status: proposed | approved | applied | rejected | exhausted
  affectedClaimIds, affectedObligationIds
```

Repairs are classified before proposal:

- **implementation divergence:** source failed to realize a valid recipe;
  patch the recipe or ask the renderer to satisfy the same constraints.
- **resolution/policy defect:** the intended plan has contradictory or stale
  bindings/obligations; return to the appropriate compiler pass.
- **insufficient evidence:** verification is inconclusive; do not fabricate a
  source repair.
- **user-decision required:** the plan contains a genuine product ambiguity;
  present the choice instead of retrying.

## Renderer interaction

The renderer receives the original recipe plus an explicit repair delta and
the relevant findings. It must not be asked to infer a fix from a raw failing
test log alone. A repaired candidate is always lifted and verified again against
the same or explicitly revised plan revision.

Bound retries by distinct repair-plan identity, not only an integer counter, to
avoid repeated equivalent generation. Preserve every candidate hash and verdict
for debugging and later evaluation. Source writes still require explicit user
authorization.

## Approval and safety

Automatic repair is permitted only for non-ambiguous implementation divergence
and only within a user-authorized generation attempt. Repairs that alter declared
intent, bindings, policy overrides, or external-effect requirements require
approval and create a new immutable plan revision.

## Acceptance criteria

- A missing audit effect yields a targeted recipe patch, not a whole-source
  regeneration instruction.
- A missing binding or conflicting policy yields an open question, not a retry.
- Repeated equivalent failed candidates terminate deterministically.
- Repair history links plan, recipe, source candidate, observed lift, and report.
- Existing `RepairLoop` remains available for direct FunctionIntent workflows.
