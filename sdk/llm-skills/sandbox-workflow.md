# Using the Sandbox for Plugin Development

## Standard Workflow

1. Scaffold: `pnpm create ce-plugin`
2. Write your plugin in `src/`
3. Add fixture files to `tests/fixtures/` (real code samples the plugin should handle)
4. Build: `pnpm bundle` (esbuild to JS) then `javy compile` (JS to WASM)
5. Run coverage: `ce-sandbox coverage dist/plugin.wasm`
6. Interpret the report:
   - `coveragePct >= 80%` is the target
   - `missingSymbols` tells you what patterns to add to `extract()`
   - `extraNodes` tells you what patterns are over-matching

## Iterating on Coverage

If coverage is low, the `missingSymbols` list tells you exactly what to add.

Example: `missingSymbols` includes `"init"` and `"TestMain"` for a Go plugin.
This means your `extract()` regex doesn't match `func init()` or `func TestMain(`.
Add those patterns.

## What the Coverage Score Means

Coverage measures extraction completeness against a simple heuristic baseline.
A score of 75% means the plugin is finding 3 out of 4 symbols a developer
would expect to be indexed.

- **80%** is the recommended minimum for publishing a plugin
- **100%** is achievable for languages with regular syntax
- **80-90%** is realistic for languages with complex macro systems

## Common Low-Coverage Patterns

**1. Method receivers** (Go)
```
func (r *Receiver) Method() {}
```
The regex must handle the optional receiver before the method name:
```typescript
/^func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/gm
```

**2. Interface methods** (Go, TypeScript)
Methods declared inside an interface body are often missed because
they're indented and don't have the typical declaration prefix.

**3. Exported constants in const blocks** (Go)
```go
const (
  MyConst = 1
)
```
Only `const` is on its own line. Individual constants need block parsing.

**4. Anonymous functions assigned to vars**
```typescript
const handler = async () => { ... }
export const getUser = async (id: string) => { ... }
```
Need a separate regex for arrow functions assigned to `const`.

## Checking a single file

```bash
ce-sandbox run dist/plugin.wasm tests/fixtures/complex.go
```

## Comparing two builds

After making changes:
```bash
ce-sandbox diff dist/plugin.old.wasm dist/plugin.wasm
```

This shows exactly what was added, removed, and the coverage delta.

## JSON output for scripting

```bash
ce-sandbox coverage dist/plugin.wasm --json | jq '.aggregate.coveragePct'
```

The JSON schema is stable (version 1). Scripts built against it won't break
on patch updates to the sandbox.
