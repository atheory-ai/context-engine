# Slice 1: Verify Function Intent

## Goal

Implement the smallest deterministic IIR verification loop for a single TypeScript function.

The target command:

```bash
context-engine iir verify <intent-file> <source-file> --json
```

The command should read intended IIR, parse the source file, extract actual IIR, compare them, apply rules, and output a verification report.

## In scope

- IIR TypeScript types for function-level intent
- JSON/YAML loading for intended IIR
- Basic schema validation
- TypeScript source parsing
- Function-level IIR extraction
- Deterministic comparison
- Basic rule evaluation
- JSON verification report
- CLI command

## Out of scope

- natural language to IIR
- code generation
- test generation
- multi-file repo indexing
- persistent storage
- remote model calls
- UI
- full plugin marketplace

## Minimum IIR node

```yaml
kind: FunctionIntent
name: validateDonationAmount
language: typescript
inputs:
  - name: amount
    type: Money
  - name: campaign
    type: Campaign
returns:
  type: ValidationResult<Money>
behavior:
  - when: amount is below campaign.minimumDonation
    then: return validation error amount_below_minimum
sideEffects: []
failureModes:
  - amount_below_minimum
constraints:
  - public function must have explicit return type
  - expected validation failure must not throw
```

## CLI behavior

```bash
context-engine iir verify examples/validateDonationAmount.iir.yaml examples/function-source-sample.ts --json
```

Expected output shape:

```json
{
  "status": "passed",
  "intended": {},
  "extracted": {},
  "matches": [],
  "mismatches": [],
  "ruleResults": [],
  "repairTargets": []
}
```

## Acceptance criteria

- Valid IIR files load successfully.
- Invalid IIR files fail with clear diagnostics.
- A TypeScript function can be parsed and converted into FunctionIntent.
- The system detects missing explicit return types.
- The system detects mismatched function names.
- The system detects undeclared side effects for simple known calls.
- The command exits non-zero when verification fails.
- The JSON report is stable enough for tests and agents to consume.
