# Slice 3: IIR Comparison and Repair Targets

## Goal

Compare intended IIR against extracted IIR and produce useful mismatch diagnostics.

The comparison system should answer:

```text
Does this code express the declared intent?
```

When the answer is no, it should say what changed and what should be repaired.

## Comparison model

The comparator should distinguish:

- exact match
- acceptable equivalent
- missing behavior
- extra behavior
- mismatched type
- undeclared side effect
- changed failure mode
- changed public contract
- unknown or unsupported comparison

## Example mismatch

```json
{
  "kind": "undeclared_side_effect",
  "severity": "error",
  "path": "FunctionIntent.sideEffects",
  "message": "Source code sends analytics but intended IIR declares no side effects.",
  "expected": [],
  "actual": ["analytics.track"],
  "repairTarget": "Either remove analytics.track or declare the side effect in intended IIR."
}
```

## In scope

- Function name comparison
- Input name/type comparison
- Return type comparison
- Failure mode comparison
- Simple side-effect comparison
- Basic behavior count/comparison
- Repair target generation

## Out of scope

- semantic equivalence across arbitrary implementations
- algorithmic correctness
- proof-level verification
- full dataflow comparison

## Acceptance criteria

- Comparator produces stable machine-readable mismatches.
- Mismatches include a repair target.
- Equivalent formatting or syntax differences do not cause failure.
- Unknown comparisons are reported as unsupported, not silently passed.
