# Context Engine — Spec 23: Evidence-Backed Semantic Verification

## Implementation spec — strengthen fidelity without false certainty

Status: proposed. Depends on Specs 19, 22, and 29, plus the existing IIR
comparator.

## Goal

Extend verification from bounded function-contract comparison into an
evidence-backed report over the claims and obligations of a semantic plan. The
system must distinguish verified, violated, unsupported, and unknown claims;
absence of evidence is never a passing fidelity result.

## Verification result model

Keep the current `iir.Report` compatible for direct FunctionIntent verification.
Add a plan-level report whose entries are tied to stable plan and observed-IIR
IDs:

```text
VerificationFinding
  claimId or obligationId
  result: verified | violated | conditional | unknown | unsupported
  severity
  expected, observed
  evidence: source spans, graph nodes, lifted IIR paths
  repairTarget
```

The overall result is `passed`, `failed`, or `inconclusive`. `passed` means all
mandatory claims are verified and no error-level conformance policy failed.
`inconclusive` means a mandatory claim cannot yet be checked. Callers choose
whether inconclusive blocks acceptance; the mutation vertical slice blocks it by
default.

## Incremental capabilities

V1 verifies the plan claims already supported by lifts: function signature,
visibility, normalized branches and consequences, required/forbidden effects,
failure modes, and policy obligations. It also links observed effects and
failures to their classifier basis (`resolved` versus `heuristic`).

V2 adds bounded interprocedural evidence: calls into resolved repository,
publisher, and provider nodes; propagation/wrapping of known failures; and
effects reachable through direct calls. It must limit traversal, record the
cutoff, and return `unknown` rather than claiming whole-program coverage.

## Extractor quality and parity

Language lifts are part of the verification oracle. Maintain a golden corpus
per supported language and compare canonical observed IIR across equivalent
fixtures. New semantic fields require fixtures for positive, negative, and
unknown cases. Heuristic classifiers must have tests that demonstrate their
lower severity and cannot silently upgrade to proof.

## Acceptance criteria

- Existing IIR reports remain stable for existing callers.
- A required but unobservable effect yields `inconclusive`, not `passed`.
- A direct missing repository call, audit effect, or failure wrap has source and
  plan evidence in its repair target.
- Traversal limits and unsupported constructs are visible in the report.
- Golden and integration tests cover TypeScript first, then parity cases for Go
  and Python where their language semantics allow it.
