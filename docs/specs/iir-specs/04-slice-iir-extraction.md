# Slice 4: TypeScript IIR Extraction

## Goal

Extract basic FunctionIntent from TypeScript source using AST and semantic analysis.

## Required extraction

For one function, extract:

- function name
- export/public visibility
- parameter names
- parameter type annotations where available
- return type annotation where available
- simple return statements
- simple conditional branches
- obvious thrown errors
- obvious side-effect calls
- imported dependencies used by the function

## Side-effect heuristic

For the MVP, side effects can be detected conservatively.

Treat the following as side effects:

- calls to imported clients/services
- calls containing names like `track`, `send`, `emit`, `publish`, `save`, `create`, `update`, `delete`, `write`
- assignments to non-local variables
- mutation of input parameters

The extractor should prefer false positives over false negatives.

## In scope

- TypeScript parser integration
- Function declaration extraction
- Exported function detection
- Simple AST traversal
- Conservative side-effect detection
- Extractor tests

## Out of scope

- whole-program type checking
- full control-flow graph
- framework-specific extraction
- class method support unless easy
- JSX/TSX component extraction

## Acceptance criteria

- A simple exported TypeScript function extracts to FunctionIntent.
- Missing parameter types are represented as unknown, not dropped.
- Missing return types are represented as absent.
- Simple branches are captured.
- Simple side effects are captured.
- Extraction output is deterministic.
