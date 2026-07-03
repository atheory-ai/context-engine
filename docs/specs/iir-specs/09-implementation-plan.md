# Implementation Plan

## Phase 1: Verification foundation

Build:

- IIR model types
- schema validation
- YAML/JSON loaders
- TypeScript function extractor
- comparator
- rule engine
- verification report
- CLI command

Result:

```text
IIR + source → verification report
```

## Phase 2: Rule packs and semantic preferences

Build:

- rule pack loading
- built-in defensive coding rules
- team/repo config loading
- rule result reporting
- repair guidance

Result:

```text
semantic preferences become executable rules
```

## Phase 3: Plugin contracts

Build:

- plugin interfaces
- manifest schema
- built-in plugin registration
- extension docs

Result:

```text
languages, frameworks, and team preferences can extend IIR
```

## Phase 4: Generation

Build:

- FunctionIntent to TypeScript emitter
- generated source re-extraction
- generated source verification
- unsupported-intent diagnostics

Result:

```text
IIR → code → extracted IIR → comparison
```

## Phase 5: Tests

Build:

- FunctionIntent to test emitter
- behavior coverage report
- failure-mode test generation
- side-effect test generation

Result:

```text
IIR → implementation
IIR → tests
```

## Phase 6: Harness integration

Build:

- agent workflow hooks
- model-stage interfaces
- user approval points
- iterative repair loop

Result:

```text
agent and user shape intent, harness turns it into verified code
```
