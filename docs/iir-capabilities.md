# IIR capability matrix

This is the current shipped-capability matrix. It is checked by
`internal/semantic/lift` tests so documentation and the host lift contract do
not silently drift. “Semantic coverage” means evidence that can satisfy a
mandatory semantic-plan verification obligation; it is deliberately stricter
than a useful function-level extraction.

| Surface | Status | Current capability | Limits / unknown behavior |
| --- | --- | --- | --- |
| Indexed TypeScript source lift | shipped | Plugin-owned `FunctionIntent`: identity, contract, behavior, normalized conditions, effects, failures | Default plugin payload is legacy intent-only, therefore host classifies semantic coverage as `partial` |
| Indexed Go source lift | shipped | Plugin-owned `FunctionIntent`: identity, contract, behavior, normalized conditions, effects, failures | Same legacy/partial semantic-coverage limit |
| Indexed Python source lift | shipped | Plugin-owned `FunctionIntent`: identity, contract, behavior, normalized conditions, effects, failures | Same legacy/partial semantic-coverage limit |
| Plan-aware source lift v1 | experimental | Host validates `schemaVersion: "v1"`, claims, source evidence, and `modeled` / `partial` / `unsupported` coverage | A default language plugin must emit those fields before a mandatory requirement can be proved |
| Semantic fields | shipped | `FunctionIntent`, semantic-plan bindings/claims/obligations/decisions, recipe effects/failures/constraints | No total language-independent IL or whole-program proof |
| Implementation recipe rendering | shipped | Deterministic TypeScript renderer from a resolved plan | Other target languages are rejected instead of guessed |
| Function-level generation and tests | shipped | Deterministic TypeScript `FunctionIntent` source and Vitest/Jest-style test output | Emitted test source is not execution or semantic-coverage evidence |
| Semantic mutation workflow | experimental | TypeScript plan → policy → recipe → render → lift → semantic verification, exposed as `ce iir implement` | Explicit `--write` required; CLI observer reports partial lift coverage when no modeled source evidence exists |
| Semantic verification | shipped | Evidence-backed required effects and failures; `passed`, `failed`, or `inconclusive` | Partial/unsupported lift never passes a mandatory claim |
| Policy passes | experimental | Deterministic host-evaluated declarative policies, approval questions, conflicts, and provenance | Plugin manifest contributions currently support IIR conformance rules; semantic-pass manifest contributions are proposed |
| Semantic build graph | experimental | Buffered durable plan, recipe, artifact, verification, approval, test-plan, and repair lineage | No public CLI/MCP/REST query adapter yet; use storage/query contracts internally |
| REST and MCP IIR APIs | shipped | Verify, generate, and generate-tests endpoints/tools | Semantic-plan mutation and build-graph APIs are not public yet |
| Cross-language generation / total IL | deferred | — | Explicit north-star direction, not a shipped promise |

## Compatibility and release gate

Plugin IIR payloads and manifests are validated by the host before persistence
or conformance evaluation. `make test-iir-golden` builds the in-tree matching
SDK default plugins and runs the real WASM parser/plugin-lift corpus; release
CI runs that same gate after staging the default plugin build. This is the
runtime/SDK compatibility check for the current IIR contract.

For protocol details, see [IIR plugin contracts](./iir-plugins.md). For the
semantic-platform plan, see [next steps](./specs/next-steps.md).
