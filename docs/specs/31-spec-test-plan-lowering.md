# Context Engine — Spec 31: Test-Plan Lowering

## Implementation spec — derive tests and coverage from resolved semantics

Status: proposed. Depends on Specs 19, 23, and 28, and evolves the existing
FunctionIntent-based test generator.

## Goal

Lower a resolved semantic plan into a target-independent `TestPlan`, then render
tests and coverage evidence from that plan. Tests must derive from intended
behavior, required effects, failure modes, policy obligations, and uncertainty—
not merely from the implementation the system just generated.

## Test-plan model

Create `internal/semantic/testplan`. A `TestPlan` references a plan revision and
contains stable expectations:

```text
TestPlan
  id, schemaVersion, planRevisionId
  cases: []TestCase
  coverage: []CoverageExpectation
  gaps: []VerificationGap
  targetLanguage, frameworkProfile

TestCase
  id, category, preconditions, action, expectedOutcome
  requiredEffects, forbiddenEffects, expectedFailures
  sourceClaimIds, obligationIds
```

Categories include nominal behavior, branch behavior, failure propagation,
effect/audit behavior, boundary contracts, and regression cases from prior
repair findings. A coverage expectation is `covered`, `not_generatable`, or
`unknown`; the generator must not claim full coverage merely because it emitted
a test file.

## Lowering rules

- Every mandatory plan claim and obligation receives a coverage expectation.
- A known missing verification capability becomes a visible test/review gap.
- Policy obligations generate tests when they are observable; otherwise they
  remain static-verification obligations with an explanation.
- Test cases use resolved symbols and recipe interfaces rather than guessing
  imports, fixtures, or framework conventions.
- Existing `GenerateTests(FunctionIntent)` remains as a compatible narrow path;
  it can be adapted to create an initial test plan where no semantic plan exists.

## Rendering and execution

Test rendering is a separate target/framework adapter. The initial adapter is
TypeScript Vitest/Jest using the existing emitter where possible. Rendered tests
are lifted only for artifact linkage; test execution results are evidence about
the source artifact, not proof that all semantic claims are verified.

## Acceptance criteria

- The mutation fixture produces nominal, failure, audit-effect, and policy
  boundary test cases tied to stable plan IDs.
- An unobservable policy is reported as a gap instead of a fabricated test.
- Coverage output distinguishes emitted, executable, passing, failing, and
  semantically covered cases.
- A repair finding can add a regression case to the next test-plan revision.
- Existing FunctionIntent test-generation tests remain compatible.
