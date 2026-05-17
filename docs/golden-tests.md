# How To Write A Golden Test

Golden tests compare deterministic output against a checked-in expected file. They are useful when the behavior is structured, broad enough that many small assertions would be noisy, and important enough that reviewers should inspect the full output when it changes.

This guide is for human contributors and AI agents working in this repository.

## When To Use One

Use a golden test for:

- serialized contracts such as core types, channel emissions, tool results, and run context
- storage schema summaries
- write-buffer flush results
- runner loop fixtures
- API or protocol payloads once their shapes are compatibility-managed

Prefer a normal assertion for:

- one or two scalar expectations
- error branches
- timing, concurrency, or retry behavior
- output that contains unavoidable randomness
- behavior where a small focused failure message is clearer than a fixture diff

## Existing Examples

Current golden tests include:

```text
internal/core/golden_test.go
internal/core/testdata/*.golden.json

internal/storage/migrations/integration_test.go
internal/storage/migrations/testdata/schemas.golden.json

internal/storage/writebuffer/integration_test.go
internal/storage/writebuffer/testdata/flush_writes.golden.json

internal/runner/e2e_golden_test.go
internal/runner/testdata/full_engine_loop/*
```

Follow the nearest existing pattern before adding a new helper.

## File Layout

Place golden files next to the test package:

```text
package/
  thing_test.go
  testdata/
    behavior.golden.json
```

Use `.golden.json` for structured JSON output. For multi-file fixtures, use a named directory:

```text
testdata/
  full_engine_loop/
    query.txt
    strategizer.xml
    reviewer.xml
    activation.golden.json
    tool_emissions.golden.json
    final.md
```

## Determinism Rules

Golden output must be deterministic. Before writing the fixture:

- sort slices when map or database iteration order could vary
- use fixed timestamps, IDs, project IDs, and model names
- normalize floating-point values if precision noise is not meaningful
- strip host-specific paths or replace them with fixture paths
- avoid wall-clock time, random UUIDs, temp directory names, pointer addresses, and goroutine scheduling artifacts
- use stable JSON field names and indentation

If the output cannot be made deterministic, do not use a golden test.

## Standard Helper Pattern

Most JSON golden tests in this repo use this shape:

```go
var updateGolden = flag.Bool("update", false, "update golden files")

func assertGolden(t *testing.T, name string, value any) {
	t.Helper()

	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden value: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", name+".golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}
```

Do not introduce a new update flag name unless there is a strong reason. `-update` is the convention.

## Running Golden Tests

Run the relevant package normally:

```bash
go test ./internal/core
go test ./internal/storage/migrations
go test ./internal/storage/writebuffer
go test ./internal/runner
```

Update a fixture intentionally with:

```bash
go test ./internal/core -update
go test ./internal/storage/migrations -update
go test ./internal/storage/writebuffer -update
```

Then inspect the diff:

```bash
git diff -- internal/core/testdata
git diff -- internal/storage/migrations/testdata
git diff -- internal/storage/writebuffer/testdata
```

Never update golden files just to make tests pass. The diff is the review surface.

## Review Checklist

When a golden file changes, verify:

- the code change explains the fixture change
- no unrelated fields changed
- ordering is stable and intentional
- timestamps, IDs, paths, and costs are deterministic
- the fixture does not contain secrets, local absolute paths, or machine-specific data
- the changed surface is categorized correctly in [stability.md](./stability.md)
- compatibility-managed changes include sibling repo updates or contract-test coverage when needed

## Guidance For AI Agents

Golden tests are useful for AI agents because they make expected behavior explicit, but they are also easy to misuse.

When adding a golden test:

- first identify the contract being protected
- read the nearest existing golden test in the same area
- create the smallest fixture that exercises the contract
- keep generated output deterministic
- run the test once without `-update` if the fixture already exists
- use `-update` only after the code path and expected output are understood
- show or summarize the fixture diff in the final response

When a golden test fails:

- do not immediately run `-update`
- inspect the mismatch
- decide whether the expected behavior or the implementation is wrong
- fix the implementation if the fixture exposed a regression
- update the fixture only for an intentional behavior change

## What Not To Put In A Golden File

Avoid:

- API keys, tokens, or secrets
- absolute local paths
- timestamps from `time.Now()`
- random UUIDs
- nondeterministic map iteration output
- large full logs where a focused summary would work
- raw LLM output unless the LLM is fully scripted or mocked
- generated binary data

## Naming

Use names that describe behavior, not implementation details:

```text
ir_validation.golden.json
channel_emissions.golden.json
schemas.golden.json
flush_writes.golden.json
tool_contract.golden.json
```

For a suite with multiple inputs, either:

- write one combined JSON object with named cases, or
- use a fixture directory when separate files make review clearer.

## Maintenance

Golden files are documentation as much as tests. Keep them readable:

- use `json.MarshalIndent` with two-space indentation
- append a trailing newline
- keep fields ordered by using structs or sorted maps/slices
- prefer compact semantic summaries over dumping entire databases
- remove obsolete fixtures when the protected behavior no longer exists
