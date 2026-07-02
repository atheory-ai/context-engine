# Context Engine IIR Project Specs

This folder contains the lightweight spec files for adding IIR to the existing Context Engine project.

Start with:

1. `00-overview.md`
2. `01-slice-verify-function-intent.md`
3. `08-agent-kickoff.md`

The first implementation target is:

```bash
ce iir verify <intent-file> <source-file> --json
```

The intended development model is slice-based:

```text
Slice 1: verification
Slice 2: rules
Slice 3: comparison
Slice 4: extraction
Slice 5: plugin surface
Slice 6: generation
Slice 7: tests
```

Do not start with generation. Prove verification first.
