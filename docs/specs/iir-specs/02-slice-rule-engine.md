# Slice 2: IIR Rule Engine

## Goal

Add a rule engine that attaches durable code preferences to IIR objects.

The rule engine should replace prompt-based style guidance for semantic code expectations.

## Principle

Rules apply to semantic objects, not raw text.

A rule should target things like:

- FunctionIntent
- VariableIntent
- BranchIntent
- ExternalInput
- SideEffect
- PublicApi
- ReactHook
- VueComponent

For the MVP, only FunctionIntent is required.

## Example rules

```yaml
rules:
  - id: function-explicit-return-type
    target: FunctionIntent
    severity: error
    when:
      visibility: public
    require:
      explicitReturnType: true

  - id: expected-failures-use-result
    target: FunctionIntent
    severity: warning
    when:
      hasFailureModes: true
    require:
      failureStrategy: ResultType

  - id: declare-side-effects
    target: FunctionIntent
    severity: error
    require:
      sideEffectsDeclared: true
```

## In scope

- Load rule packs from YAML/JSON.
- Validate rule pack shape.
- Match rules against IIR nodes.
- Return pass/fail/warn results.
- Include rule results in verification report.
- Keep rule execution deterministic.

## Out of scope

- arbitrary code execution inside rules
- LLM-judged rules
- custom plugin runtime
- repo-wide architecture rules

## Acceptance criteria

- Rules can be loaded from a file.
- A rule can target FunctionIntent.
- A rule can pass, warn, or fail.
- Rule output includes id, severity, message, target, and repair guidance.
- Rule results are included in the CLI verification report.
