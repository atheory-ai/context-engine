# Plugin Validation Checklist

Before shipping a plugin, verify all of the following.

## Manifest

- [ ] `id` is reverse-domain format (e.g., `com.example.my-plugin`)
- [ ] `name` is human-readable
- [ ] `version` is valid semver (`1.0.0`, not `v1.0.0`)

## Language Handler (if present)

- [ ] `match()` is fast — regex only, no string parsing
- [ ] `match()` handles edge cases: empty path, path with query string
- [ ] `extract()` always returns `{ nodes: [], edges: [] }` shape
- [ ] Every node uses `nodeID()` — no manually constructed IDs
- [ ] Every edge uses `edgeID()` — no manually constructed IDs
- [ ] Nodes are deduplicated before returning
- [ ] Edges are deduplicated before returning
- [ ] No `fs`, `path`, or `process` references
- [ ] Extraction handles malformed/empty files gracefully (no throws)

## Concept Seeds (if present)

- [ ] All terms are `lowercase-hyphenated`
- [ ] Terms match `/^[a-z][a-z0-9-]*$/`
- [ ] At least one term (empty array is valid but not useful)

## Tools (if present)

- [ ] Each `description` is ≤ 100 characters
- [ ] Each `activate()` is a pure function — no side effects
- [ ] Each `activate()` has no assignments or function calls
- [ ] `execute()` handles the case where `anchors` is empty
- [ ] `execute()` handles substrate returning empty results

## Analyzers (if present)

- [ ] `analyze()` handles empty `nodes` array
- [ ] Edges returned by `analyze()` use IDs from the input nodes
- [ ] Source class is `"speculative"` for inferred relationships

## Coverage (via ce-sandbox)

- [ ] At least 3 fixture files covering different patterns
- [ ] Coverage ≥ 80% on all fixtures
- [ ] No fixture with 0 extracted nodes but >0 expected symbols

## Build

- [ ] Plugin compiles to valid WASM
- [ ] `ce plugin validate dist/plugin.wasm` passes
- [ ] `ce_plugin_manifest` export returns valid JSON
