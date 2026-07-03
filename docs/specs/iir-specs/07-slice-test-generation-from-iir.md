# Slice 7: Test Generation from IIR

## Goal

Generate tests from declared intent, not from generated code.

IIR should be the source of truth for both implementation and tests.

## Why

Generating tests from code can reproduce the implementation's mistakes.

Generating tests from IIR creates an independent check against declared behavior.

## In scope

- Generate unit test cases from FunctionIntent behavior rules.
- Generate failure-mode tests.
- Generate side-effect expectation tests.
- Generate test names from behavior descriptions.
- Associate tests with IIR node ids.
- Report which IIR behaviors are covered by tests.

## Out of scope

- integration test orchestration
- browser tests
- property-based tests
- coverage instrumentation
- test runner abstraction beyond MVP

## Acceptance criteria

- Each declared behavior can produce a test case.
- Each failure mode can produce a test case.
- Each side-effect expectation can produce a test case.
- Test output is deterministic.
- Test generation reports unsupported behaviors instead of inventing tests.
- Generated tests include traceability back to IIR ids.
